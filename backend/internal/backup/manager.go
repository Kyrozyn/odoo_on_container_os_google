package backup

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/database"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/store"
)

type Manager struct {
	cfg    *config.Config
	store  *store.Store
	docker *docker.Client
	db     *database.Manager

	mu   sync.Mutex
	cron *cron.Cron
}

func NewManager(cfg *config.Config, st *store.Store, dc *docker.Client, dbm *database.Manager) *Manager {
	return &Manager{
		cfg:    cfg,
		store:  st,
		docker: dc,
		db:     dbm,
		cron:   cron.New(),
	}
}

// StartScheduler loads all enabled BackupSchedules into the cron runner.
func (m *Manager) StartScheduler() {
	m.ReloadSchedules()
	m.cron.Start()
}

// StopScheduler stops the background cron runner.
func (m *Manager) StopScheduler() {
	m.cron.Stop()
}

// ReloadSchedules rebuilds cron entries from stored schedules.
func (m *Manager) ReloadSchedules() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range m.cron.Entries() {
		m.cron.Remove(entry.ID)
	}

	st := m.store.Get()
	for _, sched := range st.BackupSchedules {
		if !sched.Enabled {
			continue
		}
		sc := sched
		_, _ = m.cron.AddFunc(sc.CronExpression, func() {
			m.runScheduledBackup(sc)
		})
	}
}

func (m *Manager) runScheduledBackup(sc models.BackupSchedule) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	inst, ok := m.store.FindInstance(sc.InstanceID)
	if !ok {
		return
	}
	_, err := m.CreateBackup(ctx, inst, sc.DBName, "scheduled")
	now := time.Now()
	status := "success"
	if err != nil {
		status = "error: " + err.Error()
	}

	_ = m.store.Update(func(s *models.State) error {
		for i := range s.BackupSchedules {
			if s.BackupSchedules[i].ID == sc.ID {
				s.BackupSchedules[i].LastRunAt = &now
				s.BackupSchedules[i].LastStatus = status
				break
			}
		}
		return nil
	})

	if err == nil && sc.RetentionCount > 0 {
		m.enforceRetention(inst.ID, sc.DBName, sc.RetentionCount)
	}
}

// CreateBackup produces a standard Odoo-compatible .zip containing dump.sql, filestore/, and manifest.json.
func (m *Manager) CreateBackup(ctx context.Context, inst models.OdooInstance, dbName, source string) (*models.BackupRecord, error) {
	if dbName == "" {
		return nil, fmt.Errorf("database name is required")
	}
	if source == "" {
		source = "manual"
	}

	if err := os.MkdirAll(m.cfg.BackupsDir(), 0o755); err != nil {
		return nil, err
	}

	ts := time.Now().Format("20060102-150405")
	fileName := fmt.Sprintf("%s_%s_%s.zip", inst.Name, dbName, ts)
	zipPath := filepath.Join(m.cfg.BackupsDir(), fileName)

	tmpDir, err := os.MkdirTemp("", "runodoo-backup-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	// 1. Dump PostgreSQL database to dump.sql
	dumpPath := filepath.Join(tmpDir, "dump.sql")
	dumpFile, err := os.Create(dumpPath)
	if err != nil {
		return nil, err
	}
	pgUser, pgPass := m.db.AdminCredsForInstance(inst)
	err = m.docker.ExecStream(ctx, inst.DBContainerName, []string{
		"pg_dump", "--no-owner", "-U", pgUser, dbName,
	}, []string{"PGPASSWORD=" + pgPass}, dumpFile)
	dumpFile.Close()
	if err != nil {
		return nil, fmt.Errorf("pg_dump failed: %w", err)
	}

	// 2. Build zip archive
	zf, err := os.Create(zipPath)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(zf)

	if err := addFileToZip(zw, dumpPath, "dump.sql"); err != nil {
		zw.Close()
		zf.Close()
		_ = os.Remove(zipPath)
		return nil, err
	}

	// Add manifest.json
	manifestBytes, _ := json.MarshalIndent(map[string]any{
		"odoo_dump":  "1",
		"db_name":    dbName,
		"version":    string(inst.Version) + ".0",
		"created_by": "RunOdoo",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	mw, _ := zw.Create("manifest.json")
	_, _ = mw.Write(manifestBytes)

	// Add filestore if present
	hasFilestore := false
	fsDir := filepath.Join(m.cfg.InstancesDir(), inst.Name, "data", "filestore", dbName)
	if info, err := os.Stat(fsDir); err == nil && info.IsDir() {
		hasFilestore = true
		_ = filepath.Walk(fsDir, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(fsDir, path)
			if err != nil {
				return nil
			}
			return addFileToZip(zw, path, "filestore/"+filepath.ToSlash(rel))
		})
	}

	if err := zw.Close(); err != nil {
		zf.Close()
		_ = os.Remove(zipPath)
		return nil, err
	}
	zf.Close()

	fi, _ := os.Stat(zipPath)
	rec := models.BackupRecord{
		ID:           randomID(),
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		DBName:       dbName,
		FileName:     fileName,
		FilePath:     zipPath,
		SizeBytes:    fi.Size(),
		HasFilestore: hasFilestore,
		Source:       source,
		CreatedAt:    time.Now(),
	}

	if err := m.store.Update(func(s *models.State) error {
		s.Backups = append([]models.BackupRecord{rec}, s.Backups...)
		return nil
	}); err != nil {
		return nil, err
	}
	return &rec, nil
}

// RestoreBackup restores a .zip backup archive (dump.sql + optional filestore/) into targetDBName.
func (m *Manager) RestoreBackup(ctx context.Context, inst models.OdooInstance, zipPath, targetDBName string) error {
	if targetDBName == "" {
		return fmt.Errorf("target database name is required")
	}

	tmpDir, err := os.MkdirTemp("", "runodoo-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Extract zip
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("invalid backup zip: %w", err)
	}
	defer r.Close()

	var dumpFound bool
	cleanTmp := filepath.Clean(tmpDir) + string(os.PathSeparator)
	for _, f := range r.File {
		target := filepath.Join(tmpDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanTmp) && filepath.Clean(target) != filepath.Clean(tmpDir) {
			continue
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		if filepath.Base(f.Name) == "dump.sql" {
			dumpFound = true
		}
	}
	if !dumpFound {
		return fmt.Errorf("backup archive does not contain dump.sql")
	}

	// Recreate target database cleanly
	_ = m.db.DropDatabase(ctx, inst, targetDBName)
	if err := m.db.CreateDatabase(ctx, inst, targetDBName); err != nil {
		return fmt.Errorf("failed creating target database: %w", err)
	}

	// Copy dump.sql into shared/dedicated pgdata or /tmp via instance bind?
	// Even cleaner: write dump.sql into a temp file inside pgdata (which is mounted at /var/lib/postgresql/data in Postgres container) and execute psql -f!
	var pgHostTempDir, pgContainerTempPath string
	tmpSQLName := "runodoo_restore_" + randomID() + ".sql"
	if inst.DBMode == models.DBModeShared {
		pgHostTempDir = filepath.Join(m.cfg.DataDir, "postgres-shared")
	} else {
		pgHostTempDir = filepath.Join(m.cfg.InstancesDir(), inst.Name, "pgdata")
	}
	hostSQLPath := filepath.Join(pgHostTempDir, tmpSQLName)
	pgContainerTempPath = "/var/lib/postgresql/data/" + tmpSQLName

	dumpBytes, err := os.ReadFile(filepath.Join(tmpDir, "dump.sql"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(hostSQLPath, dumpBytes, 0o644); err != nil {
		return err
	}
	defer os.Remove(hostSQLPath)

	pgUser, pgPass := m.db.AdminCredsForInstance(inst)
	out, code, err := m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", targetDBName, "-f", pgContainerTempPath,
	}, []string{"PGPASSWORD=" + pgPass})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("psql restore failed (exit %d): %s", code, strings.TrimSpace(out))
	}

	// Transfer ownership of restored objects to instance's DB user
	reassignSQL := fmt.Sprintf(`DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
    EXECUTE 'ALTER TABLE public.' || quote_ident(r.tablename) || ' OWNER TO ' || quote_ident('%s');
  END LOOP;
  FOR r IN SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = 'public' LOOP
    EXECUTE 'ALTER SEQUENCE public.' || quote_ident(r.sequence_name) || ' OWNER TO ' || quote_ident('%s');
  END LOOP;
END$$;`, inst.DBUser, inst.DBUser)
	_, _, _ = m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", targetDBName, "-c", reassignSQL,
	}, []string{"PGPASSWORD=" + pgPass})

	// Restore filestore if included in backup
	srcFS := filepath.Join(tmpDir, "filestore")
	if info, err := os.Stat(srcFS); err == nil && info.IsDir() {
		dstFS := filepath.Join(m.cfg.InstancesDir(), inst.Name, "data", "filestore", targetDBName)
		_ = os.RemoveAll(dstFS)
		_ = os.MkdirAll(dstFS, 0o777)
		_ = copyDir(srcFS, dstFS)
	}
	return nil
}

// DeleteBackup removes a backup file and its state record.
func (m *Manager) DeleteBackup(id string) error {
	var filePath string
	err := m.store.Update(func(s *models.State) error {
		filtered := make([]models.BackupRecord, 0, len(s.Backups))
		found := false
		for _, b := range s.Backups {
			if b.ID == id {
				filePath = b.FilePath
				found = true
				continue
			}
			filtered = append(filtered, b)
		}
		if !found {
			return fmt.Errorf("backup not found")
		}
		s.Backups = filtered
		return nil
	})
	if err != nil {
		return err
	}
	if filePath != "" {
		_ = os.Remove(filePath)
	}
	return nil
}

func (m *Manager) enforceRetention(instanceID, dbName string, keep int) {
	st := m.store.Get()
	var matching []models.BackupRecord
	for _, b := range st.Backups {
		if b.InstanceID == instanceID && b.DBName == dbName && b.Source == "scheduled" {
			matching = append(matching, b)
		}
	}
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.After(matching[j].CreatedAt)
	})
	for i := keep; i < len(matching); i++ {
		_ = m.DeleteBackup(matching[i].ID)
	}
}

func addFileToZip(zw *zip.Writer, srcPath, zipName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(zipName)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o777)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o666)
	})
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

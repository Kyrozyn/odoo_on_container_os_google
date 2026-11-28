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

	mu     sync.Mutex
	cron   *cron.Cron
	jobCh  chan string
	stopCh chan struct{}
}

func NewManager(cfg *config.Config, st *store.Store, dc *docker.Client, dbm *database.Manager) *Manager {
	return &Manager{
		cfg:    cfg,
		store:  st,
		docker: dc,
		db:     dbm,
		cron:   cron.New(),
		jobCh:  make(chan string, 128),
		stopCh: make(chan struct{}),
	}
}

// StartScheduler loads all enabled BackupSchedules into the cron runner and starts the FIFO job worker.
func (m *Manager) StartScheduler() {
	now := time.Now()
	_ = m.store.Update(func(s *models.State) error {
		for i := range s.BackupJobs {
			if s.BackupJobs[i].Status == models.BackupJobQueued || s.BackupJobs[i].Status == models.BackupJobRunning {
				s.BackupJobs[i].Status = models.BackupJobFailed
				s.BackupJobs[i].Error = "interrupted by server restart"
				s.BackupJobs[i].Step = "Interrupted"
				s.BackupJobs[i].FinishedAt = &now
			}
		}
		return nil
	})
	go m.runWorker()
	m.ReloadSchedules()
	m.cron.Start()
}

// StopScheduler stops the background cron runner and job worker.
func (m *Manager) StopScheduler() {
	m.cron.Stop()
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
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
	inst, ok := m.store.FindInstance(sc.InstanceID)
	if !ok {
		now := time.Now()
		_ = m.store.Update(func(s *models.State) error {
			for i := range s.BackupSchedules {
				if s.BackupSchedules[i].ID == sc.ID {
					s.BackupSchedules[i].LastRunAt = &now
					s.BackupSchedules[i].LastStatus = "error: instance not found"
					break
				}
			}
			return nil
		})
		return
	}

	if _, err := m.EnqueueBackup(inst, sc.DBName, "scheduled", sc.ID, sc.RetentionCount); err != nil {
		now := time.Now()
		_ = m.store.Update(func(s *models.State) error {
			for i := range s.BackupSchedules {
				if s.BackupSchedules[i].ID == sc.ID {
					s.BackupSchedules[i].LastRunAt = &now
					s.BackupSchedules[i].LastStatus = "error: " + err.Error()
					break
				}
			}
			return nil
		})
		return
	}

	_ = m.store.Update(func(s *models.State) error {
		for i := range s.BackupSchedules {
			if s.BackupSchedules[i].ID == sc.ID {
				s.BackupSchedules[i].LastStatus = "queued"
				break
			}
		}
		return nil
	})
}

const maxPersistedJobs = 50

// EnqueueBackup adds a backup creation job to the FIFO queue and returns immediately.
func (m *Manager) EnqueueBackup(inst models.OdooInstance, dbName, source, scheduleID string, retention int) (*models.BackupJob, error) {
	if strings.TrimSpace(dbName) == "" {
		return nil, fmt.Errorf("database name is required")
	}
	if source == "" {
		source = "manual"
	}
	job := models.BackupJob{
		ID:           randomID(),
		Type:         models.BackupJobCreate,
		Source:       source,
		Status:       models.BackupJobQueued,
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		DBName:       strings.TrimSpace(dbName),
		Step:         "Queued",
		ScheduleID:   scheduleID,
		Retention:    retention,
		CreatedAt:    time.Now(),
	}
	if err := m.enqueueJob(job); err != nil {
		return nil, err
	}
	return &job, nil
}

// EnqueueRestore adds a restore job to the FIFO queue and returns immediately.
func (m *Manager) EnqueueRestore(inst models.OdooInstance, zipPath, targetDBName, source, backupID, fileName string, cleanupZip bool) (*models.BackupJob, error) {
	if strings.TrimSpace(targetDBName) == "" {
		return nil, fmt.Errorf("target database name is required")
	}
	if source == "" {
		source = "restore"
	}
	job := models.BackupJob{
		ID:           randomID(),
		Type:         models.BackupJobRestore,
		Source:       source,
		Status:       models.BackupJobQueued,
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		DBName:       strings.TrimSpace(targetDBName),
		BackupID:     backupID,
		FileName:     fileName,
		Step:         "Queued",
		ZipPath:      zipPath,
		CleanupZip:   cleanupZip,
		CreatedAt:    time.Now(),
	}
	if err := m.enqueueJob(job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ClearFinishedJobs removes completed and failed jobs from state.
func (m *Manager) ClearFinishedJobs() error {
	return m.store.Update(func(s *models.State) error {
		kept := make([]models.BackupJob, 0, len(s.BackupJobs))
		for _, j := range s.BackupJobs {
			if j.Status == models.BackupJobQueued || j.Status == models.BackupJobRunning {
				kept = append(kept, j)
			}
		}
		s.BackupJobs = kept
		return nil
	})
}

func (m *Manager) enqueueJob(job models.BackupJob) error {
	if err := m.store.Update(func(s *models.State) error {
		s.BackupJobs = append([]models.BackupJob{job}, s.BackupJobs...)
		if len(s.BackupJobs) > maxPersistedJobs {
			s.BackupJobs = s.BackupJobs[:maxPersistedJobs]
		}
		return nil
	}); err != nil {
		return err
	}

	select {
	case m.jobCh <- job.ID:
		return nil
	default:
		errMsg := "backup job queue is full"
		now := time.Now()
		_ = m.updateJob(job.ID, func(j *models.BackupJob) {
			j.Status = models.BackupJobFailed
			j.Error = errMsg
			j.Step = "Failed"
			j.FinishedAt = &now
		})
		return fmt.Errorf("%s", errMsg)
	}
}

func (m *Manager) runWorker() {
	for {
		select {
		case <-m.stopCh:
			return
		case jobID := <-m.jobCh:
			m.executeJob(jobID)
		}
	}
}

func (m *Manager) updateJob(jobID string, fn func(*models.BackupJob)) error {
	return m.store.Update(func(s *models.State) error {
		for i := range s.BackupJobs {
			if s.BackupJobs[i].ID == jobID {
				fn(&s.BackupJobs[i])
				break
			}
		}
		return nil
	})
}

func (m *Manager) executeJob(jobID string) {
	st := m.store.Get()
	var job *models.BackupJob
	for i := range st.BackupJobs {
		if st.BackupJobs[i].ID == jobID {
			j := st.BackupJobs[i]
			job = &j
			break
		}
	}
	if job == nil {
		return
	}

	if job.CleanupZip && job.ZipPath != "" {
		defer os.Remove(job.ZipPath)
	}

	started := time.Now()
	_ = m.updateJob(jobID, func(j *models.BackupJob) {
		j.Status = models.BackupJobRunning
		j.StartedAt = &started
		j.Step = "Starting..."
	})

	inst, ok := m.store.FindInstance(job.InstanceID)
	if !ok {
		finished := time.Now()
		_ = m.updateJob(jobID, func(j *models.BackupJob) {
			j.Status = models.BackupJobFailed
			j.Error = "instance not found"
			j.Step = "Failed"
			j.FinishedAt = &finished
		})
		m.finishScheduleStatus(job.ScheduleID, "error: instance not found")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	setStep := func(step string) {
		_ = m.updateJob(jobID, func(j *models.BackupJob) {
			j.Step = step
		})
	}

	switch job.Type {
	case models.BackupJobCreate:
		setStep("Dumping PostgreSQL database...")
		rec, err := m.createBackupWithStep(ctx, inst, job.DBName, job.Source, setStep)
		finished := time.Now()
		if err != nil {
			_ = m.updateJob(jobID, func(j *models.BackupJob) {
				j.Status = models.BackupJobFailed
				j.Error = err.Error()
				j.Step = "Failed"
				j.FinishedAt = &finished
			})
			m.finishScheduleStatus(job.ScheduleID, "error: "+err.Error())
			return
		}
		_ = m.updateJob(jobID, func(j *models.BackupJob) {
			j.Status = models.BackupJobCompleted
			j.BackupID = rec.ID
			j.FileName = rec.FileName
			j.Step = "Completed"
			j.FinishedAt = &finished
		})
		m.finishScheduleStatus(job.ScheduleID, "success")
		if job.Retention > 0 {
			m.enforceRetention(inst.ID, job.DBName, job.Retention)
		}

	case models.BackupJobRestore:
		setStep("Extracting backup archive...")
		err := m.restoreBackupWithStep(ctx, inst, job.ZipPath, job.DBName, setStep)
		finished := time.Now()
		if err != nil {
			_ = m.updateJob(jobID, func(j *models.BackupJob) {
				j.Status = models.BackupJobFailed
				j.Error = err.Error()
				j.Step = "Failed"
				j.FinishedAt = &finished
			})
			return
		}
		_ = m.updateJob(jobID, func(j *models.BackupJob) {
			j.Status = models.BackupJobCompleted
			j.Step = "Completed"
			j.FinishedAt = &finished
		})
	}
}

func (m *Manager) finishScheduleStatus(scheduleID, status string) {
	if scheduleID == "" {
		return
	}
	now := time.Now()
	_ = m.store.Update(func(s *models.State) error {
		for i := range s.BackupSchedules {
			if s.BackupSchedules[i].ID == scheduleID {
				s.BackupSchedules[i].LastRunAt = &now
				s.BackupSchedules[i].LastStatus = status
				break
			}
		}
		return nil
	})
}

// CreateBackup produces a standard Odoo-compatible .zip containing dump.sql, filestore/, and manifest.json.
func (m *Manager) CreateBackup(ctx context.Context, inst models.OdooInstance, dbName, source string) (*models.BackupRecord, error) {
	return m.createBackupWithStep(ctx, inst, dbName, source, nil)
}

func (m *Manager) createBackupWithStep(ctx context.Context, inst models.OdooInstance, dbName, source string, onStep func(string)) (*models.BackupRecord, error) {
	if dbName == "" {
		return nil, fmt.Errorf("database name is required")
	}
	if source == "" {
		source = "manual"
	}
	step := func(msg string) {
		if onStep != nil {
			onStep(msg)
		}
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
	step("Dumping PostgreSQL database...")
	dumpPath := filepath.Join(tmpDir, "dump.sql")
	dumpFile, err := os.Create(dumpPath)
	if err != nil {
		return nil, err
	}
	step("Restoring SQL dump...")
	pgUser, pgPass := m.db.AdminCredsForInstance(inst)
	err = m.docker.ExecStream(ctx, inst.DBContainerName, []string{
		"pg_dump", "--no-owner", "-U", pgUser, dbName,
	}, []string{"PGPASSWORD=" + pgPass}, dumpFile)
	dumpFile.Close()
	if err != nil {
		return nil, fmt.Errorf("pg_dump failed: %w", err)
	}

	// 2. Build zip archive
	step("Creating ZIP archive (dump + filestore)...")
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
	return m.restoreBackupWithStep(ctx, inst, zipPath, targetDBName, nil)
}

func (m *Manager) restoreBackupWithStep(ctx context.Context, inst models.OdooInstance, zipPath, targetDBName string, onStep func(string)) error {
	if targetDBName == "" {
		return fmt.Errorf("target database name is required")
	}
	step := func(msg string) {
		if onStep != nil {
			onStep(msg)
		}
	}

	tmpDir, err := os.MkdirTemp("", "runodoo-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Extract zip
	step("Extracting backup archive...")
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("invalid backup zip: %w", err)
	}
	defer r.Close()

	var dumpFound bool
	absTmp, err := filepath.Abs(tmpDir)
	if err != nil {
		return err
	}
	tmpPrefix := absTmp + string(os.PathSeparator)
	for _, f := range r.File {
		if f.Name == "" || filepath.IsAbs(f.Name) {
			continue
		}
		cleaned := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(f.Name, "\\", "/")))
		if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			continue
		}
		target, err := filepath.Abs(filepath.Join(absTmp, cleaned))
		if err != nil || !strings.HasPrefix(target, tmpPrefix) {
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
	step("Recreating target database...")
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

	step("Restoring SQL dump...")
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
		step("Restoring filestore...")
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

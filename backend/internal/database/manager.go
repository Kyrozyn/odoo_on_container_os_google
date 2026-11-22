package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/store"
	"github.com/runodoo/runodoo/internal/system"
)

var validDBName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)

type Manager struct {
	cfg    *config.Config
	store  *store.Store
	docker *docker.Client
}

func NewManager(cfg *config.Config, st *store.Store, dc *docker.Client) *Manager {
	return &Manager{cfg: cfg, store: st, docker: dc}
}

// EnsureSharedPostgres makes sure the shared PostgreSQL container is created and running.
func (m *Manager) EnsureSharedPostgres(ctx context.Context) error {
	st := m.store.Get()
	pg := st.SharedPostgres

	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		return err
	}

	insp, err := m.docker.InspectContainer(ctx, pg.ContainerName)
	if err == nil {
		if !insp.State.Running {
			if err := m.docker.StartContainer(ctx, pg.ContainerName); err != nil {
				return err
			}
			return m.waitPostgresReady(ctx, pg.ContainerName, pg.SuperUser)
		}
		return nil
	}

	if !m.docker.ImageExists(ctx, pg.Image) {
		if err := m.docker.PullImage(ctx, pg.Image); err != nil {
			return err
		}
	}

	sysInfo := system.CollectSystemInfo(m.cfg.DataDir, m.cfg.Port, true, "")
	hostPGData := filepath.Join(m.cfg.HostDataDir, "postgres-shared")

	_, err = m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  pg.ContainerName,
		Image: pg.Image,
		Cmd:   system.RecommendedPostgresTuning(sysInfo.TotalRAMMB),
		Env: []string{
			"POSTGRES_USER=" + pg.SuperUser,
			"POSTGRES_PASSWORD=" + pg.SuperPassword,
			"POSTGRES_DB=postgres",
		},
		Labels: map[string]string{
			"runodoo.managed": "true",
			"runodoo.role":    "postgres-shared",
		},
		Binds: []string{
			fmt.Sprintf("%s:/var/lib/postgresql/data", hostPGData),
		},
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	if err := m.docker.StartContainer(ctx, pg.ContainerName); err != nil {
		return err
	}
	return m.waitPostgresReady(ctx, pg.ContainerName, pg.SuperUser)
}

// EnsureSharedRole creates an isolated non-superuser role with CREATEDB privilege on the shared PostgreSQL.
func (m *Manager) EnsureSharedRole(ctx context.Context, username, password string) error {
	if err := m.EnsureSharedPostgres(ctx); err != nil {
		return err
	}
	st := m.store.Get()
	pg := st.SharedPostgres

	sql := fmt.Sprintf(`DO $$
BEGIN
   IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '%s') THEN
      CREATE ROLE "%s" WITH LOGIN CREATEDB PASSWORD '%s';
   ELSE
      ALTER ROLE "%s" WITH LOGIN CREATEDB PASSWORD '%s';
   END IF;
END
$$;`, escapeSQL(username), username, escapeSQL(password), username, escapeSQL(password))

	out, code, err := m.docker.Exec(ctx, pg.ContainerName, []string{
		"psql", "-U", pg.SuperUser, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", sql,
	}, []string{"PGPASSWORD=" + pg.SuperPassword})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("failed to create postgres role %s: %s", username, strings.TrimSpace(out))
	}
	return nil
}

// CreateDedicatedPostgres provisions a dedicated PostgreSQL container for an Odoo instance.
func (m *Manager) CreateDedicatedPostgres(ctx context.Context, inst models.OdooInstance) error {
	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		return err
	}

	image := "postgres:16-alpine"
	if !m.docker.ImageExists(ctx, image) {
		if err := m.docker.PullImage(ctx, image); err != nil {
			return err
		}
	}

	pgDataDir := filepath.Join(m.cfg.InstancesDir(), inst.Name, "pgdata")
	if err := os.MkdirAll(pgDataDir, 0o755); err != nil {
		return err
	}
	hostPGData := filepath.Join(m.cfg.HostInstancesDir(), inst.Name, "pgdata")
	sysInfo := system.CollectSystemInfo(m.cfg.DataDir, m.cfg.Port, true, "")

	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  inst.DBContainerName,
		Image: image,
		Cmd:   system.RecommendedPostgresTuning(sysInfo.TotalRAMMB),
		Env: []string{
			"POSTGRES_USER=" + inst.DBUser,
			"POSTGRES_PASSWORD=" + inst.DBPassword,
			"POSTGRES_DB=postgres",
		},
		Labels: map[string]string{
			"runodoo.managed":  "true",
			"runodoo.role":     "postgres-dedicated",
			"runodoo.instance": inst.Name,
		},
		Binds: []string{
			fmt.Sprintf("%s:/var/lib/postgresql/data", hostPGData),
		},
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	if err := m.docker.StartContainer(ctx, inst.DBContainerName); err != nil {
		return err
	}
	return m.waitPostgresReady(ctx, inst.DBContainerName, inst.DBUser)
}

// ListInstanceDatabases lists all user databases owned by or belonging to an Odoo instance.
func (m *Manager) ListInstanceDatabases(ctx context.Context, inst models.OdooInstance) ([]models.DatabaseInfo, error) {
	pgUser, pgPass := m.adminCredsForInstance(inst)
	query := `SELECT d.datname, pg_catalog.pg_get_userbyid(d.datdba), pg_database_size(d.datname), pg_size_pretty(pg_database_size(d.datname))
FROM pg_database d
WHERE d.datistemplate = false AND d.datname <> 'postgres'`
	if inst.DBMode == models.DBModeShared {
		query += fmt.Sprintf(" AND pg_catalog.pg_get_userbyid(d.datdba) = '%s'", escapeSQL(inst.DBUser))
	}
	query += " ORDER BY d.datname;"

	out, code, err := m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", "postgres", "-At", "-F", "|", "-c", query,
	}, []string{"PGPASSWORD=" + pgPass})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("psql list failed: %s", strings.TrimSpace(out))
	}

	var dbs []models.DatabaseInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		sz, _ := strconv.ParseInt(parts[2], 10, 64)
		dbs = append(dbs, models.DatabaseInfo{
			Name:          parts[0],
			Owner:         parts[1],
			SizeBytes:     sz,
			SizePretty:    parts[3],
			InstanceID:    inst.ID,
			InstanceName:  inst.Name,
			ContainerName: inst.DBContainerName,
		})
	}
	return dbs, nil
}

// CreateDatabase creates a new empty PostgreSQL database owned by the instance's user.
func (m *Manager) CreateDatabase(ctx context.Context, inst models.OdooInstance, dbName string) error {
	if !validDBName.MatchString(dbName) {
		return fmt.Errorf("invalid database name '%s' (use letters, numbers, underscores, hyphens)", dbName)
	}
	pgUser, pgPass := m.adminCredsForInstance(inst)
	sql := fmt.Sprintf(`CREATE DATABASE "%s" OWNER "%s" ENCODING 'UTF8' TEMPLATE template0;`, dbName, inst.DBUser)
	out, code, err := m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", sql,
	}, []string{"PGPASSWORD=" + pgPass})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("create database failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// DuplicateDatabase clones an existing database and its Odoo filestore.
func (m *Manager) DuplicateDatabase(ctx context.Context, inst models.OdooInstance, srcDB, dstDB string) error {
	if !validDBName.MatchString(srcDB) || !validDBName.MatchString(dstDB) {
		return fmt.Errorf("invalid database name")
	}
	pgUser, pgPass := m.adminCredsForInstance(inst)

	// Terminate active connections on source DB so WITH TEMPLATE succeeds
	termSQL := fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();`, escapeSQL(srcDB))
	_, _, _ = m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", "postgres", "-c", termSQL,
	}, []string{"PGPASSWORD=" + pgPass})

	dupSQL := fmt.Sprintf(`CREATE DATABASE "%s" WITH TEMPLATE "%s" OWNER "%s";`, dstDB, srcDB, inst.DBUser)
	out, code, err := m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", dupSQL,
	}, []string{"PGPASSWORD=" + pgPass})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("duplicate database failed: %s", strings.TrimSpace(out))
	}

	// Copy Odoo filestore if present
	srcFS := filepath.Join(m.cfg.InstancesDir(), inst.Name, "data", "filestore", srcDB)
	dstFS := filepath.Join(m.cfg.InstancesDir(), inst.Name, "data", "filestore", dstDB)
	if _, err := os.Stat(srcFS); err == nil {
		_ = copyDir(srcFS, dstFS)
	}
	return nil
}

// DropDatabase terminates connections, drops the database, and cleans up its filestore.
func (m *Manager) DropDatabase(ctx context.Context, inst models.OdooInstance, dbName string) error {
	if !validDBName.MatchString(dbName) {
		return fmt.Errorf("invalid database name")
	}
	pgUser, pgPass := m.adminCredsForInstance(inst)

	termSQL := fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();`, escapeSQL(dbName))
	_, _, _ = m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", "postgres", "-c", termSQL,
	}, []string{"PGPASSWORD=" + pgPass})

	dropSQL := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, dbName)
	out, code, err := m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", dropSQL,
	}, []string{"PGPASSWORD=" + pgPass})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("drop database failed: %s", strings.TrimSpace(out))
	}

	fsPath := filepath.Join(m.cfg.InstancesDir(), inst.Name, "data", "filestore", dbName)
	_ = os.RemoveAll(fsPath)
	return nil
}

// AdminCredsForInstance returns the Postgres superuser/admin credentials for executing DDL/pg_dump.
func (m *Manager) AdminCredsForInstance(inst models.OdooInstance) (string, string) {
	return m.adminCredsForInstance(inst)
}

func (m *Manager) adminCredsForInstance(inst models.OdooInstance) (string, string) {
	if inst.DBMode == models.DBModeShared {
		st := m.store.Get()
		return st.SharedPostgres.SuperUser, st.SharedPostgres.SuperPassword
	}
	return inst.DBUser, inst.DBPassword
}

func (m *Manager) waitPostgresReady(ctx context.Context, containerName, user string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		_, code, err := m.docker.Exec(ctx, containerName, []string{"pg_isready", "-U", user}, nil)
		if err == nil && code == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for postgres container %s to become ready", containerName)
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
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
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

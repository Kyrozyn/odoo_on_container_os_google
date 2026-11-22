package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/runodoo/runodoo/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// Store manages thread-safe persistence of RunOdoo state in a JSON file.
type Store struct {
	mu       sync.RWMutex
	filePath string
	state    models.State
}

// New initializes the store from disk or creates a fresh default state.
func New(filePath, defaultPassword, sharedPGContainer string) (*Store, error) {
	s := &Store{filePath: filePath}

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := s.initDefault(defaultPassword, sharedPGContainer); err != nil {
			return nil, err
		}
		return s, nil
	}

	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, err
	}
	s.state = models.State{
		Settings: models.PanelSettings{
			RootDomain:       ps.Settings.RootDomain,
			PanelSubdomain:   ps.Settings.PanelSubdomain,
			PanelHasSSL:      ps.Settings.PanelHasSSL,
			PanelForceHTTPS:  ps.Settings.PanelForceHTTPS,
			LetsEncryptEmail: ps.Settings.LetsEncryptEmail,
			PasswordHash:     ps.Settings.PasswordHash,
			PasswordChanged:  ps.Settings.PasswordChanged,
			JWTSecret:        ps.Settings.JWTSecret,
		},
		SharedPostgres:  ps.SharedPostgres,
		Instances:       ps.Instances,
		AddonRepos:      ps.AddonRepos,
		Backups:         ps.Backups,
		BackupSchedules: ps.BackupSchedules,
	}
	if s.state.Instances == nil {
		s.state.Instances = []models.OdooInstance{}
	}
	if s.state.AddonRepos == nil {
		s.state.AddonRepos = []models.AddonRepo{}
	}
	if s.state.Backups == nil {
		s.state.Backups = []models.BackupRecord{}
	}
	if s.state.BackupSchedules == nil {
		s.state.BackupSchedules = []models.BackupSchedule{}
	}

	// Ensure required secrets exist even if state file was hand-edited.
	changed := false
	if s.state.Settings.JWTSecret == "" {
		s.state.Settings.JWTSecret = randomHex(32)
		changed = true
	}
	if s.state.Settings.PanelSubdomain == "" {
		s.state.Settings.PanelSubdomain = "panel"
		changed = true
	}
	if s.state.Settings.PasswordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		s.state.Settings.PasswordHash = string(hash)
		changed = true
	}
	if s.state.SharedPostgres.ContainerName == "" {
		s.state.SharedPostgres = models.SharedPostgresConfig{
			ContainerName: sharedPGContainer,
			Image:         "postgres:16-alpine",
			SuperUser:     "runodoo_admin",
			SuperPassword: randomHex(16),
			Port:          5432,
		}
		changed = true
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}

	return s, nil
}

func (s *Store) initDefault(defaultPassword, sharedPGContainer string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	s.state = models.State{
		Settings: models.PanelSettings{
			PanelSubdomain:  "panel",
			PasswordHash:    string(hash),
			PasswordChanged: false,
			JWTSecret:       randomHex(32),
		},
		SharedPostgres: models.SharedPostgresConfig{
			ContainerName: sharedPGContainer,
			Image:         "postgres:16-alpine",
			SuperUser:     "runodoo_admin",
			SuperPassword: randomHex(16),
			Port:          5432,
		},
		Instances:       []models.OdooInstance{},
		AddonRepos:      []models.AddonRepo{},
		Backups:         []models.BackupRecord{},
		BackupSchedules: []models.BackupSchedule{},
	}

	return s.saveLocked()
}

// Get returns a deep copy of the current state for safe reading.
func (s *Store) Get() models.State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := s.state
	cp.Instances = append([]models.OdooInstance(nil), s.state.Instances...)
	cp.AddonRepos = append([]models.AddonRepo(nil), s.state.AddonRepos...)
	cp.Backups = append([]models.BackupRecord(nil), s.state.Backups...)
	cp.BackupSchedules = append([]models.BackupSchedule(nil), s.state.BackupSchedules...)
	return cp
}

// Update atomically mutates state and persists it to disk.
func (s *Store) Update(fn func(st *models.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := fn(&s.state); err != nil {
		return err
	}
	return s.saveLocked()
}

// FindInstance returns an instance by ID or name.
func (s *Store) FindInstance(idOrName string) (models.OdooInstance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, inst := range s.state.Instances {
		if inst.ID == idOrName || inst.Name == idOrName {
			return inst, true
		}
	}
	return models.OdooInstance{}, false
}

// NextAvailablePort finds the next unused HTTP port starting from startPort.
func (s *Store) NextAvailablePort(startPort int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	used := make(map[int]bool)
	for _, inst := range s.state.Instances {
		used[inst.HTTPPort] = true
		used[inst.LongpollingPort] = true
	}
	port := startPort
	for used[port] {
		port++
	}
	return port
}

// persistedState mirrors models.State but includes private auth fields for disk serialization.
type persistedSettings struct {
	RootDomain       string `json:"rootDomain"`
	PanelSubdomain   string `json:"panelSubdomain"`
	PanelHasSSL      bool   `json:"panelHasSsl"`
	PanelForceHTTPS  bool   `json:"panelForceHttps"`
	LetsEncryptEmail string `json:"letsEncryptEmail"`
	PasswordHash     string `json:"passwordHash"`
	PasswordChanged  bool   `json:"passwordChanged"`
	JWTSecret        string `json:"jwtSecret"`
}

type persistedState struct {
	Settings        persistedSettings           `json:"settings"`
	SharedPostgres  models.SharedPostgresConfig `json:"sharedPostgres"`
	Instances       []models.OdooInstance       `json:"instances"`
	AddonRepos      []models.AddonRepo          `json:"addonRepos"`
	Backups         []models.BackupRecord       `json:"backups"`
	BackupSchedules []models.BackupSchedule     `json:"backupSchedules"`
}

func (s *Store) saveLocked() error {
	ps := persistedState{
		Settings: persistedSettings{
			RootDomain:       s.state.Settings.RootDomain,
			PanelSubdomain:   s.state.Settings.PanelSubdomain,
			PanelHasSSL:      s.state.Settings.PanelHasSSL,
			PanelForceHTTPS:  s.state.Settings.PanelForceHTTPS,
			LetsEncryptEmail: s.state.Settings.LetsEncryptEmail,
			PasswordHash:     s.state.Settings.PasswordHash,
			PasswordChanged:  s.state.Settings.PasswordChanged,
			JWTSecret:        s.state.Settings.JWTSecret,
		},
		SharedPostgres:  s.state.SharedPostgres,
		Instances:       s.state.Instances,
		AddonRepos:      s.state.AddonRepos,
		Backups:         s.state.Backups,
		BackupSchedules: s.state.BackupSchedules,
	}

	raw, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

// UnmarshalJSON for loading persistedState cleanly on startup.
func init() {}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

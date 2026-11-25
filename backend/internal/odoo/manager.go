package odoo

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runodoo/runodoo/internal/addons"
	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/database"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/proxy"
	"github.com/runodoo/runodoo/internal/store"
	"github.com/runodoo/runodoo/internal/system"
)

var (
	validInstanceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)
	validPythonSpec   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-\[\],=<>!~]*$`)
	validPythonName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*$`)
)

type CreateInstanceRequest struct {
	Name             string             `json:"name"`
	Version          models.OdooVersion `json:"version"`
	CustomImage      string             `json:"customImage,omitempty"`
	DBMode           models.DBMode      `json:"dbMode"`
	DefaultDBName    string             `json:"defaultDbName,omitempty"`
	AdminPassword    string             `json:"adminPassword,omitempty"`
	HTTPPort         int                `json:"httpPort,omitempty"`
	ExposePort       bool               `json:"exposePort"`
	UseRootSubdomain bool               `json:"useRootSubdomain"`
	CustomDomain      string                 `json:"customDomain,omitempty"`
	AddonRepoIDs      []string               `json:"addonRepoIds,omitempty"`
	RecommendedAddons []string               `json:"recommendedAddons,omitempty"`
	EnvVars           map[string]string      `json:"envVars,omitempty"`
	Resources         *models.ResourceConfig `json:"resources,omitempty"`
}

type UpdateInstanceRequest struct {
	Image             *string                `json:"image,omitempty"`
	ExposePort        *bool                  `json:"exposePort,omitempty"`
	HTTPPort          *int                   `json:"httpPort,omitempty"`
	UseRootSubdomain  *bool                  `json:"useRootSubdomain,omitempty"`
	Domains           []models.DomainConfig  `json:"domains,omitempty"`
	AdminPassword     *string                `json:"adminPassword,omitempty"`
	AddonRepoIDs      []string               `json:"addonRepoIds,omitempty"`
	RecommendedAddons []string               `json:"recommendedAddons,omitempty"`
	EnvVars           map[string]string      `json:"envVars,omitempty"`
	ConfOptions       map[string]string      `json:"confOptions,omitempty"`
	ResetConfOptions  bool                   `json:"resetConfOptions,omitempty"`
	CustomConf        *string                `json:"customConf,omitempty"`
	Resources        *models.ResourceConfig `json:"resources,omitempty"`
	Redeploy         bool                   `json:"redeploy"`
}

type Manager struct {
	cfg     *config.Config
	store   *store.Store
	docker  *docker.Client
	db      *database.Manager
	addons  *addons.Manager
	proxy   *proxy.Manager
	tracker *deployTracker
}

func NewManager(
	cfg *config.Config,
	st *store.Store,
	dc *docker.Client,
	dbm *database.Manager,
	am *addons.Manager,
	pm *proxy.Manager,
) *Manager {
	return &Manager{
		cfg:     cfg,
		store:   st,
		docker:  dc,
		db:      dbm,
		addons:  am,
		proxy:   pm,
		tracker: newDeployTracker(),
	}
}

// GetDeployProgress returns live deployment status and logs for an instance.
func (m *Manager) GetDeployProgress(idOrName string) *DeployProgress {
	if inst, ok := m.store.FindInstance(idOrName); ok {
		return m.tracker.Get(inst.ID)
	}
	return m.tracker.Get(idOrName)
}

// CreateInstance provisions database, odoo.conf, Docker container, and Nginx vhost for a new Odoo instance.
func (m *Manager) CreateInstance(ctx context.Context, req CreateInstanceRequest) (*models.OdooInstance, error) {
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !validInstanceName.MatchString(name) {
		return nil, fmt.Errorf("instance name must be 2-41 lowercase alphanumeric characters or hyphens")
	}
	if _, exists := m.store.FindInstance(name); exists {
		return nil, fmt.Errorf("instance '%s' already exists", name)
	}

	image := resolveImage(req.Version, req.CustomImage)
	if image == "" {
		return nil, fmt.Errorf("unsupported Odoo version '%s' (supported: 17, 18, 19, 20)", req.Version)
	}

	dbMode := req.DBMode
	if dbMode == "" {
		dbMode = models.DBModeShared
	}
	if dbMode != models.DBModeShared && dbMode != models.DBModeDedicated {
		return nil, fmt.Errorf("invalid dbMode '%s' (use 'shared' or 'dedicated')", dbMode)
	}

	httpPort := req.HTTPPort
	if httpPort <= 0 {
		httpPort = m.store.NextAvailablePort(8069)
	}
	lpPort := httpPort + 1000 // internal/optional longpolling port offset

	adminPass := strings.TrimSpace(req.AdminPassword)
	if adminPass == "" {
		adminPass = randomHex(10)
	}

	dbUser := "odoo_" + strings.ReplaceAll(name, "-", "_")
	dbPass := randomHex(12)

	st := m.store.Get()
	dbContainer := st.SharedPostgres.ContainerName
	if dbMode == models.DBModeDedicated {
		dbContainer = "runodoo-pg-" + name
	}

	sysInfo := system.CollectSystemInfo(m.cfg.DataDir, m.cfg.Port, true, "")
	res := system.RecommendedOdooResources(sysInfo.TotalRAMMB, sysInfo.CPUCores)
	if req.Resources != nil {
		res = *req.Resources
	}

	var domains []models.DomainConfig
	if d := normalizeDomain(req.CustomDomain); d != "" {
		domains = append(domains, models.DomainConfig{Domain: d})
	}

	envVars := req.EnvVars
	if envVars == nil {
		envVars = map[string]string{}
	}
	addonIDs := req.AddonRepoIDs
	if addonIDs == nil {
		addonIDs = []string{}
	}
	recAddons := req.RecommendedAddons
	if recAddons == nil {
		recAddons = []string{}
	}

	now := time.Now()
	inst := models.OdooInstance{
		ID:                randomHex(8),
		Name:              name,
		Version:           req.Version,
		Image:             image,
		ContainerName:     "runodoo-app-" + name,
		HTTPPort:          httpPort,
		LongpollingPort:   lpPort,
		ExposePort:        req.ExposePort,
		Domains:           domains,
		UseRootSubdomain:  req.UseRootSubdomain,
		DBMode:            dbMode,
		DBContainerName:   dbContainer,
		DBHost:            dbContainer,
		DBPort:            5432,
		DBUser:            dbUser,
		DBPassword:        dbPass,
		DefaultDBName:     strings.TrimSpace(req.DefaultDBName),
		AdminPassword:     adminPass,
		AddonRepoIDs:      addonIDs,
		RecommendedAddons: recAddons,
		EnvVars:           envVars,
		Resources:         res,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// Save instance immediately so UI can navigate and track live deployment progress
	if err := m.store.Update(func(s *models.State) error {
		s.Instances = append(s.Instances, inst)
		return nil
	}); err != nil {
		return nil, err
	}

	m.tracker.Start(inst.ID, fmt.Sprintf("Starting deployment for %s (Odoo %s Community)...", inst.Name, inst.Version))
	go m.runInitialDeploy(inst)

	return &inst, nil
}

func (m *Manager) runInitialDeploy(inst models.OdooInstance) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// 1. Ensure Docker network
	m.tracker.Step(inst.ID, fmt.Sprintf("Ensuring Docker network '%s'...", m.cfg.DockerNetwork), 12)
	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		m.tracker.Fail(inst.ID, fmt.Errorf("network setup failed: %w", err))
		return
	}

	// 2. Provision PostgreSQL (shared role or dedicated container)
	if inst.DBMode == models.DBModeShared {
		m.tracker.Step(inst.ID, fmt.Sprintf("Provisioning Shared PostgreSQL role '%s'...", inst.DBUser), 25)
		if err := m.db.EnsureSharedRole(ctx, inst.DBUser, inst.DBPassword); err != nil {
			m.tracker.Fail(inst.ID, fmt.Errorf("shared postgres setup failed: %w", err))
			return
		}
		m.tracker.Log(inst.ID, fmt.Sprintf("Shared PostgreSQL ready (%s)", inst.DBContainerName))
	} else {
		m.tracker.Step(inst.ID, fmt.Sprintf("Creating Dedicated PostgreSQL container '%s'...", inst.DBContainerName), 25)
		if err := m.db.CreateDedicatedPostgres(ctx, inst); err != nil {
			m.tracker.Fail(inst.ID, fmt.Errorf("dedicated postgres setup failed: %w", err))
			return
		}
		m.tracker.Log(inst.ID, fmt.Sprintf("Dedicated PostgreSQL ready (%s)", inst.DBContainerName))
	}

	if inst.DefaultDBName != "" {
		m.tracker.Log(inst.ID, fmt.Sprintf("Creating initial database '%s'...", inst.DefaultDBName))
		if err := m.db.CreateDatabase(ctx, inst, inst.DefaultDBName); err != nil {
			m.tracker.Log(inst.ID, fmt.Sprintf("Warning creating initial DB: %v", err))
		}
	}

	// 3. Prepare instance directories & odoo.conf
	m.tracker.Step(inst.ID, "Generating odoo.conf & resolving addons_path...", 42)
	if err := m.writeInstanceFiles(inst); err != nil {
		m.tracker.Fail(inst.ID, fmt.Errorf("failed writing instance config: %w", err))
		return
	}
	if len(inst.RecommendedAddons) > 0 {
		m.tracker.Log(inst.ID, fmt.Sprintf("Selected %d recommended addon(s)", len(inst.RecommendedAddons)))
	}

	// 4. Pull & start Odoo container with live progress
	if err := m.deployContainerWithProgress(ctx, inst); err != nil {
		m.tracker.Fail(inst.ID, err)
		return
	}

	// 5. Sync Nginx reverse proxy
	m.tracker.Step(inst.ID, "Syncing Nginx reverse proxy configuration...", 95)
	if err := m.proxy.SyncAll(ctx); err != nil {
		m.tracker.Log(inst.ID, fmt.Sprintf("Warning syncing Nginx: %v", err))
	}

	m.tracker.Complete(inst.ID)
}

// UpdateInstance modifies an instance's configuration and optionally redeploys its container.
func (m *Manager) UpdateInstance(ctx context.Context, idOrName string, req UpdateInstanceRequest) (*models.OdooInstance, error) {
	// Pre-flight check: detect removed addons and block if any of their modules are still installed in Odoo
	var removedModules []string
	if existing, ok := m.store.FindInstance(idOrName); ok {
		removedModules = m.computeRemovedModules(existing, req.AddonRepoIDs, req.RecommendedAddons)
		if len(removedModules) > 0 {
			installedByDB, err := m.db.FindInstalledModules(ctx, existing, removedModules)
			if err == nil && len(installedByDB) > 0 {
				var parts []string
				for dbName, mods := range installedByDB {
					parts = append(parts, fmt.Sprintf("database '%s': [%s]", dbName, strings.Join(mods, ", ")))
				}
				sort.Strings(parts)
				return nil, fmt.Errorf("tidak bisa detach addon karena modul masih ter-install di %s — silakan uninstall modul tersebut di Odoo terlebih dahulu", strings.Join(parts, "; "))
			}
		}
	}

	var updated models.OdooInstance
	portChanged := false
	err := m.store.Update(func(s *models.State) error {
		for i := range s.Instances {
			if s.Instances[i].ID == idOrName || s.Instances[i].Name == idOrName {
				inst := &s.Instances[i]
				if req.Image != nil && strings.TrimSpace(*req.Image) != "" {
					inst.Image = strings.TrimSpace(*req.Image)
				}
				if req.ExposePort != nil {
					if inst.ExposePort != *req.ExposePort {
						portChanged = true
					}
					inst.ExposePort = *req.ExposePort
				}
				if req.HTTPPort != nil && *req.HTTPPort > 0 {
					if inst.HTTPPort != *req.HTTPPort {
						portChanged = true
					}
					inst.HTTPPort = *req.HTTPPort
				}
				if req.UseRootSubdomain != nil {
					inst.UseRootSubdomain = *req.UseRootSubdomain
				}
				if req.Domains != nil {
					cleaned := make([]models.DomainConfig, 0, len(req.Domains))
					for _, d := range req.Domains {
						if host := normalizeDomain(d.Domain); host != "" {
							d.Domain = host
							cleaned = append(cleaned, d)
						}
					}
					inst.Domains = cleaned
				}
				if req.AdminPassword != nil && *req.AdminPassword != "" {
					inst.AdminPassword = *req.AdminPassword
				}
				if req.AddonRepoIDs != nil {
					inst.AddonRepoIDs = req.AddonRepoIDs
					if inst.ConfOptions != nil {
						delete(inst.ConfOptions, "addons_path")
					}
				}
				if req.RecommendedAddons != nil {
					inst.RecommendedAddons = req.RecommendedAddons
					if inst.ConfOptions != nil {
						delete(inst.ConfOptions, "addons_path")
					}
				}
				if req.EnvVars != nil {
					inst.EnvVars = req.EnvVars
				}
				if req.ResetConfOptions {
					inst.ConfOptions = map[string]string{}
				} else if req.ConfOptions != nil {
					// Avoid pinning addons_path as a static override if it matches the auto-computed value
					autoAddonsPath, _ := m.addons.ComputeAddonsPath(inst.AddonRepoIDs, inst.RecommendedAddons)
					cleaned := make(map[string]string, len(req.ConfOptions))
					for k, v := range req.ConfOptions {
						if k == "addons_path" && strings.TrimSpace(v) == autoAddonsPath {
							continue
						}
						cleaned[k] = v
					}
					inst.ConfOptions = cleaned
					if pw, ok := cleaned["admin_passwd"]; ok && strings.TrimSpace(pw) != "" {
						inst.AdminPassword = strings.TrimSpace(pw)
					}
				}
				if req.CustomConf != nil {
					inst.CustomConf = *req.CustomConf
				}
				if req.Resources != nil {
					inst.Resources = *req.Resources
				}
				inst.UpdatedAt = time.Now()
				updated = *inst
				return nil
			}
		}
		return fmt.Errorf("instance not found")
	})
	if err != nil {
		return nil, err
	}

	if err := m.writeInstanceFiles(updated); err != nil {
		return nil, err
	}
	if req.Redeploy || portChanged {
		shouldUpdateApps := req.AddonRepoIDs != nil || req.RecommendedAddons != nil
		m.tracker.Start(updated.ID, fmt.Sprintf("Redeploying %s...", updated.Name))
		go func(inst models.OdooInstance) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			if err := m.deployContainerWithProgress(ctx, inst); err != nil {
				m.tracker.Fail(inst.ID, err)
				return
			}
			m.tracker.Step(inst.ID, "Syncing Nginx reverse proxy...", 90)
			_ = m.proxy.SyncAll(ctx)
			if shouldUpdateApps {
				m.tracker.Step(inst.ID, "Updating Odoo Apps List & cleaning detached modules...", 95)
				time.Sleep(3 * time.Second)
				updatedDBs, _, err := m.UpdateAppsList(ctx, inst.ID, removedModules, func(line string) {
					m.tracker.Log(inst.ID, line)
				})
				if err != nil {
					m.tracker.Log(inst.ID, fmt.Sprintf("Warning updating Apps List: %v", err))
				} else if len(updatedDBs) == 0 {
					m.tracker.Log(inst.ID, "No initialized databases yet — Apps List will include new addons when a DB is created")
				} else {
					m.tracker.Log(inst.ID, fmt.Sprintf("Updated Apps List on %d database(s): %s", len(updatedDBs), strings.Join(updatedDBs, ", ")))
				}
			}
			m.tracker.Complete(inst.ID)
		}(updated)
		return &updated, nil
	}
	if err := m.proxy.SyncAll(ctx); err != nil {
		return &updated, fmt.Errorf("domain saved, but Nginx proxy failed: %w", err)
	}
	return &updated, nil
}

// DeleteInstance removes the Odoo container, optionally its dedicated Postgres, data dir, and Nginx vhost.
func (m *Manager) DeleteInstance(ctx context.Context, idOrName string, deleteData bool) error {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return fmt.Errorf("instance not found")
	}

	_ = m.docker.RemoveContainer(ctx, inst.ContainerName, true)
	if inst.DBMode == models.DBModeDedicated {
		_ = m.docker.RemoveContainer(ctx, inst.DBContainerName, true)
	}

	if err := m.store.Update(func(s *models.State) error {
		filtered := make([]models.OdooInstance, 0, len(s.Instances))
		for _, item := range s.Instances {
			if item.ID != inst.ID {
				filtered = append(filtered, item)
			}
		}
		s.Instances = filtered
		return nil
	}); err != nil {
		return err
	}

	if deleteData {
		_ = os.RemoveAll(filepath.Join(m.cfg.InstancesDir(), inst.Name))
	}

	_ = m.proxy.SyncAll(ctx)
	return nil
}

// ListWithStatus returns all instances enriched with live Docker status.
func (m *Manager) ListWithStatus(ctx context.Context) []models.InstanceWithStatus {
	st := m.store.Get()
	out := make([]models.InstanceWithStatus, 0, len(st.Instances))
	for _, inst := range st.Instances {
		out = append(out, m.enrichStatus(ctx, inst, st.Settings))
	}
	return out
}

// GetWithStatus returns a single instance with live Docker status.
func (m *Manager) GetWithStatus(ctx context.Context, idOrName string) (*models.InstanceWithStatus, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return nil, fmt.Errorf("instance not found")
	}
	st := m.store.Get()
	res := m.enrichStatus(ctx, inst, st.Settings)
	return &res, nil
}

// UpdateAppsList runs Odoo's ir.module.module.update_list() on every database of an instance
// and unlinks uninstalled modules that were detached from the instance.
func (m *Manager) UpdateAppsList(ctx context.Context, idOrName string, removedModules []string, onLog func(string)) ([]string, string, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return nil, "", fmt.Errorf("instance not found")
	}

	dbs, err := m.db.ListInstanceDatabases(ctx, inst)
	if err != nil {
		return nil, "", fmt.Errorf("failed listing databases: %w", err)
	}
	if len(dbs) == 0 {
		return []string{}, "No databases found on this instance", nil
	}

	pyRemovedList := "[]"
	if len(removedModules) > 0 {
		quoted := make([]string, 0, len(removedModules))
		for _, mod := range removedModules {
			quoted = append(quoted, strconv.Quote(mod))
		}
		pyRemovedList = "[" + strings.Join(quoted, ", ") + "]"
	}

	var updated []string
	var allOut strings.Builder
	for _, db := range dbs {
		if onLog != nil {
			onLog(fmt.Sprintf("Running update_list() on database '%s'...", db.Name))
		}
		script := fmt.Sprintf(`odoo shell -c /etc/odoo/odoo.conf -d %q --no-http <<'PYEOF'
added, updated_cnt = env['ir.module.module'].update_list()
removed_names = %s
unlinked_cnt = 0
if removed_names:
    stale = env['ir.module.module'].search([('name', 'in', removed_names), ('state', '=', 'uninstalled')])
    unlinked_cnt = len(stale)
    if stale:
        stale.unlink()
env.cr.commit()
print(f"RUNODOO_UPDATE_LIST_OK added={added} updated={updated_cnt} unlinked={unlinked_cnt}")
PYEOF`, db.Name, pyRemovedList)

		out, code, execErr := m.docker.Exec(ctx, inst.ContainerName, []string{"sh", "-c", script}, nil)
		allOut.WriteString(fmt.Sprintf("--- %s ---\n%s\n", db.Name, out))
		if execErr != nil || code != 0 {
			if onLog != nil {
				onLog(fmt.Sprintf("Skipped/failed on '%s' (exit %d): may not be an initialized Odoo DB", db.Name, code))
			}
			continue
		}
		updated = append(updated, db.Name)
		if onLog != nil {
			if len(removedModules) > 0 {
				onLog(fmt.Sprintf("✓ Apps List updated & detached modules cleaned on '%s'", db.Name))
			} else {
				onLog(fmt.Sprintf("✓ Apps List updated on '%s'", db.Name))
			}
		}
	}

	if updated == nil {
		updated = []string{}
	}
	return updated, allOut.String(), nil
}

// StalledModuleInfo describes an Odoo module stuck in a pending state (to install/to upgrade/to remove).
type StalledModuleInfo struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	OnDisk bool   `json:"onDisk"`
}

// CleanMissingModulesResult reports missing/stalled module cleanup or check results for a single Odoo database.
type CleanMissingModulesResult struct {
	DBName           string              `json:"dbName"`
	Status           string              `json:"status"` // "ok" or "skipped"
	Message          string              `json:"message,omitempty"`
	Mode             string              `json:"mode,omitempty"`
	TotalOnDisk      int                 `json:"totalOnDisk"`
	TotalInDB        int                 `json:"totalInDb"`
	DetectedMissing  []string            `json:"detectedMissing"`
	Removed          []string            `json:"removed"`
	SkippedInstalled []string            `json:"skippedInstalled"`
	StalledModules   []StalledModuleInfo `json:"stalledModules"`
}

// CleanMissingModules checks ir.module.module records against modules present on disk (addons_path)
// according to mode:
//   - "all": scans & removes missing uninstalled/uninstallable records across all addons
//   - "custom": scans & removes missing uninstalled/uninstallable records for custom/extra/recommended modules only
//   - "stalled": read-only check for modules stuck in pending states or missing while still installed
func (m *Manager) CleanMissingModules(ctx context.Context, idOrName, mode string) ([]CleanMissingModulesResult, string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "all"
	}
	switch mode {
	case "all", "custom", "stalled":
	default:
		return nil, "", fmt.Errorf("invalid mode %q: must be all, custom, or stalled", mode)
	}

	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return nil, "", fmt.Errorf("instance not found")
	}

	dbs, err := m.db.ListInstanceDatabases(ctx, inst)
	if err != nil {
		return nil, "", fmt.Errorf("failed listing databases: %w", err)
	}
	if len(dbs) == 0 {
		return []CleanMissingModulesResult{}, "No databases found on this instance", nil
	}

	results := make([]CleanMissingModulesResult, 0, len(dbs))
	var allOut strings.Builder
	for _, db := range dbs {
		script := fmt.Sprintf(`odoo shell -c /etc/odoo/odoo.conf -d %q --no-http <<'PYEOF'
import json, os
import odoo
from odoo.tools import config as odoo_config

MODE = %q
PENDING_STATES = ('to install', 'to upgrade', 'to remove')
ODOO_AUTHORS = {'odoo s.a.', 'odoo sa', 'odoo', 'openerp sa', 'openerp s.a.', 'tiny'}

odoo_pkg_dir = os.path.realpath(os.path.dirname(odoo.__file__))

def is_builtin_path(p):
    rp = os.path.realpath(p)
    return (
        rp == odoo_pkg_dir
        or rp.startswith(odoo_pkg_dir + os.sep)
        or rp.startswith('/usr/lib/')
        or rp.startswith('/usr/share/')
    )

def scan_dir(path):
    found = set()
    if path and os.path.isdir(path):
        for entry in os.listdir(path):
            mod_dir = os.path.join(path, entry)
            if os.path.isfile(os.path.join(mod_dir, '__manifest__.py')) or os.path.isfile(os.path.join(mod_dir, '__openerp__.py')):
                found.add(entry)
    return found

builtin_on_disk = set()
custom_on_disk = set()
for p in (odoo_config.get('addons_path') or '').split(','):
    p = p.strip()
    if not p:
        continue
    mods = scan_dir(p)
    if is_builtin_path(p):
        builtin_on_disk.update(mods)
    else:
        custom_on_disk.update(mods)

builtin_on_disk.update(scan_dir(os.path.join(odoo_pkg_dir, 'addons')))

try:
    from odoo.modules.module import get_modules
    all_on_disk = set(get_modules()) | builtin_on_disk | custom_on_disk
except Exception:
    all_on_disk = builtin_on_disk | custom_on_disk

all_mods = env['ir.module.module'].search([])

base_xml_mod_ids = set(
    env['ir.model.data'].search([
        ('model', '=', 'ir.module.module'),
        ('module', '=', 'base'),
    ]).mapped('res_id')
)

def is_builtin_module(m):
    if m.name in custom_on_disk:
        return False
    if m.name in builtin_on_disk:
        return True
    if bool(getattr(m, 'to_buy', False)):
        return True
    if m.id in base_xml_mod_ids:
        return True
    if (m.author or '').strip().lower() in ODOO_AUTHORS:
        return True
    if (m.license or '').strip().upper() == 'OEEL-1':
        return True
    return False

if MODE == 'stalled':
    stalled_recs = all_mods.filtered(lambda m: m.state in PENDING_STATES)
    stalled_list = sorted(
        [{"name": m.name, "state": m.state, "onDisk": m.name in all_on_disk} for m in stalled_recs],
        key=lambda x: (x["state"], x["name"]),
    )
    missing_installed = all_mods.filtered(lambda m: m.name not in all_on_disk and m.state == 'installed')
    skipped_names = sorted(missing_installed.mapped('name'))
    missing_names = sorted(set(skipped_names) | set(m.name for m in stalled_recs if m.name not in all_on_disk))
    removed_names = []
    total_on_disk = len(all_on_disk)
    total_in_db = len(all_mods)
else:
    target_mods = all_mods.filtered(lambda m: not is_builtin_module(m)) if MODE == 'custom' else all_mods
    missing = target_mods.filtered(lambda m: m.name not in all_on_disk)
    to_remove = missing.filtered(lambda m: m.state in ('uninstalled', 'uninstallable'))
    skipped = missing.filtered(lambda m: m.state not in ('uninstalled', 'uninstallable'))
    stalled_recs = target_mods.filtered(lambda m: m.state in PENDING_STATES)
    stalled_list = sorted(
        [{"name": m.name, "state": m.state, "onDisk": m.name in all_on_disk} for m in stalled_recs],
        key=lambda x: (x["state"], x["name"]),
    )
    missing_names = sorted(missing.mapped('name'))
    removed_names = sorted(to_remove.mapped('name'))
    skipped_names = sorted(skipped.mapped('name'))
    if to_remove:
        to_remove.unlink()
        env.cr.commit()
    total_on_disk = len(custom_on_disk) if MODE == 'custom' else len(all_on_disk)
    total_in_db = len(target_mods)

print("RUNODOO_CLEAN_MISSING_OK:" + json.dumps({
    "totalOnDisk": total_on_disk,
    "totalInDb": total_in_db,
    "detectedMissing": missing_names,
    "removed": removed_names,
    "skippedInstalled": skipped_names,
    "stalledModules": stalled_list,
}))
PYEOF`, db.Name, mode)

		out, code, execErr := m.docker.Exec(ctx, inst.ContainerName, []string{"sh", "-c", script}, nil)
		allOut.WriteString(fmt.Sprintf("--- %s ---\n%s\n", db.Name, out))
		if execErr != nil || code != 0 {
			results = append(results, CleanMissingModulesResult{
				DBName:           db.Name,
				Status:           "skipped",
				Message:          "Skipped (bukan database Odoo terinisialisasi atau gagal mengakses odoo shell)",
				Mode:             mode,
				DetectedMissing:  []string{},
				Removed:          []string{},
				SkippedInstalled: []string{},
				StalledModules:   []StalledModuleInfo{},
			})
			continue
		}

		const prefix = "RUNODOO_CLEAN_MISSING_OK:"
		var parsed struct {
			TotalOnDisk      int                 `json:"totalOnDisk"`
			TotalInDB        int                 `json:"totalInDb"`
			DetectedMissing  []string            `json:"detectedMissing"`
			Removed          []string            `json:"removed"`
			SkippedInstalled []string            `json:"skippedInstalled"`
			StalledModules   []StalledModuleInfo `json:"stalledModules"`
		}
		found := false
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, prefix) {
				payload := strings.TrimPrefix(line, prefix)
				if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
					found = true
				}
				break
			}
		}
		if !found {
			results = append(results, CleanMissingModulesResult{
				DBName:           db.Name,
				Status:           "skipped",
				Message:          "Skipped (bukan database Odoo terinisialisasi atau output tidak terbaca)",
				Mode:             mode,
				DetectedMissing:  []string{},
				Removed:          []string{},
				SkippedInstalled: []string{},
				StalledModules:   []StalledModuleInfo{},
			})
			continue
		}
		if parsed.DetectedMissing == nil {
			parsed.DetectedMissing = []string{}
		}
		if parsed.Removed == nil {
			parsed.Removed = []string{}
		}
		if parsed.SkippedInstalled == nil {
			parsed.SkippedInstalled = []string{}
		}
		if parsed.StalledModules == nil {
			parsed.StalledModules = []StalledModuleInfo{}
		}
		results = append(results, CleanMissingModulesResult{
			DBName:           db.Name,
			Status:           "ok",
			Mode:             mode,
			TotalOnDisk:      parsed.TotalOnDisk,
			TotalInDB:        parsed.TotalInDB,
			DetectedMissing:  parsed.DetectedMissing,
			Removed:          parsed.Removed,
			SkippedInstalled: parsed.SkippedInstalled,
			StalledModules:   parsed.StalledModules,
		})
	}

	return results, allOut.String(), nil
}

// computeRemovedModules returns technical module names belonging to addon repos or recommended addons
// that are present in `existing` but removed in the incoming update request.
func (m *Manager) computeRemovedModules(existing models.OdooInstance, nextRepoIDs []string, nextRecs []string) []string {
	removedSet := map[string]bool{}
	st := m.store.Get()

	if nextRepoIDs != nil {
		nextMap := make(map[string]bool, len(nextRepoIDs))
		for _, id := range nextRepoIDs {
			nextMap[id] = true
		}
		repoMap := make(map[string]models.AddonRepo, len(st.AddonRepos))
		for _, r := range st.AddonRepos {
			repoMap[r.ID] = r
		}
		for _, oldID := range existing.AddonRepoIDs {
			if !nextMap[oldID] {
				if repo, ok := repoMap[oldID]; ok {
					for _, mod := range repo.Modules {
						if mod.TechnicalName != "" {
							removedSet[mod.TechnicalName] = true
						}
					}
				}
			}
		}
	}

	if nextRecs != nil {
		nextRecMap := make(map[string]bool, len(nextRecs))
		for _, r := range nextRecs {
			nextRecMap[r] = true
		}
		allRecs := m.addons.ListRecommendedAddons()
		recByKey := map[string]string{}
		for _, mod := range allRecs {
			recByKey[mod.RelPath] = mod.TechnicalName
			recByKey[mod.TechnicalName] = mod.TechnicalName
		}
		for _, oldRec := range existing.RecommendedAddons {
			tech := recByKey[oldRec]
			if tech == "" {
				tech = filepath.Base(oldRec)
			}
			if !nextRecMap[oldRec] && !nextRecMap[tech] {
				removedSet[tech] = true
			}
		}
	}

	out := make([]string, 0, len(removedSet))
	for k := range removedSet {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// UpgradeModules runs `odoo -u <modules> --stop-after-init --no-http` inside the running Odoo container.
func (m *Manager) UpgradeModules(ctx context.Context, idOrName, dbName string, modules []string) (string, error) {
	var buf bytes.Buffer
	code, err := m.UpgradeModulesStream(ctx, idOrName, dbName, modules, &buf)
	out := buf.String()
	if err != nil {
		return out, err
	}
	if code != 0 {
		msg := strings.TrimSpace(out)
		if len(msg) > 400 {
			msg = "..." + msg[len(msg)-400:]
		}
		if msg != "" {
			return out, fmt.Errorf("module upgrade failed (exit %d): %s", code, msg)
		}
		return out, fmt.Errorf("module upgrade failed (exit %d)", code)
	}
	return out, nil
}

// UpgradeModulesStream runs `odoo -u <modules> --stop-after-init --no-http` and streams combined output to dst.
func (m *Manager) UpgradeModulesStream(ctx context.Context, idOrName, dbName string, modules []string, dst io.Writer) (int, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return -1, fmt.Errorf("instance not found")
	}
	if dbName == "" || len(modules) == 0 {
		return -1, fmt.Errorf("database name and at least one module are required")
	}
	modList := strings.Join(modules, ",")
	return m.docker.ExecStreamCombined(ctx, inst.ContainerName, []string{
		"odoo", "-c", "/etc/odoo/odoo.conf", "-d", dbName, "-u", modList,
		"--stop-after-init", "--no-http", "--workers=0", "--max-cron-threads=0",
	}, nil, dst)
}

// ExecStream runs a shell command inside the running Odoo container and streams combined output to dst.
func (m *Manager) ExecStream(ctx context.Context, idOrName, command, cwd string, dst io.Writer) (int, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return -1, fmt.Errorf("instance not found")
	}
	if strings.TrimSpace(command) == "" {
		return -1, fmt.Errorf("command is required")
	}
	env := []string{"RUNODOO_CMD=" + command}
	if strings.TrimSpace(cwd) != "" {
		env = append(env, "RUNODOO_CWD="+cwd)
	}
	script := `if [ -n "$RUNODOO_CWD" ]; then cd "$RUNODOO_CWD" 2>/dev/null || true; fi; eval "$RUNODOO_CMD"; __ec=$?; printf '\n__RUNODOO_PWD__:%s\n' "$(pwd)"; exit $__ec`
	return m.docker.ExecStreamCombined(ctx, inst.ContainerName, []string{"sh", "-c", script}, env, dst)
}

// OpenTerminal opens an interactive PTY shell inside the running Odoo container.
func (m *Manager) OpenTerminal(ctx context.Context, idOrName string, rows, cols int) (string, net.Conn, *bufio.Reader, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return "", nil, nil, fmt.Errorf("instance not found")
	}
	return m.docker.ExecInteractive(
		ctx,
		inst.ContainerName,
		[]string{"sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"},
		[]string{"TERM=xterm-256color"},
		rows,
		cols,
	)
}

// ResizeTerminal resizes the PTY of an active container terminal session.
func (m *Manager) ResizeTerminal(ctx context.Context, execID string, rows, cols int) error {
	return m.docker.ResizeExec(ctx, execID, rows, cols)
}

// PythonPackageInfo represents an installed Python package inside an Odoo container.
type PythonPackageInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Location string `json:"location,omitempty"`
	IsCustom bool   `json:"isCustom"`
}

// PythonEnvInfo reports the Python runtime details and installed packages of an Odoo container.
type PythonEnvInfo struct {
	PythonVersion string              `json:"pythonVersion"`
	PipAvailable  bool                `json:"pipAvailable"`
	PipVersion    string              `json:"pipVersion,omitempty"`
	UserSite      string              `json:"userSite"`
	Packages      []PythonPackageInfo `json:"packages"`
}

func parsePythonSpecs(inputs []string, uninstall bool) ([]string, error) {
	seen := make(map[string]bool)
	var specs []string
	for _, raw := range inputs {
		for _, line := range strings.Split(raw, "\n") {
			if idx := strings.IndexByte(line, '#'); idx != -1 {
				line = line[:idx]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			for _, tok := range strings.Fields(line) {
				if strings.HasPrefix(tok, "-") {
					return nil, fmt.Errorf("invalid package specification %q: flags are not allowed", tok)
				}
				if uninstall {
					if !validPythonName.MatchString(tok) {
						return nil, fmt.Errorf("invalid package name %q", tok)
					}
				} else {
					if !validPythonSpec.MatchString(tok) {
						return nil, fmt.Errorf("invalid package specification %q", tok)
					}
				}
				key := strings.ToLower(tok)
				if !seen[key] {
					seen[key] = true
					specs = append(specs, tok)
				}
			}
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("at least one Python package name is required")
	}
	return specs, nil
}

// ListPythonPackages inspects Python, pip, and installed packages inside an Odoo container.
func (m *Manager) ListPythonPackages(ctx context.Context, idOrName string) (*PythonEnvInfo, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return nil, fmt.Errorf("instance not found")
	}

	const pyScript = `import json, os, site, sys
import importlib.metadata as ilm

py_ver = sys.version.split()[0]
pip_ok = False
pip_ver = ""
try:
    import pip
    pip_ok = True
    pip_ver = getattr(pip, "__version__", "")
except Exception:
    pass

user_site = site.getusersitepackages() or ""
user_site_real = os.path.realpath(user_site) if user_site else ""

pkgs = {}
for d in ilm.distributions():
    name = d.metadata.get("Name") or getattr(d, "name", "") or ""
    if not name:
        continue
    ver = d.version or ""
    loc = ""
    try:
        loc = str(d.locate_file(""))
    except Exception:
        loc = ""
    loc_real = os.path.realpath(loc) if loc else ""
    is_custom = bool(user_site_real and (loc_real == user_site_real or loc_real.startswith(user_site_real + os.sep)))
    key = name.lower()
    if key not in pkgs or (is_custom and not pkgs[key]["isCustom"]):
        pkgs[key] = {
            "name": name,
            "version": ver,
            "location": loc,
            "isCustom": is_custom,
        }

ordered = sorted(pkgs.values(), key=lambda x: x["name"].lower())
print("RUNODOO_PY_LIST_OK:" + json.dumps({
    "pythonVersion": py_ver,
    "pipAvailable": pip_ok,
    "pipVersion": pip_ver,
    "userSite": user_site,
    "packages": ordered,
}))`

	out, code, err := m.docker.Exec(ctx, inst.ContainerName, []string{"python3", "-c", pyScript}, []string{"HOME=/var/lib/odoo"})
	if err != nil {
		return nil, fmt.Errorf("failed inspecting container Python environment (is the container running?): %w", err)
	}
	if code != 0 {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = fmt.Sprintf("python3 exited with code %d", code)
		}
		return nil, fmt.Errorf("failed listing Python packages: %s", msg)
	}

	const prefix = "RUNODOO_PY_LIST_OK:"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			var info PythonEnvInfo
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &info); err != nil {
				return nil, fmt.Errorf("failed parsing Python package list: %w", err)
			}
			if info.Packages == nil {
				info.Packages = []PythonPackageInfo{}
			}
			return &info, nil
		}
	}
	return nil, fmt.Errorf("unexpected output when listing Python packages")
}

// InstallPythonPackages installs or upgrades Python packages inside an Odoo container
// under /var/lib/odoo/.local (persistent across container restarts/redeploys).
func (m *Manager) InstallPythonPackages(ctx context.Context, idOrName string, rawSpecs []string, upgrade bool) (string, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return "", fmt.Errorf("instance not found")
	}
	specs, err := parsePythonSpecs(rawSpecs, false)
	if err != nil {
		return "", err
	}

	upgradeFlag := ""
	if upgrade {
		upgradeFlag = " --upgrade"
	}
	script := `BSP=""
if python3 -m pip install --help 2>/dev/null | grep -q -- '--break-system-packages'; then
  BSP="--break-system-packages"
fi
exec python3 -m pip install --user $BSP --no-warn-script-location` + upgradeFlag + ` "$@"`

	cmd := append([]string{"sh", "-c", script, "--"}, specs...)
	out, code, execErr := m.docker.Exec(ctx, inst.ContainerName, cmd, []string{"HOME=/var/lib/odoo"})
	if execErr != nil {
		return out, fmt.Errorf("failed running pip install (is the container running?): %w", execErr)
	}
	if code != 0 {
		return out, fmt.Errorf("pip install failed (exit %d)", code)
	}
	return out, nil
}

// UninstallPythonPackages removes user-installed Python packages from an Odoo container.
func (m *Manager) UninstallPythonPackages(ctx context.Context, idOrName string, rawNames []string) (string, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return "", fmt.Errorf("instance not found")
	}
	names, err := parsePythonSpecs(rawNames, true)
	if err != nil {
		return "", err
	}

	const script = `BSP=""
if python3 -m pip uninstall --help 2>/dev/null | grep -q -- '--break-system-packages'; then
  BSP="--break-system-packages"
fi
exec python3 -m pip uninstall -y $BSP "$@"`

	cmd := append([]string{"sh", "-c", script, "--"}, names...)
	out, code, execErr := m.docker.Exec(ctx, inst.ContainerName, cmd, []string{"HOME=/var/lib/odoo"})
	if execErr != nil {
		return out, fmt.Errorf("failed running pip uninstall (is the container running?): %w", execErr)
	}
	if code != 0 {
		return out, fmt.Errorf("pip uninstall failed (exit %d)", code)
	}
	return out, nil
}

// ReadOdooConf returns the generated odoo.conf content for an instance.
func (m *Manager) ReadOdooConf(inst models.OdooInstance) string {
	p := filepath.Join(m.cfg.InstancesDir(), inst.Name, "config", "odoo.conf")
	raw, err := os.ReadFile(p)
	if err != nil {
		return m.renderOdooConf(inst)
	}
	return string(raw)
}

func (m *Manager) enrichStatus(ctx context.Context, inst models.OdooInstance, settings models.PanelSettings) models.InstanceWithStatus {
	res := models.InstanceWithStatus{
		OdooInstance: inst,
		Status:       "not_found",
		StateText:    "Not created",
		DBStatus:     "unknown",
	}

	if prog := m.tracker.Get(inst.ID); prog.Status == "deploying" {
		res.Status = "deploying"
		res.StateText = prog.Step
	} else if insp, err := m.docker.InspectContainer(ctx, inst.ContainerName); err == nil {
		res.ContainerID = insp.ID
		res.Status = insp.State.Status
		if insp.State.Running {
			res.StateText = "Running"
		} else if insp.State.Restarting {
			res.StateText = "Restarting"
		} else {
			res.StateText = fmt.Sprintf("Stopped (exit %d)", insp.State.ExitCode)
		}
	}

	if dbInsp, err := m.docker.InspectContainer(ctx, inst.DBContainerName); err == nil {
		res.DBStatus = dbInsp.State.Status
	}

	// Determine primary clickable URL
	if len(inst.Domains) > 0 && normalizeDomain(inst.Domains[0].Domain) != "" {
		scheme := "http"
		if inst.Domains[0].HasSSL {
			scheme = "https"
		}
		res.PrimaryURL = fmt.Sprintf("%s://%s", scheme, normalizeDomain(inst.Domains[0].Domain))
	} else if inst.UseRootSubdomain && normalizeDomain(settings.RootDomain) != "" {
		scheme := "http"
		if inst.RootSubdomainSSL {
			scheme = "https"
		}
		res.PrimaryURL = fmt.Sprintf("%s://%s.%s", scheme, inst.Name, normalizeDomain(settings.RootDomain))
	} else if inst.ExposePort {
		res.PrimaryURL = fmt.Sprintf(":%d", inst.HTTPPort)
	}
	return res
}

func normalizeDomain(raw string) string {
	d := strings.TrimSpace(strings.ToLower(raw))
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	if idx := strings.IndexByte(d, '/'); idx != -1 {
		d = d[:idx]
	}
	return strings.TrimSpace(d)
}

func (m *Manager) writeInstanceFiles(inst models.OdooInstance) error {
	baseDir := filepath.Join(m.cfg.InstancesDir(), inst.Name)
	confDir := filepath.Join(baseDir, "config")
	dataDir := filepath.Join(baseDir, "data")
	extraAddons := filepath.Join(baseDir, "extra-addons")

	for _, d := range []string{confDir, dataDir, extraAddons} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			return err
		}
		_ = os.Chmod(d, 0o777)
	}

	confContent := m.renderOdooConf(inst)
	return os.WriteFile(filepath.Join(confDir, "odoo.conf"), []byte(confContent), 0o644)
}

// StandardOdooConfKeys defines the canonical display order for standard odoo.conf keys.
var StandardOdooConfKeys = []string{
	"http_interface",
	"http_port",
	"gevent_port",
	"admin_passwd",
	"db_host",
	"db_port",
	"db_user",
	"db_password",
	"addons_path",
	"data_dir",
	"proxy_mode",
	"list_db",
	"workers",
	"limit_memory_soft",
	"limit_memory_hard",
	"limit_time_cpu",
	"limit_time_real",
	"max_cron_threads",
}

// EffectiveConfOptions returns the merged map of default odoo.conf options + user overrides.
func (m *Manager) EffectiveConfOptions(inst models.OdooInstance) map[string]string {
	addonsPath, _ := m.addons.ComputeAddonsPath(inst.AddonRepoIDs, inst.RecommendedAddons)
	opts := map[string]string{
		"http_interface":    "0.0.0.0",
		"http_port":         "8069",
		"gevent_port":       "8072",
		"admin_passwd":      inst.AdminPassword,
		"db_host":           inst.DBHost,
		"db_port":           strconv.Itoa(inst.DBPort),
		"db_user":           inst.DBUser,
		"db_password":       inst.DBPassword,
		"addons_path":       addonsPath,
		"data_dir":          "/var/lib/odoo",
		"proxy_mode":        "True",
		"list_db":           "True",
		"workers":           strconv.Itoa(inst.Resources.Workers),
		"limit_memory_soft": strconv.FormatInt(inst.Resources.LimitMemorySoft, 10),
		"limit_memory_hard": strconv.FormatInt(inst.Resources.LimitMemoryHard, 10),
		"limit_time_cpu":    strconv.Itoa(inst.Resources.LimitTimeCPU),
		"limit_time_real":   strconv.Itoa(inst.Resources.LimitTimeReal),
		"max_cron_threads":  strconv.Itoa(inst.Resources.MaxCronThreads),
	}
	for k, v := range inst.ConfOptions {
		cleanKey := strings.TrimSpace(k)
		if cleanKey == "" {
			continue
		}
		opts[cleanKey] = strings.TrimSpace(v)
	}
	return opts
}

func (m *Manager) renderOdooConf(inst models.OdooInstance) string {
	opts := m.EffectiveConfOptions(inst)
	seen := make(map[string]bool, len(StandardOdooConfKeys))

	var sb strings.Builder
	sb.WriteString("[options]\n")
	for _, k := range StandardOdooConfKeys {
		if v, ok := opts[k]; ok {
			sb.WriteString(fmt.Sprintf("%s = %s\n", k, v))
			seen[k] = true
		}
	}
	// Render any additional user-defined options in sorted order
	var extraKeys []string
	for k := range opts {
		if !seen[k] {
			extraKeys = append(extraKeys, k)
		}
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		sb.WriteString(fmt.Sprintf("%s = %s\n", k, opts[k]))
	}

	if strings.TrimSpace(inst.CustomConf) != "" {
		sb.WriteString("\n# Additional custom directives\n")
		sb.WriteString(strings.TrimSpace(inst.CustomConf) + "\n")
	}
	return sb.String()
}

func (m *Manager) deployContainerWithProgress(ctx context.Context, inst models.OdooInstance) error {
	if !m.docker.ImageExists(ctx, inst.Image) {
		m.tracker.Step(inst.ID, fmt.Sprintf("Pulling Docker image %s (this may take a few minutes)...", inst.Image), 55)
		if err := m.docker.PullImageWithProgress(ctx, inst.Image, func(line string) {
			m.tracker.Log(inst.ID, line)
		}); err != nil {
			return fmt.Errorf("failed pulling image %s: %w", inst.Image, err)
		}
	} else {
		m.tracker.Step(inst.ID, fmt.Sprintf("Docker image %s already cached locally", inst.Image), 70)
	}

	m.tracker.Step(inst.ID, fmt.Sprintf("Creating & starting container '%s'...", inst.ContainerName), 82)
	// Remove existing container if redeploying
	_ = m.docker.RemoveContainer(ctx, inst.ContainerName, true)

	_, addonBinds := m.addons.ComputeAddonsPath(inst.AddonRepoIDs, inst.RecommendedAddons)
	hostBase := filepath.Join(m.cfg.HostInstancesDir(), inst.Name)
	binds := []string{
		fmt.Sprintf("%s/config/odoo.conf:/etc/odoo/odoo.conf:ro", hostBase),
		fmt.Sprintf("%s/data:/var/lib/odoo", hostBase),
		fmt.Sprintf("%s/extra-addons:/mnt/extra-addons", hostBase),
	}
	binds = append(binds, addonBinds...)

	ports := map[string]string{}
	if inst.ExposePort && inst.HTTPPort > 0 {
		ports["8069/tcp"] = strconv.Itoa(inst.HTTPPort)
	}

	env := []string{
		"HOST=" + inst.DBHost,
		"USER=" + inst.DBUser,
		"PASSWORD=" + inst.DBPassword,
	}
	for k, v := range inst.EnvVars {
		env = append(env, k+"="+v)
	}

	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  inst.ContainerName,
		Image: inst.Image,
		Env:   env,
		Labels: map[string]string{
			"runodoo.managed":  "true",
			"runodoo.role":     "odoo",
			"runodoo.instance": inst.Name,
			"runodoo.version":  string(inst.Version),
		},
		Binds:         binds,
		PortMappings:  ports,
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	if err := m.docker.StartContainer(ctx, inst.ContainerName); err != nil {
		return err
	}
	m.tracker.Log(inst.ID, fmt.Sprintf("Container '%s' started successfully", inst.ContainerName))
	return nil
}

func resolveImage(version models.OdooVersion, customImage string) string {
	if strings.TrimSpace(customImage) != "" {
		return strings.TrimSpace(customImage)
	}
	for _, v := range models.SupportedOdooVersions {
		if v.Version == version {
			return v.DefaultImage
		}
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

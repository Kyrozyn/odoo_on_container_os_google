package odoo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

var validInstanceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)

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
	CustomConf       *string                `json:"customConf,omitempty"`
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
	var updated models.OdooInstance
	err := m.store.Update(func(s *models.State) error {
		for i := range s.Instances {
			if s.Instances[i].ID == idOrName || s.Instances[i].Name == idOrName {
				inst := &s.Instances[i]
				if req.Image != nil && strings.TrimSpace(*req.Image) != "" {
					inst.Image = strings.TrimSpace(*req.Image)
				}
				if req.ExposePort != nil {
					inst.ExposePort = *req.ExposePort
				}
				if req.HTTPPort != nil && *req.HTTPPort > 0 {
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
				}
				if req.RecommendedAddons != nil {
					inst.RecommendedAddons = req.RecommendedAddons
				}
				if req.EnvVars != nil {
					inst.EnvVars = req.EnvVars
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
	if req.Redeploy {
		m.tracker.Start(updated.ID, fmt.Sprintf("Redeploying %s...", updated.Name))
		go func(inst models.OdooInstance) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			if err := m.deployContainerWithProgress(ctx, inst); err != nil {
				m.tracker.Fail(inst.ID, err)
				return
			}
			m.tracker.Step(inst.ID, "Syncing Nginx reverse proxy...", 95)
			_ = m.proxy.SyncAll(ctx)
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

// UpgradeModules runs `odoo -u <modules> --stop-after-init` inside the running Odoo container.
func (m *Manager) UpgradeModules(ctx context.Context, idOrName, dbName string, modules []string) (string, error) {
	inst, ok := m.store.FindInstance(idOrName)
	if !ok {
		return "", fmt.Errorf("instance not found")
	}
	if dbName == "" || len(modules) == 0 {
		return "", fmt.Errorf("database name and at least one module are required")
	}
	modList := strings.Join(modules, ",")
	out, code, err := m.docker.Exec(ctx, inst.ContainerName, []string{
		"odoo", "-c", "/etc/odoo/odoo.conf", "-d", dbName, "-u", modList, "--stop-after-init",
	}, nil)
	if err != nil {
		return out, err
	}
	if code != 0 {
		return out, fmt.Errorf("module upgrade failed (exit %d)", code)
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

func (m *Manager) renderOdooConf(inst models.OdooInstance) string {
	addonsPath, _ := m.addons.ComputeAddonsPath(inst.AddonRepoIDs, inst.RecommendedAddons)

	var sb strings.Builder
	sb.WriteString("[options]\n")
	sb.WriteString("http_interface = 0.0.0.0\n")
	sb.WriteString("http_port = 8069\n")
	sb.WriteString("gevent_port = 8072\n")
	sb.WriteString(fmt.Sprintf("admin_passwd = %s\n", inst.AdminPassword))
	sb.WriteString(fmt.Sprintf("db_host = %s\n", inst.DBHost))
	sb.WriteString(fmt.Sprintf("db_port = %d\n", inst.DBPort))
	sb.WriteString(fmt.Sprintf("db_user = %s\n", inst.DBUser))
	sb.WriteString(fmt.Sprintf("db_password = %s\n", inst.DBPassword))
	sb.WriteString(fmt.Sprintf("addons_path = %s\n", addonsPath))
	sb.WriteString("data_dir = /var/lib/odoo\n")
	sb.WriteString("proxy_mode = True\n")
	sb.WriteString("list_db = True\n")
	sb.WriteString(fmt.Sprintf("workers = %d\n", inst.Resources.Workers))
	sb.WriteString(fmt.Sprintf("limit_memory_soft = %d\n", inst.Resources.LimitMemorySoft))
	sb.WriteString(fmt.Sprintf("limit_memory_hard = %d\n", inst.Resources.LimitMemoryHard))
	sb.WriteString(fmt.Sprintf("limit_time_cpu = %d\n", inst.Resources.LimitTimeCPU))
	sb.WriteString(fmt.Sprintf("limit_time_real = %d\n", inst.Resources.LimitTimeReal))
	sb.WriteString(fmt.Sprintf("max_cron_threads = %d\n", inst.Resources.MaxCronThreads))

	if strings.TrimSpace(inst.CustomConf) != "" {
		sb.WriteString("\n# Custom configuration\n")
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

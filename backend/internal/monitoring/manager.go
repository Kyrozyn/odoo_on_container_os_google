package monitoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/database"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/proxy"
	"github.com/runodoo/runodoo/internal/store"
)

const (
	PromImage       = "prom/prometheus:latest"
	GrafanaImage    = "grafana/grafana:latest"
	PGExporterImage = "prometheuscommunity/postgres-exporter:latest"
)

type Manager struct {
	cfg    *config.Config
	store  *store.Store
	docker *docker.Client
	db     *database.Manager
	proxy  *proxy.Manager
}

func NewManager(cfg *config.Config, st *store.Store, dc *docker.Client, db *database.Manager, px *proxy.Manager) *Manager {
	return &Manager{cfg: cfg, store: st, docker: dc, db: db, proxy: px}
}

type ScrapeTarget struct {
	Job       string `json:"job"`
	Instance  string `json:"instance"`
	Health    string `json:"health"`
	LastError string `json:"lastError,omitempty"`
	ScrapeURL string `json:"scrapeUrl"`
}

type MonitoringStatus struct {
	Config           models.MonitoringConfig `json:"config"`
	PrometheusStatus string                  `json:"prometheusStatus"`
	GrafanaStatus    string                  `json:"grafanaStatus"`
	PGExporterStatus string                  `json:"pgExporterStatus"`
	PrimaryURL       string                  `json:"primaryUrl"`
	RootSubdomainURL string                  `json:"rootSubdomainUrl"`
	Targets          []ScrapeTarget          `json:"targets"`
}

type InstallRequest struct {
	ExposePort       bool                  `json:"exposePort"`
	HTTPPort         int                   `json:"httpPort"`
	UseRootSubdomain bool                  `json:"useRootSubdomain"`
	Domains          []models.DomainConfig `json:"domains"`
	GrafanaUser      string                `json:"grafanaUser"`
	GrafanaPassword  string                `json:"grafanaPassword"`
	AllowAnonymous   bool                  `json:"allowAnonymous"`
}

type UpdateConfigRequest struct {
	ExposePort              *bool                  `json:"exposePort,omitempty"`
	HTTPPort                *int                   `json:"httpPort,omitempty"`
	UseRootSubdomain        *bool                  `json:"useRootSubdomain,omitempty"`
	RootSubdomainSSL        *bool                  `json:"rootSubdomainSsl,omitempty"`
	RootSubdomainForceHTTPS *bool                  `json:"rootSubdomainForceHttps,omitempty"`
	Domains                 *[]models.DomainConfig `json:"domains,omitempty"`
	AllowAnonymous          *bool                  `json:"allowAnonymous,omitempty"`
}

func containerNamesFor(inst models.OdooInstance) (prom, grafana, pgExp string) {
	prom = inst.Monitoring.PrometheusContainer
	if prom == "" {
		prom = "runodoo-prom-" + inst.Name
	}
	grafana = inst.Monitoring.GrafanaContainer
	if grafana == "" {
		grafana = "runodoo-grafana-" + inst.Name
	}
	pgExp = inst.Monitoring.PGExporterContainer
	if pgExp == "" {
		pgExp = "runodoo-pgexp-" + inst.Name
	}
	return prom, grafana, pgExp
}

func (m *Manager) instMonDir(inst models.OdooInstance) string {
	return filepath.Join(m.cfg.InstancesDir(), inst.Name, "monitoring")
}

func (m *Manager) hostInstMonDir(inst models.OdooInstance) string {
	return filepath.Join(m.cfg.HostInstancesDir(), inst.Name, "monitoring")
}

func (m *Manager) inspectStatus(ctx context.Context, name string) string {
	if name == "" {
		return "not_found"
	}
	insp, err := m.docker.InspectContainer(ctx, name)
	if err != nil {
		return "not_found"
	}
	return insp.State.Status
}

func (m *Manager) nextMonitoringPort() int {
	st := m.store.Get()
	used := map[int]bool{}
	for _, i := range st.Instances {
		used[i.HTTPPort] = true
		used[i.LongpollingPort] = true
		if i.Monitoring.HTTPPort > 0 {
			used[i.Monitoring.HTTPPort] = true
		}
	}
	port := 3001
	for used[port] {
		port++
	}
	return port
}

func (m *Manager) Status(ctx context.Context, inst models.OdooInstance) MonitoringStatus {
	st := m.store.Get()
	prom, grafana, pgExp := containerNamesFor(inst)
	mc := inst.Monitoring
	mc.PrometheusContainer = prom
	mc.GrafanaContainer = grafana
	mc.PGExporterContainer = pgExp
	if mc.Domains == nil {
		mc.Domains = []models.DomainConfig{}
	}
	if mc.HTTPPort == 0 {
		mc.HTTPPort = m.nextMonitoringPort()
	}
	if mc.GrafanaUser == "" {
		mc.GrafanaUser = "admin"
	}

	res := MonitoringStatus{
		Config:           mc,
		PrometheusStatus: m.inspectStatus(ctx, prom),
		GrafanaStatus:    m.inspectStatus(ctx, grafana),
		PGExporterStatus: m.inspectStatus(ctx, pgExp),
		Targets:          []ScrapeTarget{},
	}

	root := proxy.NormalizeDomain(st.Settings.RootDomain)
	if root != "" {
		scheme := "http"
		if mc.RootSubdomainSSL {
			scheme = "https"
		}
		res.RootSubdomainURL = fmt.Sprintf("%s://grafana.%s.%s", scheme, inst.Name, root)
	}

	if len(mc.Domains) > 0 && proxy.NormalizeDomain(mc.Domains[0].Domain) != "" {
		scheme := "http"
		if mc.Domains[0].HasSSL {
			scheme = "https"
		}
		res.PrimaryURL = fmt.Sprintf("%s://%s", scheme, proxy.NormalizeDomain(mc.Domains[0].Domain))
	} else if mc.UseRootSubdomain && res.RootSubdomainURL != "" {
		res.PrimaryURL = res.RootSubdomainURL
	} else if mc.ExposePort && mc.HTTPPort > 0 {
		res.PrimaryURL = fmt.Sprintf(":%d", mc.HTTPPort)
	}

	if res.PrometheusStatus == "running" {
		res.Targets = m.fetchPrometheusTargets(ctx, prom)
	}
	return res
}

func (m *Manager) fetchPrometheusTargets(ctx context.Context, promContainer string) []ScrapeTarget {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, "http://"+promContainer+":9090/api/v1/targets", nil)
	if err != nil {
		return []ScrapeTarget{}
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return []ScrapeTarget{}
	}
	defer resp.Body.Close()

	var payload struct {
		Data struct {
			ActiveTargets []struct {
				Labels    map[string]string `json:"labels"`
				ScrapeURL string            `json:"scrapeUrl"`
				Health    string            `json:"health"`
				LastError string            `json:"lastError"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return []ScrapeTarget{}
	}
	out := make([]ScrapeTarget, 0, len(payload.Data.ActiveTargets))
	for _, t := range payload.Data.ActiveTargets {
		out = append(out, ScrapeTarget{
			Job:       t.Labels["job"],
			Instance:  t.Labels["instance"],
			Health:    t.Health,
			LastError: t.LastError,
			ScrapeURL: t.ScrapeURL,
		})
	}
	return out
}

func (m *Manager) updateInstanceMonitoring(instID string, fn func(*models.MonitoringConfig)) (models.OdooInstance, error) {
	var updated models.OdooInstance
	err := m.store.Update(func(st *models.State) error {
		for i := range st.Instances {
			if st.Instances[i].ID == instID {
				fn(&st.Instances[i].Monitoring)
				updated = st.Instances[i]
				return nil
			}
		}
		return fmt.Errorf("instance not found")
	})
	return updated, err
}

func (m *Manager) Install(ctx context.Context, inst models.OdooInstance, req InstallRequest) (*MonitoringStatus, error) {
	return m.InstallWithLog(ctx, inst, req, nil)
}

func (m *Manager) InstallWithLog(ctx context.Context, inst models.OdooInstance, req InstallRequest, onLog func(string)) (*MonitoringStatus, error) {
	logf := func(format string, args ...any) {
		if onLog != nil {
			onLog(fmt.Sprintf(format, args...))
		}
	}

	logf("[1/6] Ensuring Docker network %s...\n", m.cfg.DockerNetwork)
	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		return nil, err
	}
	port := req.HTTPPort
	if port <= 0 {
		port = m.nextMonitoringPort()
	}
	user := strings.TrimSpace(req.GrafanaUser)
	if user == "" {
		user = "admin"
	}
	pass := strings.TrimSpace(req.GrafanaPassword)
	if pass == "" {
		pass = randomSecret(8)
	}
	domains := req.Domains
	if domains == nil {
		domains = []models.DomainConfig{}
	}

	prom, grafana, pgExp := containerNamesFor(inst)
	updated, err := m.updateInstanceMonitoring(inst.ID, func(mc *models.MonitoringConfig) {
		*mc = models.MonitoringConfig{
			Installed:           true,
			PrometheusContainer: prom,
			GrafanaContainer:    grafana,
			PGExporterContainer: pgExp,
			ExposePort:          req.ExposePort,
			HTTPPort:            port,
			UseRootSubdomain:    req.UseRootSubdomain,
			Domains:             domains,
			GrafanaUser:         user,
			GrafanaPassword:     pass,
			AllowAnonymous:      req.AllowAnonymous,
		}
	})
	if err != nil {
		return nil, err
	}

	logf("[2/6] Writing Prometheus, PG Exporter & Grafana provisioning configs...\n")
	if err := m.writeConfigFiles(updated); err != nil {
		return nil, err
	}

	logf("[3/6] Checking & pulling Docker images...\n")
	for _, img := range []string{PGExporterImage, PromImage, GrafanaImage} {
		if m.docker.ImageExists(ctx, img) {
			logf("  -> Image %s already cached locally.\n", img)
			continue
		}
		logf("  -> Pulling %s...\n", img)
		if err := m.docker.PullImageWithProgress(ctx, img, func(line string) {
			logf("     %s\n", line)
		}); err != nil {
			return nil, fmt.Errorf("failed pulling %s: %w", img, err)
		}
	}

	logf("[4/6] Creating & starting %s...\n", pgExp)
	if err := m.recreatePGExporter(ctx, updated); err != nil {
		return nil, err
	}
	logf("[5/6] Creating & starting %s...\n", prom)
	if err := m.recreatePrometheus(ctx, updated); err != nil {
		return nil, err
	}
	logf("[6/6] Creating & starting %s...\n", grafana)
	if err := m.recreateGrafana(ctx, updated); err != nil {
		return nil, err
	}

	if m.proxy != nil {
		logf("Syncing Nginx proxy configuration...\n")
		_ = m.proxy.SyncAll(ctx)
	}
	logf("Done! Monitoring stack for %s is running.\n", inst.Name)

	status := m.Status(ctx, updated)
	return &status, nil
}

func (m *Manager) UpdateConfig(ctx context.Context, inst models.OdooInstance, req UpdateConfigRequest) (*MonitoringStatus, error) {
	var recreateGrafana bool
	updated, err := m.updateInstanceMonitoring(inst.ID, func(mc *models.MonitoringConfig) {
		if req.ExposePort != nil && *req.ExposePort != mc.ExposePort {
			mc.ExposePort = *req.ExposePort
			recreateGrafana = true
		}
		if req.HTTPPort != nil && *req.HTTPPort > 0 && *req.HTTPPort != mc.HTTPPort {
			mc.HTTPPort = *req.HTTPPort
			recreateGrafana = true
		}
		if req.UseRootSubdomain != nil {
			mc.UseRootSubdomain = *req.UseRootSubdomain
		}
		if req.RootSubdomainSSL != nil {
			mc.RootSubdomainSSL = *req.RootSubdomainSSL
		}
		if req.RootSubdomainForceHTTPS != nil {
			mc.RootSubdomainForceHTTPS = *req.RootSubdomainForceHTTPS
		}
		if req.Domains != nil {
			mc.Domains = *req.Domains
		}
		if req.AllowAnonymous != nil && *req.AllowAnonymous != mc.AllowAnonymous {
			mc.AllowAnonymous = *req.AllowAnonymous
			recreateGrafana = true
		}
	})
	if err != nil {
		return nil, err
	}

	if recreateGrafana {
		if err := m.recreateGrafana(ctx, updated); err != nil {
			return nil, err
		}
	}
	if m.proxy != nil {
		_ = m.proxy.SyncAll(ctx)
	}
	status := m.Status(ctx, updated)
	return &status, nil
}

func (m *Manager) UpdateGrafanaAccount(ctx context.Context, inst models.OdooInstance, username, password string) (*MonitoringStatus, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" {
		return nil, fmt.Errorf("Grafana username is required")
	}
	if len(password) < 4 {
		return nil, fmt.Errorf("Grafana password must be at least 4 characters")
	}

	updated, err := m.updateInstanceMonitoring(inst.ID, func(mc *models.MonitoringConfig) {
		mc.GrafanaUser = username
		mc.GrafanaPassword = password
	})
	if err != nil {
		return nil, err
	}

	_, grafana, _ := containerNamesFor(updated)
	_, _, _ = m.docker.Exec(ctx, grafana, []string{
		"grafana", "cli", "admin", "reset-admin-password", password,
	}, nil)
	if err := m.recreateGrafana(ctx, updated); err != nil {
		return nil, err
	}
	status := m.Status(ctx, updated)
	return &status, nil
}

func (m *Manager) IssueDomainSSL(ctx context.Context, inst models.OdooInstance, domain string) (*MonitoringStatus, error) {
	if m.proxy == nil {
		return nil, fmt.Errorf("proxy manager unavailable")
	}
	st := m.store.Get()
	email := st.Settings.LetsEncryptEmail
	if _, err := m.proxy.IssueSSL(ctx, domain, email); err != nil {
		return nil, err
	}

	clean := proxy.NormalizeDomain(domain)
	root := proxy.NormalizeDomain(st.Settings.RootDomain)
	updated, err := m.updateInstanceMonitoring(inst.ID, func(mc *models.MonitoringConfig) {
		if root != "" && clean == "grafana."+inst.Name+"."+root {
			mc.RootSubdomainSSL = true
		}
		for i := range mc.Domains {
			if proxy.NormalizeDomain(mc.Domains[i].Domain) == clean {
				mc.Domains[i].HasSSL = true
			}
		}
	})
	if err != nil {
		return nil, err
	}

	_ = m.proxy.SyncAll(ctx)
	status := m.Status(ctx, updated)
	return &status, nil
}

func (m *Manager) Control(ctx context.Context, inst models.OdooInstance, target, action string) error {
	prom, grafana, pgExp := containerNamesFor(inst)
	var names []string
	switch target {
	case "prometheus":
		names = []string{prom}
	case "grafana":
		names = []string{grafana}
	case "pg_exporter":
		names = []string{pgExp}
	case "all":
		names = []string{pgExp, prom, grafana}
	default:
		return fmt.Errorf("unknown target %q", target)
	}

	for _, name := range names {
		switch action {
		case "start":
			if err := m.docker.StartContainer(ctx, name); err != nil {
				return err
			}
		case "stop":
			if err := m.docker.StopContainer(ctx, name, 10); err != nil {
				return err
			}
		case "restart":
			if err := m.docker.RestartContainer(ctx, name, 10); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown action %q", action)
		}
	}
	return nil
}

func (m *Manager) Resync(ctx context.Context, inst models.OdooInstance) (*MonitoringStatus, error) {
	if err := m.writeConfigFiles(inst); err != nil {
		return nil, err
	}
	prom, grafana, pgExp := containerNamesFor(inst)
	_ = m.docker.RestartContainer(ctx, pgExp, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+prom+":9090/-/reload", nil)
	if err == nil {
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil || resp.StatusCode >= 300 {
			_ = m.docker.RestartContainer(ctx, prom, 10)
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	_ = m.docker.RestartContainer(ctx, grafana, 10)
	status := m.Status(ctx, inst)
	return &status, nil
}

func (m *Manager) writeConfigFiles(inst models.OdooInstance) error {
	base := m.instMonDir(inst)
	prom, _, _ := containerNamesFor(inst)
	dirs := []string{
		filepath.Join(base, "prometheus"),
		filepath.Join(base, "prometheus-data"),
		filepath.Join(base, "pg_exporter"),
		filepath.Join(base, "grafana-data"),
		filepath.Join(base, "grafana", "provisioning", "datasources"),
		filepath.Join(base, "grafana", "provisioning", "dashboards"),
		filepath.Join(base, "grafana", "dashboards"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o777); err != nil {
			return err
		}
		_ = os.Chmod(d, 0o777)
	}

	files := map[string]string{
		filepath.Join(base, "prometheus", "prometheus.yml"):                             m.buildPrometheusConfig(inst),
		filepath.Join(base, "pg_exporter", "postgres_exporter.yml"):                   m.buildPGExporterConfig(inst),
		filepath.Join(base, "grafana", "provisioning", "datasources", "prometheus.yml"): grafanaDatasourceYAML(prom),
		filepath.Join(base, "grafana", "provisioning", "dashboards", "dashboards.yml"):  grafanaDashboardsProviderYAML(inst.Name),
		filepath.Join(base, "grafana", "dashboards", "odoo-apps.json"):                odooAppsDashboardJSON(inst.Name),
		filepath.Join(base, "grafana", "dashboards", "postgresql.json"):               postgresqlDashboardJSON(inst.Name),
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) buildPrometheusConfig(inst models.OdooInstance) string {
	_, _, pgExp := containerNamesFor(inst)
	return fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'runodoo-apps'
    metrics_path: /internal/metrics
    params:
      instance: ['%s']
    static_configs:
      - targets: ['runodoo:%s']
        labels:
          instance: '%s'

  - job_name: 'postgres'
    metrics_path: /probe
    static_configs:
      - targets: ['%s:5432/postgres?sslmode=disable']
        labels:
          instance: '%s'
          container: '%s'
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [instance]
        target_label: __param_auth_module
      - source_labels: [__param_target]
        target_label: target
      - target_label: __address__
        replacement: %s:9187
`, inst.Name, m.cfg.Port, inst.Name, inst.DBContainerName, inst.Name, inst.DBContainerName, pgExp)
}

func (m *Manager) buildPGExporterConfig(inst models.OdooInstance) string {
	pgUser, pgPass := m.db.AdminCredsForInstance(inst)
	return fmt.Sprintf("auth_modules:\n  %s:\n    type: userpass\n    userpass:\n      username: %q\n      password: %q\n",
		inst.Name, pgUser, pgPass)
}

func (m *Manager) recreatePGExporter(ctx context.Context, inst models.OdooInstance) error {
	_, _, pgExp := containerNamesFor(inst)
	_ = m.docker.StopContainer(ctx, pgExp, 10)
	_ = m.docker.RemoveContainer(ctx, pgExp, true)

	hostMon := m.hostInstMonDir(inst)
	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  pgExp,
		Image: PGExporterImage,
		Cmd:   []string{"--config.file=/etc/postgres_exporter/postgres_exporter.yml"},
		Labels: map[string]string{
			"runodoo.managed":  "true",
			"runodoo.role":     "monitoring-pg-exporter",
			"runodoo.instance": inst.Name,
		},
		Binds: []string{
			fmt.Sprintf("%s/pg_exporter/postgres_exporter.yml:/etc/postgres_exporter/postgres_exporter.yml:ro", hostMon),
		},
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	return m.docker.StartContainer(ctx, pgExp)
}

func (m *Manager) recreatePrometheus(ctx context.Context, inst models.OdooInstance) error {
	prom, _, _ := containerNamesFor(inst)
	_ = m.docker.StopContainer(ctx, prom, 10)
	_ = m.docker.RemoveContainer(ctx, prom, true)

	hostMon := m.hostInstMonDir(inst)
	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  prom,
		Image: PromImage,
		Cmd: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--storage.tsdb.retention.time=15d",
			"--web.enable-lifecycle",
		},
		Labels: map[string]string{
			"runodoo.managed":  "true",
			"runodoo.role":     "monitoring-prometheus",
			"runodoo.instance": inst.Name,
		},
		Binds: []string{
			fmt.Sprintf("%s/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro", hostMon),
			fmt.Sprintf("%s/prometheus-data:/prometheus", hostMon),
		},
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	return m.docker.StartContainer(ctx, prom)
}

func (m *Manager) recreateGrafana(ctx context.Context, inst models.OdooInstance) error {
	_, grafana, _ := containerNamesFor(inst)
	_ = m.docker.StopContainer(ctx, grafana, 10)
	_ = m.docker.RemoveContainer(ctx, grafana, true)

	mc := inst.Monitoring
	hostMon := m.hostInstMonDir(inst)

	ports := map[string]string{}
	if mc.ExposePort && mc.HTTPPort > 0 {
		ports["3000/tcp"] = strconv.Itoa(mc.HTTPPort)
	}

	anon := "false"
	if mc.AllowAnonymous {
		anon = "true"
	}

	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  grafana,
		Image: GrafanaImage,
		Env: []string{
			"GF_SECURITY_ADMIN_USER=" + mc.GrafanaUser,
			"GF_SECURITY_ADMIN_PASSWORD=" + mc.GrafanaPassword,
			"GF_USERS_ALLOW_SIGN_UP=false",
			"GF_AUTH_ANONYMOUS_ENABLED=" + anon,
			"GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer",
			"GF_SECURITY_ALLOW_EMBEDDING=true",
		},
		Labels: map[string]string{
			"runodoo.managed":  "true",
			"runodoo.role":     "monitoring-grafana",
			"runodoo.instance": inst.Name,
		},
		Binds: []string{
			fmt.Sprintf("%s/grafana-data:/var/lib/grafana", hostMon),
			fmt.Sprintf("%s/grafana/provisioning:/etc/grafana/provisioning:ro", hostMon),
			fmt.Sprintf("%s/grafana/dashboards:/var/lib/grafana/dashboards:ro", hostMon),
		},
		PortMappings:  ports,
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	return m.docker.StartContainer(ctx, grafana)
}

// RenderMetrics outputs Prometheus exposition metrics, optionally filtered to a single instance name.
func (m *Manager) RenderMetrics(ctx context.Context, instanceFilter string) string {
	st := m.store.Get()
	var sb strings.Builder

	sb.WriteString("# TYPE runodoo_container_up gauge\n")
	sb.WriteString("# TYPE runodoo_container_cpu_percent gauge\n")
	sb.WriteString("# TYPE runodoo_container_memory_usage_bytes gauge\n")
	sb.WriteString("# TYPE runodoo_container_memory_limit_bytes gauge\n")
	sb.WriteString("# TYPE runodoo_container_memory_percent gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_http_up gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_http_response_seconds gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_workers_configured gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_users_active gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_cron_active gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_cron_overdue gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_installed_modules gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_attachments_count gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_attachments_bytes gauge\n")
	sb.WriteString("# TYPE runodoo_odoo_mail_queue_pending gauge\n")

	seenPG := map[string]bool{}
	httpClient := &http.Client{Timeout: 2 * time.Second}

	for _, inst := range st.Instances {
		if instanceFilter != "" && inst.Name != instanceFilter {
			continue
		}
		up := 0
		if insp, err := m.docker.InspectContainer(ctx, inst.ContainerName); err == nil && insp.State.Running {
			up = 1
		}
		sb.WriteString(fmt.Sprintf("runodoo_container_up{instance=%q,container=%q,role=\"odoo\"} %d\n", inst.Name, inst.ContainerName, up))
		if up == 1 {
			if stats, err := m.docker.ContainerStats(ctx, inst.ContainerName); err == nil {
				sb.WriteString(fmt.Sprintf("runodoo_container_cpu_percent{instance=%q,container=%q,role=\"odoo\"} %.4f\n", inst.Name, inst.ContainerName, stats.CPUPercent))
				sb.WriteString(fmt.Sprintf("runodoo_container_memory_usage_bytes{instance=%q,container=%q,role=\"odoo\"} %.0f\n", inst.Name, inst.ContainerName, stats.MemoryUsageMB*1024*1024))
				sb.WriteString(fmt.Sprintf("runodoo_container_memory_limit_bytes{instance=%q,container=%q,role=\"odoo\"} %.0f\n", inst.Name, inst.ContainerName, stats.MemoryLimitMB*1024*1024))
				sb.WriteString(fmt.Sprintf("runodoo_container_memory_percent{instance=%q,container=%q,role=\"odoo\"} %.4f\n", inst.Name, inst.ContainerName, stats.MemoryPercent))
			}
		}

		if !seenPG[inst.DBContainerName] {
			seenPG[inst.DBContainerName] = true
			pgUp := 0
			if insp, err := m.docker.InspectContainer(ctx, inst.DBContainerName); err == nil && insp.State.Running {
				pgUp = 1
			}
			sb.WriteString(fmt.Sprintf("runodoo_container_up{instance=%q,container=%q,role=\"postgres\"} %d\n", inst.Name, inst.DBContainerName, pgUp))
			if pgUp == 1 {
				if stats, err := m.docker.ContainerStats(ctx, inst.DBContainerName); err == nil {
					sb.WriteString(fmt.Sprintf("runodoo_container_cpu_percent{instance=%q,container=%q,role=\"postgres\"} %.4f\n", inst.Name, inst.DBContainerName, stats.CPUPercent))
					sb.WriteString(fmt.Sprintf("runodoo_container_memory_usage_bytes{instance=%q,container=%q,role=\"postgres\"} %.0f\n", inst.Name, inst.DBContainerName, stats.MemoryUsageMB*1024*1024))
					sb.WriteString(fmt.Sprintf("runodoo_container_memory_limit_bytes{instance=%q,container=%q,role=\"postgres\"} %.0f\n", inst.Name, inst.DBContainerName, stats.MemoryLimitMB*1024*1024))
					sb.WriteString(fmt.Sprintf("runodoo_container_memory_percent{instance=%q,container=%q,role=\"postgres\"} %.4f\n", inst.Name, inst.DBContainerName, stats.MemoryPercent))
				}
			}
		}

		httpUp := 0
		var latency float64
		if up == 1 {
			start := time.Now()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+inst.ContainerName+":8069/web/login", nil)
			if err == nil {
				if resp, err := httpClient.Do(req); err == nil {
					latency = time.Since(start).Seconds()
					if resp.StatusCode < 500 {
						httpUp = 1
					}
					resp.Body.Close()
				}
			}
		}
		sb.WriteString(fmt.Sprintf("runodoo_odoo_http_up{instance=%q} %d\n", inst.Name, httpUp))
		sb.WriteString(fmt.Sprintf("runodoo_odoo_http_response_seconds{instance=%q} %.6f\n", inst.Name, latency))
		sb.WriteString(fmt.Sprintf("runodoo_odoo_workers_configured{instance=%q} %d\n", inst.Name, inst.Resources.Workers))

		if dbs, err := m.db.ListInstanceDatabases(ctx, inst); err == nil {
			for _, d := range dbs {
				om := m.queryOdooDBMetrics(ctx, inst, d.Name)
				sb.WriteString(fmt.Sprintf("runodoo_odoo_users_active{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.activeUsers))
				sb.WriteString(fmt.Sprintf("runodoo_odoo_cron_active{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.activeCrons))
				sb.WriteString(fmt.Sprintf("runodoo_odoo_cron_overdue{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.overdueCrons))
				sb.WriteString(fmt.Sprintf("runodoo_odoo_installed_modules{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.installedModules))
				sb.WriteString(fmt.Sprintf("runodoo_odoo_attachments_count{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.attachmentsCount))
				sb.WriteString(fmt.Sprintf("runodoo_odoo_attachments_bytes{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.attachmentsBytes))
				sb.WriteString(fmt.Sprintf("runodoo_odoo_mail_queue_pending{instance=%q,db=%q} %d\n", inst.Name, d.Name, om.mailQueuePending))
			}
		}
	}

	return sb.String()
}

type odooDBMetrics struct {
	activeUsers      int64
	activeCrons      int64
	overdueCrons     int64
	installedModules int64
	attachmentsCount int64
	attachmentsBytes int64
	mailQueuePending int64
}

func (m *Manager) queryOdooDBMetrics(ctx context.Context, inst models.OdooInstance, dbName string) odooDBMetrics {
	var res odooDBMetrics
	pgUser, pgPass := m.db.AdminCredsForInstance(inst)
	query := `SELECT
		COALESCE((SELECT count(*) FROM res_users WHERE active = true AND share = false), 0),
		COALESCE((SELECT count(*) FROM ir_cron WHERE active = true), 0),
		COALESCE((SELECT count(*) FROM ir_cron WHERE active = true AND nextcall < now() - interval '15 minutes'), 0),
		COALESCE((SELECT count(*) FROM ir_module_module WHERE state = 'installed'), 0),
		COALESCE((SELECT count(*) FROM ir_attachment), 0),
		COALESCE((SELECT sum(file_size) FROM ir_attachment), 0),
		COALESCE((SELECT count(*) FROM mail_mail WHERE state IN ('outgoing', 'exception')), 0);`

	out, code, err := m.docker.Exec(ctx, inst.DBContainerName, []string{
		"psql", "-U", pgUser, "-d", dbName, "-At", "-F", "|", "-c", query,
	}, []string{"PGPASSWORD=" + pgPass})
	if err != nil || code != 0 {
		return res
	}
	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) >= 7 {
		res.activeUsers, _ = strconv.ParseInt(parts[0], 10, 64)
		res.activeCrons, _ = strconv.ParseInt(parts[1], 10, 64)
		res.overdueCrons, _ = strconv.ParseInt(parts[2], 10, 64)
		res.installedModules, _ = strconv.ParseInt(parts[3], 10, 64)
		res.attachmentsCount, _ = strconv.ParseInt(parts[4], 10, 64)
		res.attachmentsBytes, _ = strconv.ParseInt(parts[5], 10, 64)
		res.mailQueuePending, _ = strconv.ParseInt(parts[6], 10, 64)
	}
	return res
}

func randomSecret(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

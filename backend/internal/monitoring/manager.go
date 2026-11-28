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
	PromContainer       = "runodoo-prometheus"
	GrafanaContainer    = "runodoo-grafana"
	PGExporterContainer = "runodoo-pg-exporter"

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
	Job        string `json:"job"`
	Instance   string `json:"instance"`
	Health     string `json:"health"`
	LastError  string `json:"lastError,omitempty"`
	ScrapeURL  string `json:"scrapeUrl"`
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

func (m *Manager) Status(ctx context.Context) MonitoringStatus {
	st := m.store.Get()
	mc := st.Monitoring
	if mc.PrometheusContainer == "" {
		mc.PrometheusContainer = PromContainer
	}
	if mc.GrafanaContainer == "" {
		mc.GrafanaContainer = GrafanaContainer
	}
	if mc.PGExporterContainer == "" {
		mc.PGExporterContainer = PGExporterContainer
	}
	if mc.HTTPPort == 0 {
		mc.HTTPPort = 3001
	}
	if mc.GrafanaUser == "" {
		mc.GrafanaUser = "admin"
	}

	res := MonitoringStatus{
		Config:           mc,
		PrometheusStatus: m.inspectStatus(ctx, mc.PrometheusContainer),
		GrafanaStatus:    m.inspectStatus(ctx, mc.GrafanaContainer),
		PGExporterStatus: m.inspectStatus(ctx, mc.PGExporterContainer),
		Targets:          []ScrapeTarget{},
	}

	root := proxy.NormalizeDomain(st.Settings.RootDomain)
	if root != "" {
		scheme := "http"
		if mc.RootSubdomainSSL {
			scheme = "https"
		}
		res.RootSubdomainURL = fmt.Sprintf("%s://grafana.%s", scheme, root)
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
		res.Targets = m.fetchPrometheusTargets(ctx)
	}
	return res
}

func (m *Manager) fetchPrometheusTargets(ctx context.Context) []ScrapeTarget {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, "http://"+PromContainer+":9090/api/v1/targets", nil)
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

func (m *Manager) Install(ctx context.Context, req InstallRequest) (*MonitoringStatus, error) {
	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		return nil, err
	}
	port := req.HTTPPort
	if port <= 0 {
		port = 3001
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

	if err := m.store.Update(func(st *models.State) error {
		st.Monitoring = models.MonitoringConfig{
			Installed:           true,
			PrometheusContainer: PromContainer,
			GrafanaContainer:    GrafanaContainer,
			PGExporterContainer: PGExporterContainer,
			ExposePort:          req.ExposePort,
			HTTPPort:            port,
			UseRootSubdomain:    req.UseRootSubdomain,
			Domains:             domains,
			GrafanaUser:         user,
			GrafanaPassword:     pass,
			AllowAnonymous:      req.AllowAnonymous,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := m.writeConfigFiles(); err != nil {
		return nil, err
	}

	for _, img := range []string{PGExporterImage, PromImage, GrafanaImage} {
		if !m.docker.ImageExists(ctx, img) {
			if err := m.docker.PullImage(ctx, img); err != nil {
				return nil, fmt.Errorf("failed pulling %s: %w", img, err)
			}
		}
	}

	if err := m.recreatePGExporter(ctx); err != nil {
		return nil, err
	}
	if err := m.recreatePrometheus(ctx); err != nil {
		return nil, err
	}
	if err := m.recreateGrafana(ctx); err != nil {
		return nil, err
	}

	if m.proxy != nil {
		_ = m.proxy.SyncAll(ctx)
	}

	st := m.Status(ctx)
	return &st, nil
}

func (m *Manager) UpdateConfig(ctx context.Context, req UpdateConfigRequest) (*MonitoringStatus, error) {
	var recreateGrafana bool
	err := m.store.Update(func(st *models.State) error {
		mc := &st.Monitoring
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
		return nil
	})
	if err != nil {
		return nil, err
	}

	if recreateGrafana {
		if err := m.recreateGrafana(ctx); err != nil {
			return nil, err
		}
	}
	if m.proxy != nil {
		_ = m.proxy.SyncAll(ctx)
	}
	st := m.Status(ctx)
	return &st, nil
}

func (m *Manager) UpdateGrafanaAccount(ctx context.Context, username, password string) (*MonitoringStatus, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" {
		return nil, fmt.Errorf("Grafana username is required")
	}
	if len(password) < 4 {
		return nil, fmt.Errorf("Grafana password must be at least 4 characters")
	}

	if err := m.store.Update(func(st *models.State) error {
		st.Monitoring.GrafanaUser = username
		st.Monitoring.GrafanaPassword = password
		return nil
	}); err != nil {
		return nil, err
	}

	// Update existing Grafana DB password via CLI, then recreate container so env stays in sync.
	_, _, _ = m.docker.Exec(ctx, GrafanaContainer, []string{
		"grafana", "cli", "admin", "reset-admin-password", password,
	}, nil)
	if err := m.recreateGrafana(ctx); err != nil {
		return nil, err
	}
	st := m.Status(ctx)
	return &st, nil
}

func (m *Manager) IssueDomainSSL(ctx context.Context, domain string) (*MonitoringStatus, error) {
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
	if err := m.store.Update(func(s *models.State) error {
		if root != "" && clean == "grafana."+root {
			s.Monitoring.RootSubdomainSSL = true
		}
		for i := range s.Monitoring.Domains {
			if proxy.NormalizeDomain(s.Monitoring.Domains[i].Domain) == clean {
				s.Monitoring.Domains[i].HasSSL = true
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	_ = m.proxy.SyncAll(ctx)
	status := m.Status(ctx)
	return &status, nil
}

func (m *Manager) Control(ctx context.Context, target, action string) error {
	var names []string
	switch target {
	case "prometheus":
		names = []string{PromContainer}
	case "grafana":
		names = []string{GrafanaContainer}
	case "pg_exporter":
		names = []string{PGExporterContainer}
	case "all":
		names = []string{PGExporterContainer, PromContainer, GrafanaContainer}
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

func (m *Manager) Resync(ctx context.Context) (*MonitoringStatus, error) {
	if err := m.writeConfigFiles(); err != nil {
		return nil, err
	}
	_ = m.docker.RestartContainer(ctx, PGExporterContainer, 10)
	// Reload Prometheus configuration via lifecycle API; fallback to restart if unavailable.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+PromContainer+":9090/-/reload", nil)
	if err == nil {
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil || resp.StatusCode >= 300 {
			_ = m.docker.RestartContainer(ctx, PromContainer, 10)
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	_ = m.docker.RestartContainer(ctx, GrafanaContainer, 10)
	st := m.Status(ctx)
	return &st, nil
}

func (m *Manager) writeConfigFiles() error {
	base := m.cfg.MonitoringDir()
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
		filepath.Join(base, "prometheus", "prometheus.yml"):                             m.buildPrometheusConfig(),
		filepath.Join(base, "pg_exporter", "postgres_exporter.yml"):                   m.buildPGExporterConfig(),
		filepath.Join(base, "grafana", "provisioning", "datasources", "prometheus.yml"): grafanaDatasourceYAML(),
		filepath.Join(base, "grafana", "provisioning", "dashboards", "dashboards.yml"):  grafanaDashboardsProviderYAML(),
		filepath.Join(base, "grafana", "dashboards", "odoo-apps.json"):                odooAppsDashboardJSON(),
		filepath.Join(base, "grafana", "dashboards", "postgresql.json"):               postgresqlDashboardJSON(),
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) buildPrometheusConfig() string {
	st := m.store.Get()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'runodoo-apps'
    metrics_path: /internal/metrics
    static_configs:
      - targets: ['runodoo:%s']

  - job_name: 'postgres'
    metrics_path: /probe
    static_configs:
`, m.cfg.Port))

	sb.WriteString(fmt.Sprintf(`      - targets: ['%s:5432/postgres?sslmode=disable']
        labels:
          instance: 'shared'
          container: '%s'
`, st.SharedPostgres.ContainerName, st.SharedPostgres.ContainerName))

	for _, inst := range st.Instances {
		if inst.DBMode == models.DBModeDedicated {
			sb.WriteString(fmt.Sprintf(`      - targets: ['%s:5432/postgres?sslmode=disable']
        labels:
          instance: '%s'
          container: '%s'
`, inst.DBContainerName, inst.Name, inst.DBContainerName))
		}
	}

	sb.WriteString(`    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [instance]
        target_label: __param_auth_module
      - source_labels: [__param_target]
        target_label: target
      - target_label: __address__
        replacement: runodoo-pg-exporter:9187
`)
	return sb.String()
}

func (m *Manager) buildPGExporterConfig() string {
	st := m.store.Get()
	var sb strings.Builder
	sb.WriteString("auth_modules:\n")
	sb.WriteString(fmt.Sprintf("  shared:\n    type: userpass\n    userpass:\n      username: %q\n      password: %q\n",
		st.SharedPostgres.SuperUser, st.SharedPostgres.SuperPassword))
	for _, inst := range st.Instances {
		if inst.DBMode == models.DBModeDedicated {
			sb.WriteString(fmt.Sprintf("  %s:\n    type: userpass\n    userpass:\n      username: %q\n      password: %q\n",
				inst.Name, inst.DBUser, inst.DBPassword))
		}
	}
	return sb.String()
}

func (m *Manager) recreatePGExporter(ctx context.Context) error {
	_ = m.docker.StopContainer(ctx, PGExporterContainer, 10)
	_ = m.docker.RemoveContainer(ctx, PGExporterContainer, true)

	hostMon := m.cfg.HostMonitoringDir()
	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  PGExporterContainer,
		Image: PGExporterImage,
		Cmd:   []string{"--config.file=/etc/postgres_exporter/postgres_exporter.yml"},
		Labels: map[string]string{
			"runodoo.managed": "true",
			"runodoo.role":    "monitoring-pg-exporter",
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
	return m.docker.StartContainer(ctx, PGExporterContainer)
}

func (m *Manager) recreatePrometheus(ctx context.Context) error {
	_ = m.docker.StopContainer(ctx, PromContainer, 10)
	_ = m.docker.RemoveContainer(ctx, PromContainer, true)

	hostMon := m.cfg.HostMonitoringDir()
	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  PromContainer,
		Image: PromImage,
		Cmd: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--storage.tsdb.retention.time=15d",
			"--web.enable-lifecycle",
		},
		Labels: map[string]string{
			"runodoo.managed": "true",
			"runodoo.role":    "monitoring-prometheus",
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
	return m.docker.StartContainer(ctx, PromContainer)
}

func (m *Manager) recreateGrafana(ctx context.Context) error {
	_ = m.docker.StopContainer(ctx, GrafanaContainer, 10)
	_ = m.docker.RemoveContainer(ctx, GrafanaContainer, true)

	st := m.store.Get()
	mc := st.Monitoring
	hostMon := m.cfg.HostMonitoringDir()

	ports := map[string]string{}
	if mc.ExposePort && mc.HTTPPort > 0 {
		ports["3000/tcp"] = strconv.Itoa(mc.HTTPPort)
	}

	anon := "false"
	if mc.AllowAnonymous {
		anon = "true"
	}

	_, err := m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  GrafanaContainer,
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
			"runodoo.managed": "true",
			"runodoo.role":    "monitoring-grafana",
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
	return m.docker.StartContainer(ctx, GrafanaContainer)
}

// RenderMetrics outputs Prometheus exposition metrics for all Odoo instances and PostgreSQL containers.
func (m *Manager) RenderMetrics(ctx context.Context) string {
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
		// Odoo container stats
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

		// Postgres container stats (deduplicated for shared)
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

		// Odoo HTTP health probe
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

		// Odoo internal DB metrics
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

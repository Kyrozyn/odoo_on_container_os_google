package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/store"
)

type Manager struct {
	cfg    *config.Config
	store  *store.Store
	docker *docker.Client
}

func NewManager(cfg *config.Config, st *store.Store, dc *docker.Client) *Manager {
	return &Manager{cfg: cfg, store: st, docker: dc}
}

// EnsureNginx ensures the managed Nginx reverse proxy container is created, running, and synced.
func (m *Manager) EnsureNginx(ctx context.Context) error {
	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		return err
	}
	if err := m.writeConfigs(); err != nil {
		return err
	}
	if err := m.ensureNginxContainer(ctx); err != nil {
		return err
	}
	return m.ReloadNginx(ctx)
}

// SyncAll regenerates Nginx configs for the panel and all Odoo instances,
// auto-starts Nginx if any domain is configured, and reloads Nginx.
func (m *Manager) SyncAll(ctx context.Context) error {
	hasAnyDomain, err := m.writeConfigsWithFlag()
	if err != nil {
		return err
	}
	if hasAnyDomain {
		if err := m.ensureNginxContainer(ctx); err != nil {
			return err
		}
	}
	return m.ReloadNginx(ctx)
}

func (m *Manager) ensureNginxContainer(ctx context.Context) error {
	if err := m.docker.EnsureNetwork(ctx, m.cfg.DockerNetwork); err != nil {
		return err
	}
	insp, err := m.docker.InspectContainer(ctx, m.cfg.NginxContainer)
	if err == nil {
		if !insp.State.Running {
			return m.docker.StartContainer(ctx, m.cfg.NginxContainer)
		}
		return nil
	}

	image := "nginx:1.27-alpine"
	if !m.docker.ImageExists(ctx, image) {
		if err := m.docker.PullImage(ctx, image); err != nil {
			return err
		}
	}

	_, err = m.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:  m.cfg.NginxContainer,
		Image: image,
		Labels: map[string]string{
			"runodoo.managed": "true",
			"runodoo.role":    "proxy",
		},
		Binds: []string{
			fmt.Sprintf("%s:/etc/nginx/conf.d:ro", m.cfg.HostNginxConfDir()),
			fmt.Sprintf("%s:/var/www/certbot:ro", m.cfg.HostCertbotWebroot()),
			fmt.Sprintf("%s:/etc/letsencrypt:ro", m.cfg.HostCertbotConfDir()),
		},
		PortMappings: map[string]string{
			"80/tcp":  "80",
			"443/tcp": "443",
		},
		Network:       m.cfg.DockerNetwork,
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		return err
	}
	return m.docker.StartContainer(ctx, m.cfg.NginxContainer)
}

func (m *Manager) writeConfigs() error {
	_, err := m.writeConfigsWithFlag()
	return err
}

func (m *Manager) writeConfigsWithFlag() (bool, error) {
	confDir := m.cfg.NginxConfDir()
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return false, err
	}

	// Clean previously generated runodoo-*.conf files
	entries, _ := os.ReadDir(confDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "runodoo-") && strings.HasSuffix(e.Name(), ".conf") {
			_ = os.Remove(filepath.Join(confDir, e.Name()))
		}
	}

	st := m.store.Get()
	hasAnyDomain := false

	// 1. Default catch-all server for ACME challenges & unknown hosts
	defaultConf := `server {
    listen 80 default_server;
    server_name _;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 404 "RunOdoo: No application configured for this domain.\n";
    }
}
`
	if err := os.WriteFile(filepath.Join(confDir, "runodoo-00-default.conf"), []byte(defaultConf), 0o644); err != nil {
		return false, err
	}

	// 2. Panel domain config (e.g., panel.runodoo.example.com -> runodoo:3000)
	if st.Settings.RootDomain != "" {
		hasAnyDomain = true
		panelHost := st.Settings.PanelSubdomain + "." + st.Settings.RootDomain
		if st.Settings.PanelSubdomain == "" || st.Settings.PanelSubdomain == "@" {
			panelHost = st.Settings.RootDomain
		}
		panelConf := m.renderPanelVhost(panelHost, st.Settings.PanelHasSSL, st.Settings.PanelForceHTTPS)
		if err := os.WriteFile(filepath.Join(confDir, "runodoo-01-panel.conf"), []byte(panelConf), 0o644); err != nil {
			return false, err
		}
	}

	// 3. Per-instance Odoo vhosts
	for _, inst := range st.Instances {
		var domains []models.DomainConfig
		if inst.UseRootSubdomain && st.Settings.RootDomain != "" {
			domains = append(domains, models.DomainConfig{
				Domain:     inst.Name + "." + st.Settings.RootDomain,
				HasSSL:     inst.RootSubdomainSSL,
				ForceHTTPS: inst.RootSubdomainSSL,
			})
		}
		domains = append(domains, inst.Domains...)

		if len(domains) == 0 {
			continue
		}

		var sb strings.Builder
		for _, d := range domains {
			if strings.TrimSpace(d.Domain) == "" {
				continue
			}
			hasAnyDomain = true
			sb.WriteString(m.renderOdooVhost(inst.ContainerName, d))
			sb.WriteString("\n")
		}
		if sb.Len() > 0 {
			fileName := fmt.Sprintf("runodoo-app-%s.conf", inst.Name)
			if err := os.WriteFile(filepath.Join(confDir, fileName), []byte(sb.String()), 0o644); err != nil {
				return false, err
			}
		}
	}

	return hasAnyDomain, nil
}

// ReloadNginx tests and reloads Nginx if the container is currently running.
func (m *Manager) ReloadNginx(ctx context.Context) error {
	insp, err := m.docker.InspectContainer(ctx, m.cfg.NginxContainer)
	if err != nil || !insp.State.Running {
		return nil
	}
	out, code, err := m.docker.Exec(ctx, m.cfg.NginxContainer, []string{"sh", "-c", "nginx -t && nginx -s reload"}, nil)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("nginx reload failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// IssueSSL runs Certbot via webroot to obtain a Let's Encrypt certificate for the given domain.
func (m *Manager) IssueSSL(ctx context.Context, domain, email string) (string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}
	if email == "" {
		st := m.store.Get()
		email = st.Settings.LetsEncryptEmail
	}
	if email == "" {
		return "", fmt.Errorf("Let's Encrypt email is required (configure in Settings first)")
	}

	// Ensure Nginx is running and serving /.well-known/acme-challenge/
	if err := m.EnsureNginx(ctx); err != nil {
		return "", fmt.Errorf("failed to start nginx before SSL issuance: %w", err)
	}

	logs, code, err := m.docker.RunOneShot(ctx, docker.CreateContainerSpec{
		Name:  "runodoo-certbot-" + strings.ReplaceAll(domain, ".", "-"),
		Image: "certbot/certbot:latest",
		Cmd: []string{
			"certonly", "--webroot",
			"-w", "/var/www/certbot",
			"-d", domain,
			"--email", email,
			"--agree-tos",
			"--non-interactive",
			"--keep-until-expiring",
		},
		Binds: []string{
			fmt.Sprintf("%s:/var/www/certbot", m.cfg.HostCertbotWebroot()),
			fmt.Sprintf("%s:/etc/letsencrypt", m.cfg.HostCertbotConfDir()),
		},
		Network: m.cfg.DockerNetwork,
	})
	if err != nil {
		return logs, err
	}
	if code != 0 {
		return logs, fmt.Errorf("certbot exited with code %d: %s", code, strings.TrimSpace(logs))
	}
	return logs, nil
}

// CertExists checks whether Let's Encrypt cert files exist on disk for a domain.
func (m *Manager) CertExists(domain string) bool {
	certPath := filepath.Join(m.cfg.CertbotConfDir(), "live", domain, "fullchain.pem")
	_, err := os.Stat(certPath)
	return err == nil
}

func (m *Manager) renderPanelVhost(domain string, hasSSL, forceHTTPS bool) string {
	useSSL := hasSSL && m.CertExists(domain)
	upstream := fmt.Sprintf("runodoo:%s", m.cfg.Port)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }
`, domain))

	if useSSL && forceHTTPS {
		sb.WriteString(`
    location / {
        return 301 https://$host$request_uri;
    }
}
`)
	} else {
		sb.WriteString(fmt.Sprintf(`
    resolver 127.0.0.11 valid=10s ipv6=off;
    set $panel_upstream http://%s;
    client_max_body_size 512m;
    location / {
        proxy_pass $panel_upstream;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
    }
}
`, upstream))
	}

	if useSSL {
		sb.WriteString(fmt.Sprintf(`
server {
    listen 443 ssl;
    http2 on;
    server_name %s;

    ssl_certificate /etc/letsencrypt/live/%s/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/%s/privkey.pem;
    resolver 127.0.0.11 valid=10s ipv6=off;
    set $panel_upstream http://%s;
    client_max_body_size 512m;

    location / {
        proxy_pass $panel_upstream;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 300s;
    }
}
`, domain, domain, domain, upstream))
	}
	return sb.String()
}

func (m *Manager) renderOdooVhost(containerName string, d models.DomainConfig) string {
	domain := strings.TrimSpace(strings.ToLower(d.Domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if idx := strings.IndexByte(domain, '/'); idx != -1 {
		domain = domain[:idx]
	}
	useSSL := d.HasSSL && m.CertExists(domain)

	odooLocations := fmt.Sprintf(`    resolver 127.0.0.11 valid=10s ipv6=off;
    set $odoo_http http://%s:8069;
    set $odoo_ws http://%s:8072;

    client_max_body_size 512m;
    proxy_read_timeout 720s;
    proxy_connect_timeout 720s;
    proxy_send_timeout 720s;
    proxy_buffers 16 64k;
    proxy_buffer_size 128k;

    location /websocket {
        proxy_pass $odoo_ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /longpolling {
        proxy_pass $odoo_ws;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        proxy_pass $odoo_http;
        proxy_redirect off;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    gzip on;
    gzip_types text/css text/scss text/plain text/xml application/xml application/json application/javascript;
`, containerName, containerName)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }
`, domain))

	if useSSL && d.ForceHTTPS {
		sb.WriteString(`
    location / {
        return 301 https://$host$request_uri;
    }
}
`)
	} else {
		sb.WriteString("\n" + odooLocations + "}\n")
	}

	if useSSL {
		sb.WriteString(fmt.Sprintf(`
server {
    listen 443 ssl;
    http2 on;
    server_name %s;

    ssl_certificate /etc/letsencrypt/live/%s/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/%s/privkey.pem;

%s}
`, domain, domain, domain, odooLocations))
	}
	return sb.String()
}

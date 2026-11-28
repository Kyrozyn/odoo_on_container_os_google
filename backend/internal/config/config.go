package config

import (
	"os"
	"path/filepath"
)

// Config holds runtime configuration for RunOdoo.
type Config struct {
	Port            string
	DataDir         string
	HostDataDir     string // Host path mapped to DataDir (for Docker bind mounts when running inside a container)
	DockerSocket    string
	DockerNetwork   string
	NginxContainer  string
	SharedPGName    string
	StaticDir       string
	DefaultPassword string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	dataDir := getEnv("RUNODOO_DATA_DIR", "/var/runodoo")
	hostDataDir := getEnv("RUNODOO_HOST_DATA_DIR", dataDir)

	return &Config{
		Port:            getEnv("RUNODOO_PORT", "3000"),
		DataDir:         dataDir,
		HostDataDir:     hostDataDir,
		DockerSocket:    getEnv("DOCKER_SOCKET", "/var/run/docker.sock"),
		DockerNetwork:   getEnv("RUNODOO_NETWORK", "runodoo-net"),
		NginxContainer:  getEnv("RUNODOO_NGINX_CONTAINER", "runodoo-nginx"),
		SharedPGName:    getEnv("RUNODOO_SHARED_PG_CONTAINER", "runodoo-postgres-shared"),
		StaticDir:       getEnv("RUNODOO_STATIC_DIR", "./static"),
		DefaultPassword: getEnv("RUNODOO_DEFAULT_PASSWORD", "runodoo123"),
	}
}

func (c *Config) StateFile() string {
	return filepath.Join(c.DataDir, "config", "state.json")
}

func (c *Config) InstancesDir() string {
	return filepath.Join(c.DataDir, "instances")
}

func (c *Config) HostInstancesDir() string {
	return filepath.Join(c.HostDataDir, "instances")
}

func (c *Config) AddonsDir() string {
	return filepath.Join(c.DataDir, "addons")
}

func (c *Config) HostAddonsDir() string {
	return filepath.Join(c.HostDataDir, "addons")
}

func (c *Config) BackupsDir() string {
	return filepath.Join(c.DataDir, "backups")
}

func (c *Config) NginxConfDir() string {
	return filepath.Join(c.DataDir, "nginx", "conf.d")
}

func (c *Config) HostNginxConfDir() string {
	return filepath.Join(c.HostDataDir, "nginx", "conf.d")
}

func (c *Config) CertbotWebroot() string {
	return filepath.Join(c.DataDir, "certbot", "www")
}

func (c *Config) HostCertbotWebroot() string {
	return filepath.Join(c.HostDataDir, "certbot", "www")
}

func (c *Config) CertbotConfDir() string {
	return filepath.Join(c.DataDir, "certbot", "conf")
}

func (c *Config) HostCertbotConfDir() string {
	return filepath.Join(c.HostDataDir, "certbot", "conf")
}

func (c *Config) MonitoringDir() string {
	return filepath.Join(c.DataDir, "monitoring")
}

func (c *Config) HostMonitoringDir() string {
	return filepath.Join(c.HostDataDir, "monitoring")
}

// EnsureDirs creates all required runtime directories.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		filepath.Join(c.DataDir, "config"),
		c.InstancesDir(),
		c.AddonsDir(),
		c.BackupsDir(),
		c.NginxConfDir(),
		c.CertbotWebroot(),
		c.CertbotConfDir(),
		filepath.Join(c.DataDir, "postgres-shared"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

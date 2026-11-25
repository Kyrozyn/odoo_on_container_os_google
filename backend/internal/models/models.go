package models

import "time"

// OdooVersion represents supported Odoo Community versions.
type OdooVersion string

const (
	Odoo17 OdooVersion = "17"
	Odoo18 OdooVersion = "18"
	Odoo19 OdooVersion = "19"
	Odoo20 OdooVersion = "20"
)

// SupportedOdooVersions lists all officially supported Community versions and default Docker images.
var SupportedOdooVersions = []OdooVersionInfo{
	{Version: Odoo17, Label: "Odoo 17 Community (LTS)", DefaultImage: "odoo:17.0", Status: "stable"},
	{Version: Odoo18, Label: "Odoo 18 Community", DefaultImage: "odoo:18.0", Status: "stable"},
	{Version: Odoo19, Label: "Odoo 19 Community", DefaultImage: "odoo:19.0", Status: "stable"},
	{Version: Odoo20, Label: "Odoo 20 Community", DefaultImage: "odoo:20.0", Status: "latest"},
}

type OdooVersionInfo struct {
	Version      OdooVersion `json:"version"`
	Label        string      `json:"label"`
	DefaultImage string      `json:"defaultImage"`
	Status       string      `json:"status"`
}

// DBMode defines whether an Odoo instance uses the shared PostgreSQL or a dedicated container.
type DBMode string

const (
	DBModeShared    DBMode = "shared"
	DBModeDedicated DBMode = "dedicated"
)

// DomainConfig describes a custom domain attached to an Odoo instance.
type DomainConfig struct {
	Domain     string `json:"domain"`
	HasSSL     bool   `json:"hasSsl"`
	ForceHTTPS bool   `json:"forceHttps"`
}

// ResourceConfig holds Odoo worker and memory settings.
type ResourceConfig struct {
	Workers         int   `json:"workers"`
	LimitMemorySoft int64 `json:"limitMemorySoft"`
	LimitMemoryHard int64 `json:"limitMemoryHard"`
	LimitTimeCPU    int   `json:"limitTimeCpu"`
	LimitTimeReal   int   `json:"limitTimeReal"`
	MaxCronThreads  int   `json:"maxCronThreads"`
}

// OdooInstance represents a managed Odoo deployment.
type OdooInstance struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Version         OdooVersion       `json:"version"`
	Image           string            `json:"image"`
	ContainerName   string            `json:"containerName"`
	HTTPPort        int               `json:"httpPort"`
	LongpollingPort int               `json:"longpollingPort"`
	ExposePort      bool              `json:"exposePort"`
	Domains         []DomainConfig    `json:"domains"`
	UseRootSubdomain bool             `json:"useRootSubdomain"`
	RootSubdomainSSL bool             `json:"rootSubdomainSsl"`
	DBMode          DBMode            `json:"dbMode"`
	DBContainerName string            `json:"dbContainerName"`
	DBHost          string            `json:"dbHost"`
	DBPort          int               `json:"dbPort"`
	DBUser          string            `json:"dbUser"`
	DBPassword      string            `json:"dbPassword"`
	DefaultDBName     string            `json:"defaultDbName"`
	AdminPassword     string            `json:"adminPassword"`
	AddonRepoIDs      []string          `json:"addonRepoIds"`
	EnvVars           map[string]string `json:"envVars"`
	ConfOptions       map[string]string `json:"confOptions"`
	CustomConf        string            `json:"customConf"`
	Resources       ResourceConfig    `json:"resources"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// InstanceWithStatus augments OdooInstance with live Docker container status.
type InstanceWithStatus struct {
	OdooInstance
	Status        string `json:"status"`        // running, exited, restarting, not_found
	StateText     string `json:"stateText"`
	DBStatus      string `json:"dbStatus"`
	ContainerID   string `json:"containerId"`
	PrimaryURL    string `json:"primaryUrl"`
}

// AddonModule represents a single Odoo module discovered via __manifest__.py.
type AddonModule struct {
	TechnicalName string   `json:"technicalName"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Summary       string   `json:"summary"`
	Author        string   `json:"author"`
	Depends       []string `json:"depends"`
	RelPath       string   `json:"relPath"`
}

// AddonRepo represents an installed addons source (git repository, uploaded zip, or local directory).
type AddonRepo struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	SourceType  string        `json:"sourceType"` // git, zip, local
	GitURL      string        `json:"gitUrl,omitempty"`
	GitBranch   string        `json:"gitBranch,omitempty"`
	LocalPath   string        `json:"localPath"`
	Modules     []AddonModule `json:"modules"`
	UpdatedAt   time.Time     `json:"updatedAt"`
	CreatedAt   time.Time     `json:"createdAt"`
}

// DatabaseInfo represents a PostgreSQL database inside a managed Postgres container.
type DatabaseInfo struct {
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	SizeBytes     int64  `json:"sizeBytes"`
	SizePretty    string `json:"sizePretty"`
	InstanceID    string `json:"instanceId,omitempty"`
	InstanceName  string `json:"instanceName,omitempty"`
	ContainerName string `json:"containerName"`
}

// BackupRecord tracks a saved backup archive (DB dump + filestore).
type BackupRecord struct {
	ID           string    `json:"id"`
	InstanceID   string    `json:"instanceId"`
	InstanceName string    `json:"instanceName"`
	DBName       string    `json:"dbName"`
	FileName     string    `json:"fileName"`
	FilePath     string    `json:"filePath"`
	SizeBytes    int64     `json:"sizeBytes"`
	HasFilestore bool      `json:"hasFilestore"`
	Source       string    `json:"source"` // manual, scheduled, uploaded
	CreatedAt    time.Time `json:"createdAt"`
}

// BackupSchedule defines a recurring cron backup job.
type BackupSchedule struct {
	ID             string     `json:"id"`
	InstanceID     string     `json:"instanceId"`
	DBName         string     `json:"dbName"`
	CronExpression string     `json:"cronExpression"` // e.g., "0 2 * * *"
	RetentionCount int        `json:"retentionCount"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      *time.Time `json:"lastRunAt,omitempty"`
	LastStatus     string     `json:"lastStatus,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// SharedPostgresConfig stores credentials for the shared PostgreSQL container.
type SharedPostgresConfig struct {
	ContainerName string `json:"containerName"`
	Image         string `json:"image"`
	SuperUser     string `json:"superUser"`
	SuperPassword string `json:"superPassword"`
	Port          int    `json:"port"`
}

// PanelSettings stores root domain, SSL, and auth configuration.
type PanelSettings struct {
	RootDomain        string `json:"rootDomain"`       // e.g., "runodoo.example.com" -> panel at panel.runodoo.example.com
	PanelSubdomain    string `json:"panelSubdomain"`   // default: "panel"
	PanelHasSSL       bool   `json:"panelHasSsl"`
	PanelForceHTTPS   bool   `json:"panelForceHttps"`
	DisableDirectPort bool   `json:"disableDirectPort"`
	LetsEncryptEmail  string `json:"letsEncryptEmail"`
	PasswordHash      string `json:"-"`                // never exposed in API JSON
	PasswordChanged   bool   `json:"passwordChanged"`
	JWTSecret         string `json:"-"`
}

// State is the root persistent JSON document.
type State struct {
	Settings        PanelSettings        `json:"settings"`
	SharedPostgres  SharedPostgresConfig `json:"sharedPostgres"`
	Instances       []OdooInstance       `json:"instances"`
	AddonRepos      []AddonRepo          `json:"addonRepos"`
	Backups         []BackupRecord       `json:"backups"`
	BackupSchedules []BackupSchedule     `json:"backupSchedules"`
}

// SystemInfo summarizes host OS, CPU, RAM, disk, and Docker status.
type SystemInfo struct {
	Hostname        string  `json:"hostname"`
	OSPretty        string  `json:"osPretty"`
	IsCOS           bool    `json:"isCos"`
	Arch            string  `json:"arch"`
	CPUCores        int     `json:"cpuCores"`
	TotalRAMMB      int64   `json:"totalRamMb"`
	AvailableRAMMB  int64   `json:"availableRamMb"`
	DiskTotalGB     float64 `json:"diskTotalGb"`
	DiskFreeGB      float64 `json:"diskFreeGb"`
	DockerConnected bool    `json:"dockerConnected"`
	DockerVersion   string  `json:"dockerVersion"`
	PanelPort       string  `json:"panelPort"`
}

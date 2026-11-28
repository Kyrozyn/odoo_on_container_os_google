package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/runodoo/runodoo/internal/addons"
	"github.com/runodoo/runodoo/internal/auth"
	"github.com/runodoo/runodoo/internal/backup"
	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/database"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/monitoring"
	"github.com/runodoo/runodoo/internal/odoo"
	"github.com/runodoo/runodoo/internal/proxy"
	"github.com/runodoo/runodoo/internal/store"
	"github.com/runodoo/runodoo/internal/system"
)

type Server struct {
	cfg        *config.Config
	store      *store.Store
	auth       *auth.Manager
	docker     *docker.Client
	odoo       *odoo.Manager
	db         *database.Manager
	addons     *addons.Manager
	proxy      *proxy.Manager
	backup     *backup.Manager
	monitoring *monitoring.Manager
}

func NewServer(
	cfg *config.Config,
	st *store.Store,
	am *auth.Manager,
	dc *docker.Client,
	om *odoo.Manager,
	dbm *database.Manager,
	adm *addons.Manager,
	pm *proxy.Manager,
	bm *backup.Manager,
	mm *monitoring.Manager,
) *Server {
	return &Server{
		cfg:        cfg,
		store:      st,
		auth:       am,
		docker:     dc,
		odoo:       om,
		db:         dbm,
		addons:     adm,
		proxy:      pm,
		backup:     bm,
		monitoring: mm,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public & internal routes
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("GET /internal/metrics", s.handleInternalMetrics)

	// Protected API routes
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/auth/me", s.handleMe)
	protected.HandleFunc("POST /api/auth/change-password", s.handleChangePassword)

	// System & Settings
	protected.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	protected.HandleFunc("GET /api/system/versions", s.handleVersions)
	protected.HandleFunc("GET /api/settings", s.handleGetSettings)
	protected.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
	protected.HandleFunc("POST /api/settings/port", s.handleChangePanelPort)
	protected.HandleFunc("POST /api/settings/enable-ssl", s.handleEnablePanelSSL)
	protected.HandleFunc("POST /api/proxy/sync", s.handleProxySync)

	// Odoo Instances
	protected.HandleFunc("GET /api/instances", s.handleListInstances)
	protected.HandleFunc("POST /api/instances", s.handleCreateInstance)
	protected.HandleFunc("GET /api/instances/{id}", s.handleGetInstance)
	protected.HandleFunc("PUT /api/instances/{id}", s.handleUpdateInstance)
	protected.HandleFunc("DELETE /api/instances/{id}", s.handleDeleteInstance)
	protected.HandleFunc("POST /api/instances/{id}/start", s.handleStartInstance)
	protected.HandleFunc("POST /api/instances/{id}/stop", s.handleStopInstance)
	protected.HandleFunc("POST /api/instances/{id}/restart", s.handleRestartInstance)
	protected.HandleFunc("GET /api/instances/{id}/logs", s.handleInstanceLogs)
	protected.HandleFunc("GET /api/instances/{id}/deploy-status", s.handleDeployStatus)
	protected.HandleFunc("GET /api/instances/{id}/stats", s.handleInstanceStats)
	protected.HandleFunc("GET /api/instances/{id}/conf", s.handleInstanceConf)
	protected.HandleFunc("POST /api/instances/{id}/ssl", s.handleInstanceSSL)
	protected.HandleFunc("POST /api/instances/{id}/update-apps-list", s.handleUpdateAppsList)
	protected.HandleFunc("POST /api/instances/{id}/clean-missing-modules", s.handleCleanMissingModules)
	protected.HandleFunc("POST /api/instances/{id}/upgrade-modules", s.handleUpgradeModules)
	protected.HandleFunc("POST /api/instances/{id}/upgrade-modules/stream", s.handleUpgradeModulesStream)
	protected.HandleFunc("POST /api/instances/{id}/exec/stream", s.handleExecStream)
	protected.HandleFunc("GET /api/instances/{id}/terminal", s.handleTerminalWS)
	protected.HandleFunc("GET /api/instances/{id}/python-packages", s.handleListPythonPackages)
	protected.HandleFunc("POST /api/instances/{id}/python-packages/install", s.handleInstallPythonPackages)
	protected.HandleFunc("POST /api/instances/{id}/python-packages/uninstall", s.handleUninstallPythonPackages)

	// Databases
	protected.HandleFunc("GET /api/databases", s.handleListAllDatabases)
	protected.HandleFunc("GET /api/instances/{id}/databases", s.handleListInstanceDatabases)
	protected.HandleFunc("POST /api/instances/{id}/databases", s.handleCreateDatabase)
	protected.HandleFunc("POST /api/instances/{id}/databases/duplicate", s.handleDuplicateDatabase)
	protected.HandleFunc("DELETE /api/instances/{id}/databases/{dbName}", s.handleDropDatabase)
	protected.HandleFunc("GET /api/instances/{id}/postgres/metrics", s.handlePostgresMetrics)
	protected.HandleFunc("GET /api/instances/{id}/postgres/config", s.handleGetPostgresConfig)
	protected.HandleFunc("PUT /api/instances/{id}/postgres/config", s.handleUpdatePostgresConfig)
	protected.HandleFunc("POST /api/instances/{id}/postgres/restart", s.handleRestartPostgres)
	protected.HandleFunc("POST /api/instances/{id}/postgres/maintenance", s.handlePostgresMaintenance)

	// Addons
	protected.HandleFunc("GET /api/addons", s.handleListAddons)
	protected.HandleFunc("POST /api/addons/git", s.handleCloneAddon)
	protected.HandleFunc("POST /api/addons/upload", s.handleUploadAddonZip)
	protected.HandleFunc("POST /api/addons/{id}/pull", s.handlePullAddon)
	protected.HandleFunc("POST /api/addons/{id}/rescan", s.handleRescanAddon)
	protected.HandleFunc("DELETE /api/addons/{id}", s.handleDeleteAddon)

	// Backups & Schedules
	protected.HandleFunc("GET /api/backups", s.handleListBackups)
	protected.HandleFunc("POST /api/backups", s.handleCreateBackup)
	protected.HandleFunc("GET /api/backup-jobs", s.handleListBackupJobs)
	protected.HandleFunc("DELETE /api/backup-jobs", s.handleClearBackupJobs)
	protected.HandleFunc("GET /api/backups/{id}/download", s.handleDownloadBackup)
	protected.HandleFunc("POST /api/backups/{id}/restore", s.handleRestoreBackup)
	protected.HandleFunc("POST /api/backups/upload-restore", s.handleUploadRestoreBackup)
	protected.HandleFunc("DELETE /api/backups/{id}", s.handleDeleteBackup)
	protected.HandleFunc("GET /api/backup-schedules", s.handleListSchedules)
	protected.HandleFunc("POST /api/backup-schedules", s.handleCreateSchedule)
	protected.HandleFunc("DELETE /api/backup-schedules/{id}", s.handleDeleteSchedule)

	// Containers (Cluster)
	protected.HandleFunc("GET /api/containers", s.handleListContainers)
	protected.HandleFunc("POST /api/containers/{id}/start", s.handleContainerStart)
	protected.HandleFunc("POST /api/containers/{id}/stop", s.handleContainerStop)
	protected.HandleFunc("POST /api/containers/{id}/restart", s.handleContainerRestart)
	protected.HandleFunc("GET /api/containers/{id}/logs", s.handleContainerLogs)
	protected.HandleFunc("GET /api/containers/{id}/stats", s.handleContainerStats)

	// Monitoring (Per-instance Prometheus & Grafana)
	protected.HandleFunc("GET /api/instances/{id}/monitoring/status", s.handleMonitoringStatus)
	protected.HandleFunc("POST /api/instances/{id}/monitoring/install", s.handleMonitoringInstall)
	protected.HandleFunc("POST /api/instances/{id}/monitoring/install/stream", s.handleMonitoringInstallStream)
	protected.HandleFunc("PUT /api/instances/{id}/monitoring/config", s.handleMonitoringUpdateConfig)
	protected.HandleFunc("POST /api/instances/{id}/monitoring/account", s.handleMonitoringAccount)
	protected.HandleFunc("POST /api/instances/{id}/monitoring/ssl", s.handleMonitoringSSL)
	protected.HandleFunc("POST /api/instances/{id}/monitoring/control", s.handleMonitoringControl)
	protected.HandleFunc("POST /api/instances/{id}/monitoring/resync", s.handleMonitoringResync)
	protected.HandleFunc("DELETE /api/instances/{id}/monitoring", s.handleMonitoringUninstall)

	mux.Handle("/api/", s.auth.Middleware(protected))

	// Static SPA frontend
	mux.Handle("/", s.spaHandler())

	return s.withPanelAccessControl(withCORS(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "app": "RunOdoo"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.auth.VerifyPassword(body.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid password")
		return
	}
	token, err := s.auth.GenerateToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	st := s.store.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"token":           token,
		"passwordChanged": st.Settings.PasswordChanged,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"user":            "admin",
		"passwordChanged": st.Settings.PasswordChanged,
		"rootDomain":      st.Settings.RootDomain,
	})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.auth.VerifyPassword(body.CurrentPassword) {
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	if err := s.auth.ChangePassword(body.NewPassword); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	dockerOK := false
	dockerVer := ""
	if v, err := s.docker.Version(ctx); err == nil {
		dockerOK = true
		dockerVer = v.Version
	}
	panelPort := s.currentHostPanelPort(ctx)
	info := system.CollectSystemInfo(s.cfg.DataDir, panelPort, dockerOK, dockerVer)
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, models.SupportedOdooVersions)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	settings := st.Settings
	settings.PanelPort = s.currentHostPanelPort(r.Context())
	if panelDomain := proxy.PanelDomain(settings.RootDomain, settings.PanelSubdomain); panelDomain != "" {
		settings.PanelHasSSL = s.proxy.CertExists(panelDomain)
	} else {
		settings.PanelHasSSL = false
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RootDomain        *string `json:"rootDomain"`
		PanelSubdomain    *string `json:"panelSubdomain"`
		LetsEncryptEmail  *string `json:"letsEncryptEmail"`
		PanelForceHTTPS   *bool   `json:"panelForceHttps"`
		DisableDirectPort *bool   `json:"disableDirectPort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	prev := s.store.Get().Settings
	var updated models.PanelSettings
	err := s.store.Update(func(st *models.State) error {
		if body.RootDomain != nil {
			st.Settings.RootDomain = proxy.NormalizeDomain(*body.RootDomain)
		}
		if body.PanelSubdomain != nil {
			sub := strings.Trim(strings.TrimSpace(strings.ToLower(*body.PanelSubdomain)), ".")
			if sub == "" {
				sub = "panel"
			}
			st.Settings.PanelSubdomain = sub
		}
		if body.LetsEncryptEmail != nil {
			st.Settings.LetsEncryptEmail = strings.TrimSpace(*body.LetsEncryptEmail)
		}
		if body.PanelForceHTTPS != nil {
			st.Settings.PanelForceHTTPS = *body.PanelForceHTTPS
		}
		if body.DisableDirectPort != nil {
			st.Settings.DisableDirectPort = *body.DisableDirectPort
		}

		if st.Settings.RootDomain == "" {
			if body.DisableDirectPort != nil && *body.DisableDirectPort {
				return fmt.Errorf("configure a Root Domain before disabling direct port access")
			}
			st.Settings.DisableDirectPort = false
			st.Settings.PanelHasSSL = false
		} else {
			panelDomain := proxy.PanelDomain(st.Settings.RootDomain, st.Settings.PanelSubdomain)
			st.Settings.PanelHasSSL = s.proxy.CertExists(panelDomain)
		}

		updated = st.Settings
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var syncErr error
	if updated.DisableDirectPort {
		syncErr = s.proxy.EnsureNginx(r.Context())
	} else {
		syncErr = s.proxy.SyncAll(r.Context())
	}
	if syncErr != nil {
		if updated.DisableDirectPort && !prev.DisableDirectPort {
			_ = s.store.Update(func(st *models.State) error {
				st.Settings.DisableDirectPort = false
				return nil
			})
		}
		writeErr(w, http.StatusInternalServerError, "settings updated, but failed to reload Nginx: "+syncErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) inspectSelfContainer(ctx context.Context) (*docker.ContainerInspect, string) {
	if host, err := os.Hostname(); err == nil && host != "" {
		if info, err := s.docker.InspectContainer(ctx, host); err == nil {
			name := strings.TrimPrefix(info.Name, "/")
			if name == "" {
				name = host
			}
			return info, name
		}
	}
	if info, err := s.docker.InspectContainer(ctx, "runodoo"); err == nil {
		name := strings.TrimPrefix(info.Name, "/")
		if name == "" {
			name = "runodoo"
		}
		return info, name
	}
	return nil, ""
}

func (s *Server) currentHostPanelPort(ctx context.Context) string {
	info, _ := s.inspectSelfContainer(ctx)
	if info != nil {
		key := s.cfg.Port + "/tcp"
		if bindings, ok := info.HostConfig.PortBindings[key]; ok && len(bindings) > 0 && bindings[0].HostPort != "" {
			return bindings[0].HostPort
		}
		for _, bindings := range info.HostConfig.PortBindings {
			if len(bindings) > 0 && bindings[0].HostPort != "" {
				return bindings[0].HostPort
			}
		}
	}
	return s.cfg.Port
}

func (s *Server) handleChangePanelPort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Port < 1 || body.Port > 65535 {
		writeErr(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	newPortStr := strconv.Itoa(body.Port)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	selfInfo, selfName := s.inspectSelfContainer(ctx)
	if selfInfo == nil {
		writeErr(w, http.StatusBadRequest, "changing panel port is only supported when RunOdoo is running inside Docker")
		return
	}

	currentPort := s.currentHostPanelPort(ctx)
	if currentPort == newPortStr {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"port":       newPortStr,
			"recreating": false,
		})
		return
	}

	// Check if another running container already uses the target port on the host.
	containers, err := s.docker.ListContainers(ctx, false, "")
	if err == nil {
		for _, c := range containers {
			if c.ID == selfInfo.ID {
				continue
			}
			for _, p := range c.Ports {
				if p.PublicPort == body.Port {
					cName := c.ID[:12]
					if len(c.Names) > 0 {
						cName = strings.TrimPrefix(c.Names[0], "/")
					}
					writeErr(w, http.StatusConflict, fmt.Sprintf("port %d is already in use by container %s", body.Port, cName))
					return
				}
			}
		}
	}

	// Also check if something on the container's own loopback/port (not applicable for host,
	// so the helper container performs atomic rename + rollback if Docker bind fails).
	networkName := s.cfg.DockerNetwork
	for netName := range selfInfo.NetworkSettings.Networks {
		if netName != "" && netName != "bridge" {
			networkName = netName
			break
		}
	}

	restartPolicy := selfInfo.HostConfig.RestartPolicy.Name
	if restartPolicy == "" {
		restartPolicy = "unless-stopped"
	}

	recreateSpec := docker.RecreateSpec{
		TargetContainer: selfName,
		NewSpec: docker.CreateContainerSpec{
			Name:          selfName,
			Image:         selfInfo.Config.Image,
			Env:           selfInfo.Config.Env,
			Labels:        selfInfo.Config.Labels,
			Binds:         selfInfo.HostConfig.Binds,
			PortMappings:  map[string]string{s.cfg.Port + "/tcp": newPortStr},
			Network:       networkName,
			RestartPolicy: restartPolicy,
		},
	}

	rawSpec, err := json.Marshal(recreateSpec)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to encode recreate spec: "+err.Error())
		return
	}

	const updaterName = "runodoo-port-updater"
	_ = s.docker.RemoveContainer(ctx, updaterName, true)

	helperID, err := s.docker.CreateContainer(ctx, docker.CreateContainerSpec{
		Name:          updaterName,
		Image:         selfInfo.Config.Image,
		Env:           []string{"RUNODOO_RECREATE_SPEC=" + string(rawSpec), "DOCKER_SOCKET=" + s.cfg.DockerSocket},
		Binds:         []string{s.cfg.DockerSocket + ":/var/run/docker.sock"},
		RestartPolicy: "no",
		AutoRemove:    true,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create port updater helper: "+err.Error())
		return
	}
	if err := s.docker.StartContainer(ctx, helperID); err != nil {
		_ = s.docker.RemoveContainer(ctx, helperID, true)
		writeErr(w, http.StatusInternalServerError, "failed to start port updater helper: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"port":       newPortStr,
		"recreating": true,
	})
}

func (s *Server) handleEnablePanelSSL(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	panelDomain := proxy.PanelDomain(st.Settings.RootDomain, st.Settings.PanelSubdomain)
	if panelDomain == "" {
		writeErr(w, http.StatusBadRequest, "set a Root Domain first before enabling SSL")
		return
	}

	logs, err := s.proxy.IssueSSL(r.Context(), panelDomain, st.Settings.LetsEncryptEmail)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "logs": logs})
		return
	}
	_ = s.store.Update(func(s *models.State) error {
		s.Settings.PanelHasSSL = true
		return nil
	})
	if err := s.proxy.SyncAll(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "SSL issued, but failed to reload Nginx: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "domain": panelDomain, "logs": logs})
}

func (s *Server) handleProxySync(w http.ResponseWriter, r *http.Request) {
	if err := s.proxy.EnsureNginx(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Instances ---

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.odoo.ListWithStatus(r.Context()))
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req odoo.CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	inst, err := s.odoo.CreateInstance(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := s.odoo.GetWithStatus(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req odoo.UpdateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	inst, err := s.odoo.UpdateInstance(r.Context(), id, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleteData := r.URL.Query().Get("deleteData") == "true"
	if err := s.odoo.DeleteInstance(r.Context(), id, deleteData); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStartInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if err := s.docker.StartContainer(r.Context(), inst.ContainerName); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStopInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if err := s.docker.StopContainer(r.Context(), inst.ContainerName, 15); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRestartInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if err := s.docker.RestartContainer(r.Context(), inst.ContainerName, 15); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleInstanceLogs(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	logs, err := s.docker.ContainerLogs(r.Context(), inst.ContainerName, tail)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleDeployStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.odoo.GetDeployProgress(r.PathValue("id")))
}

func (s *Server) handleInstanceStats(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	stats, err := s.docker.ContainerStats(r.Context(), inst.ContainerName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleInstanceConf(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generatedConf": s.odoo.ReadOdooConf(inst),
		"customConf":    inst.CustomConf,
		"confOptions":   s.odoo.EffectiveConfOptions(inst),
		"orderedKeys":   odoo.StandardOdooConfKeys,
		"envVars":       inst.EnvVars,
	})
}

func (s *Server) handleInstanceSSL(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		Domain string `json:"domain"`
		Email  string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	logs, err := s.proxy.IssueSSL(r.Context(), body.Domain, body.Email)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "logs": logs})
		return
	}

	st := s.store.Get()
	rootSub := inst.Name + "." + st.Settings.RootDomain
	_ = s.store.Update(func(s *models.State) error {
		for i := range s.Instances {
			if s.Instances[i].ID == inst.ID {
				if st.Settings.RootDomain != "" && strings.EqualFold(body.Domain, rootSub) {
					s.Instances[i].RootSubdomainSSL = true
				}
				for j := range s.Instances[i].Domains {
					if strings.EqualFold(s.Instances[i].Domains[j].Domain, body.Domain) {
						s.Instances[i].Domains[j].HasSSL = true
						s.Instances[i].Domains[j].ForceHTTPS = true
					}
				}
			}
		}
		return nil
	})
	_ = s.proxy.SyncAll(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "logs": logs})
}

func (s *Server) handleUpdateAppsList(w http.ResponseWriter, r *http.Request) {
	updated, out, err := s.odoo.UpdateAppsList(r.Context(), r.PathValue("id"), nil, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "output": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": updated, "output": out})
}

func (s *Server) handleCleanMissingModules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			writeErr(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	mode := strings.TrimSpace(strings.ToLower(body.Mode))
	if mode == "" {
		mode = "all"
	}
	switch mode {
	case "all", "custom", "stalled":
	default:
		writeErr(w, http.StatusBadRequest, "invalid mode: must be all, custom, or stalled")
		return
	}
	results, out, err := s.odoo.CleanMissingModules(r.Context(), r.PathValue("id"), mode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "output": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "results": results, "output": out})
}

func (s *Server) handleUpgradeModules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DBName  string   `json:"dbName"`
		Modules []string `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	out, err := s.odoo.UpgradeModules(r.Context(), r.PathValue("id"), body.DBName, body.Modules)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "output": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})
}

type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

func (s *Server) handleUpgradeModulesStream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DBName  string   `json:"dbName"`
		Modules []string `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.DBName) == "" || len(body.Modules) == 0 {
		writeErr(w, http.StatusBadRequest, "database name and at least one module are required")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, _ := w.(http.Flusher)
	fw := &flushWriter{w: w, f: flusher}
	_, _ = fmt.Fprintf(fw, "$ odoo -c /etc/odoo/odoo.conf -d %s -u %s --stop-after-init --no-http --workers=0 --max-cron-threads=0\n",
		body.DBName, strings.Join(body.Modules, ","))

	code, err := s.odoo.UpgradeModulesStream(r.Context(), r.PathValue("id"), body.DBName, body.Modules, fw)
	if err != nil {
		_, _ = fmt.Fprintf(fw, "\n__RUNODOO_ERROR__:%s\n", err.Error())
		return
	}
	_, _ = fmt.Fprintf(fw, "\n__RUNODOO_EXIT__:%d\n", code)
}

func (s *Server) handleExecStream(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(body.Command) == "" {
		writeErr(w, http.StatusBadRequest, "command is required")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, _ := w.(http.Flusher)
	fw := &flushWriter{w: w, f: flusher}
	code, err := s.odoo.ExecStream(r.Context(), r.PathValue("id"), body.Command, body.Cwd, fw)
	if err != nil {
		_, _ = fmt.Fprintf(fw, "\n__RUNODOO_ERROR__:%s\n", err.Error())
		return
	}
	_, _ = fmt.Fprintf(fw, "\n__RUNODOO_EXIT__:%d\n", code)
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	execID, dockerConn, dockerReader, err := s.odoo.OpenTerminal(r.Context(), r.PathValue("id"), rows, cols)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	ws, err := upgradeWebSocket(w, r)
	if err != nil {
		_ = dockerConn.Close()
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	defer func() {
		_ = dockerConn.Close()
		_ = ws.Close()
	}()

	done := make(chan struct{}, 2)

	// Docker PTY -> WebSocket
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 8192)
		for {
			n, err := dockerReader.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(wsOpBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> Docker PTY
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			op, payload, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if op == wsOpText && len(payload) > 0 && payload[0] == '{' {
				var ctrl struct {
					Type string `json:"type"`
					Cols int    `json:"cols"`
					Rows int    `json:"rows"`
				}
				if json.Unmarshal(payload, &ctrl) == nil && ctrl.Type == "resize" && ctrl.Cols > 0 && ctrl.Rows > 0 {
					_ = s.odoo.ResizeTerminal(context.Background(), execID, ctrl.Rows, ctrl.Cols)
					continue
				}
			}
			if len(payload) > 0 {
				if _, err := dockerConn.Write(payload); err != nil {
					return
				}
			}
		}
	}()

	<-done
}

func (s *Server) handleListPythonPackages(w http.ResponseWriter, r *http.Request) {
	info, err := s.odoo.ListPythonPackages(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleInstallPythonPackages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Packages     []string `json:"packages"`
		Requirements string   `json:"requirements"`
		Upgrade      bool     `json:"upgrade"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	specs := append([]string{}, body.Packages...)
	if strings.TrimSpace(body.Requirements) != "" {
		specs = append(specs, body.Requirements)
	}
	out, err := s.odoo.InstallPythonPackages(r.Context(), r.PathValue("id"), specs, body.Upgrade)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "output": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})
}

func (s *Server) handleUninstallPythonPackages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Packages []string `json:"packages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	out, err := s.odoo.UninstallPythonPackages(r.Context(), r.PathValue("id"), body.Packages)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "output": out})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": out})
}

// --- Databases ---

func (s *Server) handleListAllDatabases(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	var all []models.DatabaseInfo
	for _, inst := range st.Instances {
		dbs, err := s.db.ListInstanceDatabases(r.Context(), inst)
		if err == nil {
			all = append(all, dbs...)
		}
	}
	if all == nil {
		all = []models.DatabaseInfo{}
	}
	writeJSON(w, http.StatusOK, all)
}

func (s *Server) handleListInstanceDatabases(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	dbs, err := s.db.ListInstanceDatabases(r.Context(), inst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if dbs == nil {
		dbs = []models.DatabaseInfo{}
	}
	writeJSON(w, http.StatusOK, dbs)
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var req database.CreateOdooDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.db.CreateOdooDatabase(r.Context(), inst, req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "name": req.Name})
}

func (s *Server) handleDuplicateDatabase(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		SourceName string `json:"sourceName"`
		TargetName string `json:"targetName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.db.DuplicateDatabase(r.Context(), inst, body.SourceName, body.TargetName); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleDropDatabase(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	dbName := r.PathValue("dbName")
	if err := s.db.DropDatabase(r.Context(), inst, dbName); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePostgresMetrics(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	metrics, err := s.db.GetPostgresMetrics(r.Context(), inst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleGetPostgresConfig(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	settings, err := s.db.GetPostgresConfig(r.Context(), inst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdatePostgresConfig(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		Updates map[string]string `json:"updates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings, err := s.db.UpdatePostgresConfig(r.Context(), inst, body.Updates)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleRestartPostgres(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if err := s.db.RestartPostgres(r.Context(), inst); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePostgresMaintenance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		Action string `json:"action"`
		DBName string `json:"dbName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	msg, err := s.db.RunMaintenance(r.Context(), inst, body.Action, body.DBName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// --- Addons ---

func (s *Server) handleListAddons(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, st.AddonRepos)
}

func (s *Server) handleCloneAddon(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		GitURL string `json:"gitUrl"`
		Branch string `json:"branch"`
		Token  string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	repo, err := s.addons.CloneGitRepo(r.Context(), body.Name, body.GitURL, body.Branch, body.Token)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) handleUploadAddonZip(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "failed to parse upload: "+err.Error())
		return
	}
	name := r.FormValue("name")
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	if name == "" {
		name = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}

	tmp, err := os.CreateTemp("", "runodoo-addon-*.zip")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp.Close()

	repo, err := s.addons.UploadZip(name, tmpPath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) handlePullAddon(w http.ResponseWriter, r *http.Request) {
	repo, err := s.addons.PullGitRepo(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleRescanAddon(w http.ResponseWriter, r *http.Request) {
	repo, err := s.addons.RescanRepo(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleDeleteAddon(w http.ResponseWriter, r *http.Request) {
	if err := s.addons.DeleteRepo(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Backups & Schedules ---

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, st.Backups)
}

func (s *Server) handleListBackupJobs(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, st.BackupJobs)
}

func (s *Server) handleClearBackupJobs(w http.ResponseWriter, r *http.Request) {
	if err := s.backup.ClearFinishedJobs(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID string `json:"instanceId"`
		DBName     string `json:"dbName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	inst, ok := s.store.FindInstance(body.InstanceID)
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	job, err := s.backup.EnqueueBackup(inst, body.DBName, "manual", "", 0)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st := s.store.Get()
	for _, b := range st.Backups {
		if b.ID == id {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, b.FileName))
			http.ServeFile(w, r, b.FilePath)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "backup not found")
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		InstanceID   string `json:"instanceId"`
		TargetDBName string `json:"targetDbName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	st := s.store.Get()
	var backupRec *models.BackupRecord
	for i := range st.Backups {
		if st.Backups[i].ID == id {
			backupRec = &st.Backups[i]
			break
		}
	}
	if backupRec == nil {
		writeErr(w, http.StatusNotFound, "backup not found")
		return
	}

	instID := body.InstanceID
	if instID == "" {
		instID = backupRec.InstanceID
	}
	inst, ok := s.store.FindInstance(instID)
	if !ok {
		writeErr(w, http.StatusNotFound, "target instance not found")
		return
	}
	targetDB := body.TargetDBName
	if targetDB == "" {
		targetDB = backupRec.DBName
	}

	job, err := s.backup.EnqueueRestore(inst, backupRec.FilePath, targetDB, "restore", backupRec.ID, backupRec.FileName, false)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleUploadRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1024 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "failed parsing multipart upload: "+err.Error())
		return
	}
	instanceID := r.FormValue("instanceId")
	targetDB := r.FormValue("targetDbName")
	inst, ok := s.store.FindInstance(instanceID)
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}

	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "runodoo-upload-restore-*.zip")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp.Close()

	job, err := s.backup.EnqueueRestore(inst, tmpPath, targetDB, "upload-restore", "", hdr.Filename, true)
	if err != nil {
		_ = os.Remove(tmpPath)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if err := s.backup.DeleteBackup(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, st.BackupSchedules)
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID     string `json:"instanceId"`
		DBName         string `json:"dbName"`
		CronExpression string `json:"cronExpression"`
		RetentionCount int    `json:"retentionCount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := s.store.FindInstance(body.InstanceID); !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	if body.CronExpression == "" {
		body.CronExpression = "0 2 * * *"
	}
	if body.RetentionCount <= 0 {
		body.RetentionCount = 7
	}

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	sched := models.BackupSchedule{
		ID:             hex.EncodeToString(b),
		InstanceID:     body.InstanceID,
		DBName:         body.DBName,
		CronExpression: body.CronExpression,
		RetentionCount: body.RetentionCount,
		Enabled:        true,
		CreatedAt:      time.Now(),
	}
	if err := s.store.Update(func(st *models.State) error {
		st.BackupSchedules = append(st.BackupSchedules, sched)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.backup.ReloadSchedules()
	writeJSON(w, http.StatusCreated, sched)
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.Update(func(st *models.State) error {
		filtered := make([]models.BackupSchedule, 0, len(st.BackupSchedules))
		for _, sc := range st.BackupSchedules {
			if sc.ID != id {
				filtered = append(filtered, sc)
			}
		}
		st.BackupSchedules = filtered
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.backup.ReloadSchedules()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Containers ---

func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	onlyManaged := r.URL.Query().Get("managed") == "true"
	label := ""
	if onlyManaged {
		label = "runodoo.managed=true"
	}
	list, err := s.docker.ListContainers(r.Context(), true, label)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []docker.ContainerSummary{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleContainerStart(w http.ResponseWriter, r *http.Request) {
	if err := s.docker.StartContainer(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	if err := s.docker.StopContainer(r.Context(), r.PathValue("id"), 15); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleContainerRestart(w http.ResponseWriter, r *http.Request) {
	if err := s.docker.RestartContainer(r.Context(), r.PathValue("id"), 15); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	logs, err := s.docker.ContainerLogs(r.Context(), r.PathValue("id"), tail)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleContainerStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.docker.ContainerStats(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- Monitoring (Prometheus & Grafana) ---

func isPrivateOrLoopback(remoteAddr string) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func (s *Server) handleInternalMetrics(w http.ResponseWriter, r *http.Request) {
	if !isPrivateOrLoopback(r.RemoteAddr) {
		writeErr(w, http.StatusForbidden, "metrics endpoint is internal only")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(s.monitoring.RenderMetrics(r.Context(), r.URL.Query().Get("instance"))))
}

func (s *Server) handleMonitoringStatus(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	writeJSON(w, http.StatusOK, s.monitoring.Status(r.Context(), inst))
}

func (s *Server) handleMonitoringInstall(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var req monitoring.InstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, err := s.monitoring.Install(r.Context(), inst, req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleMonitoringInstallStream(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var req monitoring.InstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, _ := w.(http.Flusher)
	fw := &flushWriter{w: w, f: flusher}
	_, _ = fmt.Fprintf(fw, "$ runodoo monitoring install --instance %s\n", inst.Name)

	_, err := s.monitoring.InstallWithLog(r.Context(), inst, req, func(line string) {
		_, _ = fmt.Fprint(fw, line)
	})
	if err != nil {
		_, _ = fmt.Fprintf(fw, "\n__RUNODOO_ERROR__:%s\n", err.Error())
		return
	}
	_, _ = fmt.Fprintf(fw, "\n__RUNODOO_EXIT__:0\n")
}

func (s *Server) handleMonitoringUpdateConfig(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var req monitoring.UpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, err := s.monitoring.UpdateConfig(r.Context(), inst, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleMonitoringAccount(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, err := s.monitoring.UpdateGrafanaAccount(r.Context(), inst, body.Username, body.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleMonitoringSSL(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Domain) == "" {
		writeErr(w, http.StatusBadRequest, "domain is required")
		return
	}
	status, err := s.monitoring.IssueDomainSSL(r.Context(), inst, body.Domain)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleMonitoringControl(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	var body struct {
		Target string `json:"target"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.monitoring.Control(r.Context(), inst, body.Target, body.Action); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.monitoring.Status(r.Context(), inst))
}

func (s *Server) handleMonitoringResync(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	status, err := s.monitoring.Resync(r.Context(), inst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleMonitoringUninstall(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.store.FindInstance(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "instance not found")
		return
	}
	removeData := r.URL.Query().Get("removeData") != "false"
	status, err := s.monitoring.Uninstall(r.Context(), inst, removeData)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// --- Static SPA Fallback ---

func (s *Server) spaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, http.StatusNotFound, "endpoint not found")
			return
		}
		staticDir := s.cfg.StaticDir
		indexFile := filepath.Join(staticDir, "index.html")
		if _, err := os.Stat(indexFile); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:sans-serif;padding:2rem"><h2>RunOdoo API Server</h2><p>Frontend static assets not found in <code>` + staticDir + `</code>. Build the frontend first.</p></body></html>`))
			return
		}
		if r.URL.Path == "/" {
			http.ServeFile(w, r, indexFile)
			return
		}
		absStaticDir, err := filepath.Abs(staticDir)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		reqPath := strings.TrimPrefix(r.URL.Path, "/")
		cleaned := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(reqPath, "\\", "/")))
		if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		target, err := filepath.Abs(filepath.Join(absStaticDir, cleaned))
		if err != nil || !strings.HasPrefix(target, absStaticDir+string(os.PathSeparator)) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			http.ServeFile(w, r, target)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	})
}

func hostWithoutPort(host string) string {
	h := strings.TrimSpace(strings.ToLower(host))
	if parsed, _, err := net.SplitHostPort(h); err == nil {
		return parsed
	}
	return h
}

func (s *Server) withPanelAccessControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" || r.URL.Path == "/internal/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		st := s.store.Get()
		panelDomain := proxy.PanelDomain(st.Settings.RootDomain, st.Settings.PanelSubdomain)
		if panelDomain == "" {
			next.ServeHTTP(w, r)
			return
		}

		viaProxy := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-RunOdoo-Proxy")), []byte(s.store.ProxyToken())) == 1
		isPanelHost := strings.EqualFold(hostWithoutPort(r.Host), panelDomain)

		if st.Settings.DisableDirectPort && !(viaProxy && isPanelHost) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeErr(w, http.StatusForbidden, "Direct port access is disabled. Please access RunOdoo via your configured Panel Domain.")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:sans-serif;padding:2rem"><h2>Direct Port Access Disabled</h2><p>Direct access via the panel port is disabled. Please access RunOdoo via your configured Panel Domain.</p></body></html>`))
			return
		}

		if st.Settings.PanelForceHTTPS && viaProxy && isPanelHost && s.proxy.CertExists(panelDomain) {
			if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") {
				http.Redirect(w, r, "https://"+panelDomain+r.URL.RequestURI(), http.StatusMovedPermanently)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

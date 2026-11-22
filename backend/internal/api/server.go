package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/runodoo/runodoo/internal/odoo"
	"github.com/runodoo/runodoo/internal/proxy"
	"github.com/runodoo/runodoo/internal/store"
	"github.com/runodoo/runodoo/internal/system"
)

type Server struct {
	cfg    *config.Config
	store  *store.Store
	auth   *auth.Manager
	docker *docker.Client
	odoo   *odoo.Manager
	db     *database.Manager
	addons *addons.Manager
	proxy  *proxy.Manager
	backup *backup.Manager
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
) *Server {
	return &Server{
		cfg:    cfg,
		store:  st,
		auth:   am,
		docker: dc,
		odoo:   om,
		db:     dbm,
		addons: adm,
		proxy:  pm,
		backup: bm,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)

	// Protected API routes
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/auth/me", s.handleMe)
	protected.HandleFunc("POST /api/auth/change-password", s.handleChangePassword)

	// System & Settings
	protected.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	protected.HandleFunc("GET /api/system/versions", s.handleVersions)
	protected.HandleFunc("GET /api/settings", s.handleGetSettings)
	protected.HandleFunc("PUT /api/settings", s.handleUpdateSettings)
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
	protected.HandleFunc("POST /api/instances/{id}/upgrade-modules", s.handleUpgradeModules)

	// Databases
	protected.HandleFunc("GET /api/databases", s.handleListAllDatabases)
	protected.HandleFunc("GET /api/instances/{id}/databases", s.handleListInstanceDatabases)
	protected.HandleFunc("POST /api/instances/{id}/databases", s.handleCreateDatabase)
	protected.HandleFunc("POST /api/instances/{id}/databases/duplicate", s.handleDuplicateDatabase)
	protected.HandleFunc("DELETE /api/instances/{id}/databases/{dbName}", s.handleDropDatabase)

	// Addons
	protected.HandleFunc("GET /api/addons", s.handleListAddons)
	protected.HandleFunc("GET /api/addons/recommended", s.handleListRecommendedAddons)
	protected.HandleFunc("POST /api/addons/git", s.handleCloneAddon)
	protected.HandleFunc("POST /api/addons/upload", s.handleUploadAddonZip)
	protected.HandleFunc("POST /api/addons/{id}/pull", s.handlePullAddon)
	protected.HandleFunc("POST /api/addons/{id}/rescan", s.handleRescanAddon)
	protected.HandleFunc("DELETE /api/addons/{id}", s.handleDeleteAddon)

	// Backups & Schedules
	protected.HandleFunc("GET /api/backups", s.handleListBackups)
	protected.HandleFunc("POST /api/backups", s.handleCreateBackup)
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

	mux.Handle("/api/", s.auth.Middleware(protected))

	// Static SPA frontend
	mux.Handle("/", s.spaHandler())

	return withCORS(mux)
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
	info := system.CollectSystemInfo(s.cfg.DataDir, s.cfg.Port, dockerOK, dockerVer)
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, models.SupportedOdooVersions)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, st.Settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RootDomain       *string `json:"rootDomain"`
		PanelSubdomain   *string `json:"panelSubdomain"`
		LetsEncryptEmail *string `json:"letsEncryptEmail"`
		PanelForceHTTPS  *bool   `json:"panelForceHttps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var updated models.PanelSettings
	err := s.store.Update(func(st *models.State) error {
		if body.RootDomain != nil {
			st.Settings.RootDomain = strings.TrimSpace(strings.ToLower(*body.RootDomain))
		}
		if body.PanelSubdomain != nil {
			sub := strings.TrimSpace(strings.ToLower(*body.PanelSubdomain))
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
		updated = st.Settings
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.proxy.SyncAll(r.Context())
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleEnablePanelSSL(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	if st.Settings.RootDomain == "" {
		writeErr(w, http.StatusBadRequest, "set a Root Domain first before enabling SSL")
		return
	}
	panelDomain := st.Settings.PanelSubdomain + "." + st.Settings.RootDomain
	if st.Settings.PanelSubdomain == "" || st.Settings.PanelSubdomain == "@" {
		panelDomain = st.Settings.RootDomain
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
	_ = s.proxy.SyncAll(r.Context())
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

// --- Addons ---

func (s *Server) handleListAddons(w http.ResponseWriter, r *http.Request) {
	st := s.store.Get()
	writeJSON(w, http.StatusOK, st.AddonRepos)
}

func (s *Server) handleListRecommendedAddons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.addons.ListRecommendedAddons())
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
	rec, err := s.backup.CreateBackup(r.Context(), inst, body.DBName, "manual")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
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

	if err := s.backup.RestoreBackup(r.Context(), inst, backupRec.FilePath, targetDB); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

	file, _, err := r.FormFile("file")
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
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp.Close()

	if err := s.backup.RestoreBackup(r.Context(), inst, tmpPath, targetDB); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		target := filepath.Join(staticDir, cleanPath)
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			http.ServeFile(w, r, target)
			return
		}
		http.ServeFile(w, r, indexFile)
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

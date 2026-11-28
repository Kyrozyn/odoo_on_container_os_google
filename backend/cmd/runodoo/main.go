package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/runodoo/runodoo/internal/addons"
	"github.com/runodoo/runodoo/internal/api"
	"github.com/runodoo/runodoo/internal/auth"
	"github.com/runodoo/runodoo/internal/backup"
	"github.com/runodoo/runodoo/internal/config"
	"github.com/runodoo/runodoo/internal/database"
	"github.com/runodoo/runodoo/internal/docker"
	"github.com/runodoo/runodoo/internal/monitoring"
	"github.com/runodoo/runodoo/internal/odoo"
	"github.com/runodoo/runodoo/internal/proxy"
	"github.com/runodoo/runodoo/internal/store"
)

func main() {
	if rawSpec := os.Getenv("RUNODOO_RECREATE_SPEC"); rawSpec != "" {
		sock := os.Getenv("DOCKER_SOCKET")
		if sock == "" {
			sock = "/var/run/docker.sock"
		}
		dc := docker.NewClient(sock)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := docker.RunRecreateHelper(ctx, dc, rawSpec); err != nil {
			log.Fatalf("recreate helper failed: %v", err)
		}
		return
	}

	cfg := config.Load()
	if err := cfg.EnsureDirs(); err != nil {
		log.Fatalf("failed to initialize data directories (%s): %v", cfg.DataDir, err)
	}

	st, err := store.New(cfg.StateFile(), cfg.DefaultPassword, cfg.SharedPGName)
	if err != nil {
		log.Fatalf("failed to initialize state store: %v", err)
	}

	dc := docker.NewClient(cfg.DockerSocket)
	authMgr := auth.NewManager(st)
	addonMgr := addons.NewManager(cfg, st)
	proxyMgr := proxy.NewManager(cfg, st, dc)
	dbMgr := database.NewManager(cfg, st, dc)
	odooMgr := odoo.NewManager(cfg, st, dc, dbMgr, addonMgr, proxyMgr)
	backupMgr := backup.NewManager(cfg, st, dc, dbMgr)
	monMgr := monitoring.NewManager(cfg, st, dc, dbMgr, proxyMgr)

	// Start scheduled backup cron runner
	backupMgr.StartScheduler()
	defer backupMgr.StopScheduler()

	// Best-effort network & nginx sync on startup
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := dc.Ping(ctx); err == nil {
			_ = dc.EnsureNetwork(ctx, cfg.DockerNetwork)
			_ = proxyMgr.SyncAll(ctx)
		} else {
			log.Printf("warning: docker daemon not reachable at %s: %v", cfg.DockerSocket, err)
		}
	}()

	srv := api.NewServer(cfg, st, authMgr, dc, odooMgr, dbMgr, addonMgr, proxyMgr, backupMgr, monMgr)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		log.Printf("RunOdoo Control Panel listening on http://0.0.0.0:%s (data: %s)", cfg.Port, cfg.DataDir)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down RunOdoo...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

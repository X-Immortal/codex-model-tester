package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chatgpt-codex-proxy/internal/config"
	"chatgpt-codex-proxy/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	app, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to build server", "error", err)
		os.Exit(1)
	}
	defer app.Close()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		logger.Error("failed to listen", "addr", cfg.ListenAddr, "error", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("server listening", "addr", listener.Addr().String(), "data_dir", cfg.DataDir)
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("server exited", "error", err)
			os.Exit(1)
		}
	}()
	if cfg.OpenBrowser {
		url, err := adminUIURL(cfg.ListenAddr)
		if err != nil {
			logger.Warn("resolve admin UI URL failed", "error", err)
		} else if err := openBrowser(url); err != nil {
			logger.Warn("open admin UI failed", "url", url, "error", err)
		} else {
			logger.Info("admin UI opened", "url", url)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, stopDesktopParent := desktopParentShutdownContext(ctx, os.Stdin, os.Getenv("CODEX_DESKTOP_PARENT") == "1")
	defer stopDesktopParent()
	<-ctx.Done()

	logger.Info("shutdown requested")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

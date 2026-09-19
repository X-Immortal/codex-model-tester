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
	applyPlatformDefaults()
	retainEmbeddedLegalNotices()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		showFatalError("Codex Model Tester 启动失败", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	app, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("failed to build server", "error", err)
		showFatalError("Codex Model Tester 启动失败", err)
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
		showFatalError("Codex Model Tester 无法启动本地服务", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("server listening", "addr", listener.Addr().String(), "data_dir", cfg.DataDir)
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("server exited", "error", err)
			showFatalError("Codex Model Tester 后端已停止", err)
			os.Exit(1)
		}
	}()
	url, err := adminUIURL(cfg.ListenAddr)
	if err != nil {
		logger.Error("resolve admin UI URL failed", "error", err)
		showFatalError("Codex Model Tester 无法解析网页地址", err)
		os.Exit(1)
	}
	if cfg.OpenBrowser {
		if err := openBrowser(url); err != nil {
			logger.Warn("open admin UI failed", "url", url, "error", err)
		} else {
			logger.Info("admin UI opened", "url", url)
		}
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	ctx, requestShutdown := context.WithCancel(signalCtx)
	defer requestShutdown()
	if err := waitForApplicationExit(ctx, requestShutdown, url, logger); err != nil {
		logger.Error("application lifecycle failed", "error", err)
		showFatalError("Codex Model Tester 托盘运行失败", err)
		requestShutdown()
	}

	logger.Info("shutdown requested")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

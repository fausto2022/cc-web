package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cc-web/backend/internal/httpapi"
	"cc-web/backend/internal/store"
)

func bindAllowed(addr, allowContainerBind, dbPath string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(allowContainerBind), "true") && strings.TrimSpace(dbPath) != ""
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	addr := strings.TrimSpace(os.Getenv("CC_WEB_LISTEN"))
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		logger.Error("invalid listen address", "error", err)
		os.Exit(2)
	}
	ip := net.ParseIP(host)
	if ip == nil || (!ip.IsLoopback() && !bindAllowed(addr, os.Getenv("CC_WEB_ALLOW_CONTAINER_BIND"), os.Getenv("CC_WEB_DB_PATH"))) {
		logger.Error("refusing non-loopback bind before authentication and HTTPS proxy are configured", "addr", addr)
		os.Exit(2)
	}
	dbPath := strings.TrimSpace(os.Getenv("CC_WEB_DB_PATH"))
	if dbPath == "" {
		dbPath = "/data/cc-web.db"
	}
	st, err := store.Open(dbPath)
	if err != nil {
		logger.Error("database initialization failed", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer st.Close()
	server := &http.Server{Addr: addr, Handler: httpapi.New(st, logger), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info("cc-web backend starting", "addr", addr, "runtime", "disconnected", "storage", "sqlite", "database", dbPath)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

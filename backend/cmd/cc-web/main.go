package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cc-web/backend/internal/auth"
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

func bootstrapAdmin(ctx context.Context, st *store.Store, username, passwordFile, dbPath string) (string, error) {
	hasUser, err := st.HasAuthUser(ctx)
	if err != nil {
		return "", err
	}
	if hasUser {
		return "", nil
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if passwordFile != "" {
		password, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("read bootstrap password file: %w", err)
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r")
		if err := auth.New(st).Bootstrap(ctx, username, value); err != nil {
			return "", fmt.Errorf("initialize administrator: %w", err)
		}
		return passwordFile, nil
	}
	secretPath := dbPath + ".bootstrap-password"
	password := ""
	if saved, err := os.ReadFile(secretPath); err == nil {
		password = strings.TrimSuffix(strings.TrimSuffix(string(saved), "\n"), "\r")
	} else if errors.Is(err, os.ErrNotExist) {
		password, err = generateBootstrapPassword()
		if err != nil {
			return "", err
		}
		file, err := os.OpenFile(secretPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", fmt.Errorf("create bootstrap password file: %w", err)
		}
		if _, err := file.WriteString(password + "\n"); err != nil {
			_ = file.Close()
			_ = os.Remove(secretPath)
			return "", fmt.Errorf("write bootstrap password file: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(secretPath)
			return "", err
		}
	} else {
		return "", err
	}
	if err := auth.New(st).CreateInitialAdmin(ctx, username, password); err != nil {
		return "", fmt.Errorf("initialize administrator: %w", err)
	}
	return secretPath, nil
}

func generateBootstrapPassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
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
	bootstrapPath, err := bootstrapAdmin(context.Background(), st, os.Getenv("CC_WEB_ADMIN_USERNAME"), os.Getenv("CC_WEB_ADMIN_PASSWORD_FILE"), dbPath)
	if err != nil {
		logger.Error("administrator bootstrap failed", "error", err)
		os.Exit(1)
	}
	if bootstrapPath != "" {
		logger.Warn("one-time administrator password generated", "username", firstNonEmpty(os.Getenv("CC_WEB_ADMIN_USERNAME"), "admin"), "password_file", bootstrapPath, "action", "change password after first login and delete the file")
	}
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

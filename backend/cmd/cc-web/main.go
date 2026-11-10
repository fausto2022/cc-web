package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"golang.org/x/term"
)

func bindAllowed(addr, allowContainerBind, dbPath string, administratorInitialized bool) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if !administratorInitialized {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(allowContainerBind), "true") && strings.TrimSpace(dbPath) != ""
}

func databasePath() string {
	path := strings.TrimSpace(os.Getenv("CC_WEB_DB_PATH"))
	if path == "" {
		return "/data/cc-web.db"
	}
	return path
}

func runAdminInit(ctx context.Context, args []string, prompt func() (string, error)) error {
	flags := flag.NewFlagSet("admin init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	username := flags.String("username", "admin", "administrator username")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("usage: cc-web admin init [--username name]")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: cc-web admin init [--username name]")
	}
	st, err := store.Open(databasePath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()
	hasUser, err := st.HasAuthUser(ctx)
	if err != nil {
		return fmt.Errorf("check administrator state: %w", err)
	}
	if hasUser {
		return errors.New("administrator already initialized")
	}
	password, err := prompt()
	if err != nil {
		return err
	}
	if err := auth.New(st).CreateInitialAdmin(ctx, *username, password); err != nil {
		return fmt.Errorf("initialize administrator: %w", err)
	}
	return nil
}

func promptPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("admin init requires an interactive terminal for hidden password input")
	}
	fmt.Fprint(os.Stderr, "New administrator password (12-128 bytes): ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}
	if subtle.ConstantTimeCompare(password, confirmation) != 1 {
		return "", errors.New("password confirmation does not match")
	}
	return string(password), nil
}

func run(ctx context.Context, args []string, logger *slog.Logger, listen func(network, address string) (net.Listener, error)) error {
	if len(args) >= 2 && args[0] == "admin" && args[1] == "init" {
		return runAdminInit(ctx, args[2:], promptPassword)
	}
	if len(args) != 0 {
		return errors.New("usage: cc-web [admin init --username name]")
	}

	addr := strings.TrimSpace(os.Getenv("CC_WEB_LISTEN"))
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	dbPath := databasePath()
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}
	defer st.Close()
	hasUser, err := st.HasAuthUser(ctx)
	if err != nil {
		return fmt.Errorf("administrator state check failed: %w", err)
	}
	if !hasUser {
		return errors.New("administrator is not initialized; run cc-web admin init --username admin from an interactive terminal")
	}
	if !bindAllowed(addr, os.Getenv("CC_WEB_ALLOW_CONTAINER_BIND"), dbPath, hasUser) {
		return errors.New("refusing non-loopback bind without explicit container bind configuration")
	}
	listener, err := listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Addr: addr, Handler: httpapi.New(st, logger), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info("cc-web backend starting", "addr", addr, "runtime", "disconnected", "storage", "sqlite", "database", dbPath)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server stopped: %w", err)
	}
	return nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], logger, net.Listen); err != nil {
		logger.Error("cc-web startup failed", "error", err)
		os.Exit(1)
	}
}

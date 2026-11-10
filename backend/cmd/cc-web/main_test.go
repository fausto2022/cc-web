package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"cc-web/backend/internal/store"
)

func TestRunAdminInitCreatesAdministratorOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-web.db")
	t.Setenv("CC_WEB_DB_PATH", dbPath)
	promptCount := 0
	prompt := func() (string, error) {
		promptCount++
		return "one-time-bootstrap-password", nil
	}
	if err := runAdminInit(context.Background(), []string{"--username", "admin"}, prompt); err != nil {
		t.Fatal(err)
	}
	if promptCount != 1 {
		t.Fatalf("password prompt callback called %d times, want 1", promptCount)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.AuthUserByUsername(context.Background(), "admin")
	if err != nil || !user.MustChangePassword {
		t.Fatalf("admin not initialized correctly: %#v %v", user, err)
	}
	if err := runAdminInit(context.Background(), []string{"--username", "other"}, func() (string, error) { return "unused", nil }); err == nil {
		t.Fatal("second administrator initialization unexpectedly succeeded")
	}
}

func TestRunAdminInitRejectsMismatchedPassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-web.db")
	t.Setenv("CC_WEB_DB_PATH", dbPath)
	err := runAdminInit(context.Background(), []string{"--username", "admin"}, func() (string, error) {
		return "", errors.New("password confirmation does not match")
	})
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("expected password confirmation failure, got %v", err)
	}
	st, openErr := store.Open(dbPath)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer st.Close()
	if has, err := st.HasAuthUser(context.Background()); err != nil || has {
		t.Fatalf("mismatched password created admin: has=%v err=%v", has, err)
	}
}

func TestRunRefusesUninitializedDatabaseBeforeListen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-web.db")
	t.Setenv("CC_WEB_DB_PATH", dbPath)
	t.Setenv("CC_WEB_LISTEN", "127.0.0.1:0")
	called := false
	err := run(context.Background(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)), func(network, address string) (net.Listener, error) {
		called = true
		return nil, errors.New("listener should not be called")
	})
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected uninitialized refusal, got %v", err)
	}
	if called {
		t.Fatal("uninitialized database opened a listener")
	}
}

func TestBindPolicyRequiresInitializedAdministrator(t *testing.T) {
	for _, tc := range []struct {
		addr, allow, db string
		initialized   bool
		want          bool
	}{
		{"127.0.0.1:8080", "", "", false, false},
		{"127.0.0.1:8080", "", "", true, true},
		{"[::1]:8080", "", "", true, true},
		{"0.0.0.0:8080", "", "/data/cc-web.db", true, false},
		{"0.0.0.0:8080", "true", "", true, false},
		{"0.0.0.0:8080", "true", "/data/cc-web.db", true, true},
	} {
		if got := bindAllowed(tc.addr, tc.allow, tc.db, tc.initialized); got != tc.want {
			t.Errorf("bindAllowed(%q,%q,%q,%v)=%v, want %v", tc.addr, tc.allow, tc.db, tc.initialized, got, tc.want)
		}
	}
}

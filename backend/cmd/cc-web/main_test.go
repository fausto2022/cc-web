package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cc-web/backend/internal/store"
)

func TestBootstrapGeneratesOneTimePasswordFileAndDoesNotRegenerate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data", "cc-web.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	secretPath, err := bootstrapAdmin(context.Background(), st, "", "", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < 13 {
		t.Fatalf("bootstrap password too short: %d bytes", len(secret))
	}
	secondSecret, err := bootstrapAdmin(context.Background(), st, "", "", dbPath)
	if err != nil || secondSecret != "" {
		t.Fatalf("initialized admin should not bootstrap twice: path=%q err=%v", secondSecret, err)
	}
	secretAgain, err := os.ReadFile(secretPath)
	if err != nil || string(secretAgain) != string(secret) {
		t.Fatalf("bootstrap password changed on restart: %v", err)
	}
	if err := os.Remove(secretPath); err != nil {
		t.Fatal(err)
	}
	if again, err := bootstrapAdmin(context.Background(), st, "", "", dbPath); err != nil || again != "" {
		t.Fatalf("initialized admin re-bootstrap: path=%q err=%v", again, err)
	}
}

func TestBootstrapRejectsSecondInitialization(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cc-web.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateAuthUser(context.Background(), "admin", "hash"); err != nil {
		t.Fatal(err)
	}
	if path, err := bootstrapAdmin(context.Background(), st, "", "", dbPath); err != nil || path != "" {
		t.Fatalf("bootstrap should not run twice: path=%q err=%v", path, err)
	}
}

func TestBindPolicy(t *testing.T) {
	for _, tc := range []struct {
		addr, allow, db string
		want            bool
	}{
		{"127.0.0.1:8080", "", "", true},
		{"[::1]:8080", "", "", true},
		{"0.0.0.0:8080", "", "", false},
		{"0.0.0.0:8080", "true", "", false},
		{"0.0.0.0:8080", "true", "/data/cc-web.db", true},
	} {
		if got := bindAllowed(tc.addr, tc.allow, tc.db); got != tc.want {
			t.Errorf("bindAllowed(%q,%q,%q)=%v, want %v", tc.addr, tc.allow, tc.db, got, tc.want)
		}
	}
}

package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cc-web/backend/internal/store"
)

func TestPasswordHashAndBootstrapOnlyOnce(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st)
	if err := svc.Bootstrap(context.Background(), "admin", "one-time-bootstrap-password"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Bootstrap(context.Background(), "admin", "another-bootstrap-password"); err == nil {
		t.Fatal("second bootstrap succeeded")
	}
	user, err := st.AuthUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash == "one-time-bootstrap-password" || !VerifyPassword(user.PasswordHash, "one-time-bootstrap-password") {
		t.Fatal("password was not stored as a verifiable hash")
	}
	if !user.MustChangePassword {
		t.Fatal("initial account must require password change")
	}
}

func TestSessionExpiryAndPasswordChangeRevokesSessions(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	svc := New(st)
	if err := svc.Bootstrap(ctx, "admin", "one-time-bootstrap-password"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Login(ctx, "admin", "one-time-bootstrap-password")
	if err != nil || !first.MustChangePassword {
		t.Fatalf("initial login: %#v %v", first, err)
	}
	if _, err := svc.Authenticate(ctx, first.Token); err != nil {
		t.Fatalf("session not usable: %v", err)
	}
	if err := svc.ChangePassword(ctx, first.UserID, "one-time-bootstrap-password", "a-new-strong-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, first.Token); err == nil {
		t.Fatal("password change did not revoke existing session")
	}
	if _, err := svc.Login(ctx, "admin", "one-time-bootstrap-password"); err == nil {
		t.Fatal("old password remained valid")
	}
	second, err := svc.Login(ctx, "admin", "a-new-strong-password")
	if err != nil || second.MustChangePassword {
		t.Fatalf("new password login: %#v %v", second, err)
	}
	if err := svc.Logout(ctx, second.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, second.Token); err == nil {
		t.Fatal("logout did not revoke session")
	}
	if err := st.CreateAuthSession(ctx, second.UserID, tokenHash("expired-token"), "csrf", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, "expired-token"); err == nil {
		t.Fatal("expired session remained valid")
	}
}

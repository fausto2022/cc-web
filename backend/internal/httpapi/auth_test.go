package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-web/backend/internal/auth"
	"cc-web/backend/internal/store"
)

func authTestHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.New(st).Bootstrap(context.Background(), "admin", "one-time-bootstrap-password"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_WEB_ENV", "development")
	t.Setenv("CC_WEB_COOKIE_SECURE", "false")
	t.Setenv("CC_WEB_PUBLIC_ORIGIN", "")
	t.Setenv("CC_WEB_TRUSTED_PROXIES", "")
	return New(st, slog.New(slog.NewTextHandler(io.Discard, nil))), st
}

func serveJSON(h http.Handler, method, path, body, cookie, csrf, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "ccweb_session", Value: cookie})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAuthenticationLifecycleAndSecurityGates(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	if got := serveJSON(h, http.MethodGet, "/api/projects", "", "", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous business API: %d %s", got.Code, got.Body.String())
	}
	if got := serveJSON(h, http.MethodGet, "/api/status", "", "", "", ""); got.Code != http.StatusOK {
		t.Fatalf("public status: %d", got.Code)
	}
	login := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"one-time-bootstrap-password"}`, "", "", "http://127.0.0.1:5177")
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure {
		t.Fatalf("unexpected development cookie attributes: %#v", cookie)
	}
	var authResponse struct {
		CSRFToken          string `json:"csrfToken"`
		MustChangePassword bool   `json:"mustChangePassword"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &authResponse); err != nil {
		t.Fatal(err)
	}
	if !authResponse.MustChangePassword || authResponse.CSRFToken == "" {
		t.Fatalf("bootstrap login flags missing: %#v", authResponse)
	}
	if got := serveJSON(h, http.MethodGet, "/api/projects", "", cookie.Value, "", ""); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "password_change_required") {
		t.Fatalf("initial password gate: %d %s", got.Code, got.Body.String())
	}
	if got := serveJSON(h, http.MethodPost, "/api/auth/password", `{"currentPassword":"one-time-bootstrap-password","newPassword":"a-new-strong-password"}`, cookie.Value, authResponse.CSRFToken, "https://evil.example"); got.Code != http.StatusForbidden {
		t.Fatalf("evil origin accepted: %d", got.Code)
	}
	if got := serveJSON(h, http.MethodPost, "/api/auth/password", `{"currentPassword":"one-time-bootstrap-password","newPassword":"a-new-strong-password"}`, cookie.Value, "bad", "http://127.0.0.1:5177"); got.Code != http.StatusForbidden {
		t.Fatalf("invalid csrf accepted: %d", got.Code)
	}
	changed := serveJSON(h, http.MethodPost, "/api/auth/password", `{"currentPassword":"one-time-bootstrap-password","newPassword":"a-new-strong-password"}`, cookie.Value, authResponse.CSRFToken, "http://127.0.0.1:5177")
	if changed.Code != http.StatusNoContent {
		t.Fatalf("password change: %d %s", changed.Code, changed.Body.String())
	}
	if got := serveJSON(h, http.MethodGet, "/api/projects", "", cookie.Value, "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("old session remains usable after password change: %d", got.Code)
	}
	if got := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"one-time-bootstrap-password"}`, "", "", "http://127.0.0.1:5177"); got.Code != http.StatusUnauthorized {
		t.Fatalf("old password remains valid: %d", got.Code)
	}
	newLogin := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"a-new-strong-password"}`, "", "", "http://127.0.0.1:5177")
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login: %d %s", newLogin.Code, newLogin.Body.String())
	}
	newCookie := newLogin.Result().Cookies()[0]
	var newAuth struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(newLogin.Body.Bytes(), &newAuth); err != nil {
		t.Fatal(err)
	}
	logout := serveJSON(h, http.MethodPost, "/api/auth/logout", "{}", newCookie.Value, newAuth.CSRFToken, "http://127.0.0.1:5177")
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}
	if got := serveJSON(h, http.MethodGet, "/api/auth/me", "", newCookie.Value, "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("session usable after logout: %d", got.Code)
	}
}

func TestProductionCookieSecureAndUnknownOriginDenied(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	t.Setenv("CC_WEB_ENV", "production")
	t.Setenv("CC_WEB_COOKIE_SECURE", "false")
	t.Setenv("CC_WEB_PUBLIC_ORIGIN", "https://192.0.2.10:9443")
	// The handler snapshots origin/cookie configuration when constructed.
	h = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	badOrigin := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"one-time-bootstrap-password"}`, "", "", "https://evil.example")
	if badOrigin.Code != http.StatusForbidden {
		t.Fatalf("unknown origin accepted: %d", badOrigin.Code)
	}
	goodOrigin := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"one-time-bootstrap-password"}`, "", "", "https://192.0.2.10:9443")
	if goodOrigin.Code != http.StatusOK {
		t.Fatalf("configured origin login: %d %s", goodOrigin.Code, goodOrigin.Body.String())
	}
	cookie := goodOrigin.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Name != "__Host-ccweb_session" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure production cookie: %#v", cookie)
	}
}

func TestSessionExpiryReturns401(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	ctx := context.Background()
	user, err := st.AuthUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAuthSession(ctx, user.ID, tokenHashForTest("expired"), "csrf", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := serveJSON(h, http.MethodGet, "/api/auth/me", "", "expired", "", "")
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("expired session accepted: %d %s", got.Code, got.Body.String())
	}
}

func tokenHashForTest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TestProductionWithoutConfiguredOriginRejectsLogin(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	t.Setenv("CC_WEB_ENV", "production")
	t.Setenv("CC_WEB_PUBLIC_ORIGIN", "")
	h = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"one-time-bootstrap-password"}`, "", "", "http://127.0.0.1:5177")
	if response.Code != http.StatusForbidden {
		t.Fatalf("production accepted development origin without configured public origin: %d", response.Code)
	}
}

func TestDevelopmentFlagCannotDisableSecureCookieOutsideDevelopment(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	t.Setenv("CC_WEB_ENV", "production")
	t.Setenv("CC_WEB_COOKIE_SECURE", "false")
	t.Setenv("CC_WEB_PUBLIC_ORIGIN", "https://secure.example")
	h = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := serveJSON(h, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"one-time-bootstrap-password"}`, "", "", "https://secure.example")
	if response.Code != http.StatusOK {
		t.Fatalf("login: %d %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	if !cookie.Secure {
		t.Fatalf("production flag disabled Secure cookie: %#v", cookie)
	}
}

func TestLoginRateLimitPerClientIP(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()

	serveLogin := func(remoteAddr string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("{"))
		request.RemoteAddr = remoteAddr
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://127.0.0.1:5177")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		return response
	}

	for attempt := 1; attempt <= 5; attempt++ {
		if got := serveLogin("192.0.2.1:1234"); got.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d from first IP: got %d, want %d (%s)", attempt, got.Code, http.StatusBadRequest, got.Body.String())
		}
	}
	if got := serveLogin("192.0.2.1:1234"); got.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt from first IP: got %d, want %d (%s)", got.Code, http.StatusTooManyRequests, got.Body.String())
	}
	if got := serveLogin("192.0.2.2:1234"); got.Code != http.StatusBadRequest {
		t.Fatalf("first attempt from second IP: got %d, want %d (%s)", got.Code, http.StatusBadRequest, got.Body.String())
	}
}

func loginAttempt(h http.Handler, remoteAddr string, realIPs ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("{"))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:5177")
	request.Header.Set("X-Forwarded-For", "198.51.100.250, 203.0.113.250")
	request.Header.Set("Forwarded", "for=198.51.100.250")
	for _, ip := range realIPs {
		request.Header.Add("X-Real-IP", ip)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func TestLoginRateLimitTrustedProxyUsesXRealIP(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	t.Setenv("CC_WEB_TRUSTED_PROXIES", "172.26.0.1/32")
	h = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for attempt := 1; attempt <= 5; attempt++ {
		if got := loginAttempt(h, "172.26.0.1:1234", "198.51.100.10"); got.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d from first client: got %d, want %d (%s)", attempt, got.Code, http.StatusBadRequest, got.Body.String())
		}
	}
	if got := loginAttempt(h, "172.26.0.1:1234", "198.51.100.10"); got.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt from first client: got %d, want %d (%s)", got.Code, http.StatusTooManyRequests, got.Body.String())
	}
	if got := loginAttempt(h, "172.26.0.1:1234", "198.51.100.11"); got.Code != http.StatusBadRequest {
		t.Fatalf("first attempt from second client: got %d, want %d (%s)", got.Code, http.StatusBadRequest, got.Body.String())
	}
}

func TestLoginRateLimitIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	t.Setenv("CC_WEB_TRUSTED_PROXIES", "172.26.0.1/32")
	h = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for attempt := 1; attempt <= 5; attempt++ {
		if got := loginAttempt(h, "192.0.2.10:1234", "198.51.100.10"); got.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d from first peer: got %d, want %d (%s)", attempt, got.Code, http.StatusBadRequest, got.Body.String())
		}
	}
	if got := loginAttempt(h, "192.0.2.10:1234", "198.51.100.10"); got.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt from first peer: got %d, want %d (%s)", got.Code, http.StatusTooManyRequests, got.Body.String())
	}
	if got := loginAttempt(h, "192.0.2.11:1234", "198.51.100.10"); got.Code != http.StatusBadRequest {
		t.Fatalf("spoofed forwarding headers shared another peer bucket: got %d, want %d (%s)", got.Code, http.StatusBadRequest, got.Body.String())
	}
}

func TestTrustedProxyRejectsMissingOrInvalidXRealIP(t *testing.T) {
	tests := []struct {
		name    string
		realIPs []string
	}{
		{name: "missing"},
		{name: "duplicate", realIPs: []string{"198.51.100.10", "198.51.100.11"}},
		{name: "chain", realIPs: []string{"198.51.100.10, 198.51.100.11"}},
		{name: "malformed", realIPs: []string{"not-an-ip"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, st := authTestHandler(t)
			defer st.Close()
			t.Setenv("CC_WEB_TRUSTED_PROXIES", "172.26.0.1/32")
			h = New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
			got := loginAttempt(h, "172.26.0.1:1234", tt.realIPs...)
			if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "client_ip_invalid") {
				t.Fatalf("invalid X-Real-IP accepted: got %d (%s)", got.Code, got.Body.String())
			}
		})
	}
}

func TestLoginRequiresOrigin(t *testing.T) {
	h, st := authTestHandler(t)
	defer st.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"one-time-bootstrap-password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("login without origin accepted: %d", response.Code)
	}
}

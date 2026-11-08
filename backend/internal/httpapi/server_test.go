package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-web/backend/internal/store"
)

func testServer() http.Handler {
	return New(store.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func TestHealthAndRuntimeTruth(t *testing.T) {
	h := testServer()
	for _, tc := range []struct{ path, contains string }{{"/healthz", `"status":"ok"`}, {"/api/status", `"cliRuntime":"not_connected"`}, {"/api/status", `"persistence":"sqlite"`}} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.contains) {
			t.Fatalf("%s: code=%d body=%s", tc.path, w.Code, w.Body.String())
		}
	}
}
func TestCreateTeamPlanNeverClaimsDispatch(t *testing.T) {
	h := testServer()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/session-login-refresh/team-runs", strings.NewReader(`{"taskPrompt":"Review refresh failure handling","members":[{"name":"auth-reviewer","role":"review","task":"Review refresh failure handling"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	for _, needle := range []string{`"status":"awaiting_runtime"`, `"status":"planned"`, `"runtimeConnected":false`, `have not started`} {
		if !strings.Contains(w.Body.String(), needle) {
			t.Fatalf("response missing %q: %s", needle, w.Body.String())
		}
	}
}
func TestRegisterProjectConfinedToConfiguredWorkspace(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "repo")
	outside := t.TempDir()
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_WEB_HOST_ROOT", root)
	h := testServer()
	allowedBody, _ := json.Marshal(map[string]string{"name": "repo", "path": inside})
	allowed := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(allowedBody))
	allowed.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, allowed)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected in-root path, got %d %s", w.Code, w.Body.String())
	}
	deniedBody, _ := json.Marshal(map[string]string{"name": "outside", "path": outside})
	denied := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(deniedBody))
	denied.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, denied)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "project_path_denied") {
		t.Fatalf("expected outside path denied, got %d %s", w.Code, w.Body.String())
	}
}

func TestUnknownProjectRejected(t *testing.T) {
	h := testServer()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(`{"projectId":"outside","title":"x","provider":"Codex","mode":"single"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "project_not_found") {
		t.Fatalf("expected unknown project rejection, got %d %s", w.Code, w.Body.String())
	}
}
func TestApprovalCannotChangeTerminalDecision(t *testing.T) {
	h := testServer()
	create := httptest.NewRequest(http.MethodPost, "/api/sessions/session-login-refresh/approvals", strings.NewReader(`{"source":"Codex","command":"npm test","risk":"Runs project scripts"}`))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	h.ServeHTTP(created, create)
	var response struct {
		Approval struct {
			ID string `json:"id"`
		} `json:"approval"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	resolve := func(decision string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/approvals/"+response.Approval.ID+"/"+decision, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := resolve("approve"); w.Code != http.StatusOK {
		t.Fatalf("first decision: %d %s", w.Code, w.Body.String())
	}
	if w := resolve("reject"); w.Code != http.StatusConflict {
		t.Fatalf("conflicting decision: %d %s", w.Code, w.Body.String())
	}
}
func TestTeamMessagesAreStoredSeparatelyFromConversation(t *testing.T) {
	h := testServer()
	create := httptest.NewRequest(http.MethodPost, "/api/sessions/session-login-refresh/team-runs", strings.NewReader(`{"taskPrompt":"Review refresh logic","members":[{"name":"review","role":"review","task":"Review refresh logic"}]}`))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	h.ServeHTTP(created, create)
	var plan struct {
		TeamRun struct {
			ID string `json:"id"`
		} `json:"teamRun"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/team-runs/"+plan.TeamRun.ID+"/messages", strings.NewReader(`{"content":"Please check refresh errors"}`))
	post.Header.Set("Content-Type", "application/json")
	posted := httptest.NewRecorder()
	post.Header.Set("Origin", "http://127.0.0.1:5177")
	h.ServeHTTP(posted, post)
	if posted.Code != http.StatusAccepted {
		t.Fatalf("team message code=%d body=%s", posted.Code, posted.Body.String())
	}
	conversation := httptest.NewRecorder()
	h.ServeHTTP(conversation, httptest.NewRequest(http.MethodGet, "/api/sessions/session-login-refresh/messages", nil))
	if !strings.Contains(conversation.Body.String(), "Review refresh logic") {
		t.Fatalf("team launch task not recorded in main conversation: %s", conversation.Body.String())
	}
	if strings.Contains(conversation.Body.String(), "Please check refresh errors") {
		t.Fatalf("team reply leaked into the main conversation: %s", conversation.Body.String())
	}
	team := httptest.NewRecorder()
	h.ServeHTTP(team, httptest.NewRequest(http.MethodGet, "/api/team-runs/"+plan.TeamRun.ID+"/messages", nil))
	if !strings.Contains(team.Body.String(), "Please check refresh errors") {
		t.Fatalf("team reply missing from team transcript: %s", team.Body.String())
	}
}

func TestTeamModeCannotBeEnabledAfterConversationStarted(t *testing.T) {
	h := testServer()
	send := httptest.NewRequest(http.MethodPost, "/api/sessions/session-login-refresh/messages", strings.NewReader(`{"content":"first message"}`))
	send.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, send)
	team := httptest.NewRequest(http.MethodPost, "/api/sessions/session-login-refresh/team-runs", strings.NewReader(`{"taskPrompt":"Start a team","members":[{"name":"review","role":"review","task":"Review"}]}`))
	team.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, team)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "team_mode_locked") {
		t.Fatalf("expected team mode lock, got %d %s", response.Code, response.Body.String())
	}
}

func TestCreateMessageRequiresExistingSession(t *testing.T) {
	h := testServer()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/missing/messages", strings.NewReader(`{"content":"hello","teamMode":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
func TestLocalDevelopmentOriginAllowed(t *testing.T) {
	h := testServer()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5177")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5177" {
		t.Fatalf("expected allowlisted dev origin, got %d %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestUnknownOriginRejected(t *testing.T) {
	h := testServer()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cc-web/backend/internal/auth"
	"cc-web/backend/internal/model"
	"cc-web/backend/internal/store"
)

type Server struct {
	store          *store.Store
	log            *slog.Logger
	auth           *auth.Service
	publicOrigin   string
	secureCookie   bool
	loginLimiter   *loginLimiter
	trustedProxies []netip.Prefix
}

type contextKey string

const sessionContextKey contextKey = "cc-web.auth-session"

func New(st *store.Store, logger *slog.Logger) http.Handler {
	return NewWithAuth(st, auth.New(st), logger)
}

func NewWithAuth(st *store.Store, authService *auth.Service, logger *slog.Logger) http.Handler {
	publicOrigin := strings.TrimSpace(os.Getenv("CC_WEB_PUBLIC_ORIGIN"))
	if publicOrigin == "" && os.Getenv("CC_WEB_ENV") != "development" {
		logger.Warn("CC_WEB_PUBLIC_ORIGIN is unset; rejecting browser origins outside explicit development mode")
	}
	s := &Server{store: st, log: logger, auth: authService, publicOrigin: publicOrigin, secureCookie: !(os.Getenv("CC_WEB_ENV") == "development" && os.Getenv("CC_WEB_COOKIE_SECURE") == "false"), loginLimiter: newLoginLimiter(), trustedProxies: parseTrustedProxies(os.Getenv("CC_WEB_TRUSTED_PROXIES"))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/password", s.changePassword)
	mux.HandleFunc("GET /api/projects", s.projects)
	mux.HandleFunc("POST /api/projects", s.registerProject)
	mux.HandleFunc("GET /api/projects/{projectID}", s.project)
	mux.HandleFunc("GET /api/sessions", s.sessions)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("GET /api/sessions/{sessionID}", s.session)
	mux.HandleFunc("PATCH /api/sessions/{sessionID}", s.renameSession)
	mux.HandleFunc("GET /api/sessions/{sessionID}/messages", s.messages)
	mux.HandleFunc("POST /api/sessions/{sessionID}/messages", s.createMessage)
	mux.HandleFunc("GET /api/sessions/{sessionID}/team-runs", s.teamRuns)
	mux.HandleFunc("POST /api/sessions/{sessionID}/team-runs", s.createTeamRun)
	mux.HandleFunc("GET /api/team-runs/{teamRunID}/messages", s.teamMessages)
	mux.HandleFunc("POST /api/team-runs/{teamRunID}/messages", s.createTeamMessage)
	mux.HandleFunc("POST /api/sessions/{sessionID}/approvals", s.createApproval)
	mux.HandleFunc("POST /api/approvals/{approvalID}/{decision}", s.resolveApproval)
	return securityHeaders(cors(s.authMiddleware(mux), s.publicOrigin))
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/status" {
			if origin := r.Header.Get("Origin"); origin != "" && !s.validOrigin(r) {
				writeError(w, http.StatusForbidden, "origin_denied", "request origin is not allowed")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/auth/login" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(s.sessionCookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		session, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if errors.Is(err, auth.ErrSessionInvalid) {
			s.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		if err != nil {
			s.log.Error("session lookup failed", "error", err)
			writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "authentication temporarily unavailable")
			return
		}
		if isMutation(r.Method) {
			if !s.validOrigin(r) {
				writeError(w, http.StatusForbidden, "origin_denied", "request origin is not allowed")
				return
			}
			csrf := r.Header.Get("X-CSRF-Token")
			if csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRFToken)) != 1 {
				writeError(w, http.StatusForbidden, "csrf_invalid", "valid CSRF token required")
				return
			}
		}
		if session.MustChangePassword && r.URL.Path != "/api/auth/me" && r.URL.Path != "/api/auth/password" && r.URL.Path != "/api/auth/logout" {
			writeError(w, http.StatusForbidden, "password_change_required", "change the initial password before continuing")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), sessionContextKey, session))
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sessionCookieName() string {
	if s.secureCookie {
		return "__Host-ccweb_session"
	}
	return "ccweb_session"
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	if s.publicOrigin != "" {
		return origin == s.publicOrigin
	}
	return s.publicOrigin == "" && os.Getenv("CC_WEB_ENV") == "development" && isAllowedDevelopmentOrigin(origin)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Value: token, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteStrictMode})
}

func isJSONContentType(value string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	return mediaType == "application/json"
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.validOrigin(r) {
		writeError(w, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return
	}
	client, ok := clientIP(r, s.trustedProxies)
	if !ok {
		writeError(w, http.StatusBadRequest, "client_ip_invalid", "trusted proxy must provide one valid X-Real-IP address")
		return
	}
	if !s.loginLimiter.allow(client) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts")
		return
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "content_type_required", "application/json required")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request")
		return
	}
	session, err := s.auth.Login(r.Context(), in.Username, in.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "username or password is incorrect")
		return
	}
	if errors.Is(err, auth.ErrLoginInput) {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "username or password is invalid")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "constraint failed") || strings.Contains(err.Error(), "unique constraint") {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "username or password is incorrect")
			return
		}
		s.log.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "login_failed", "login unavailable")
		return
	}
	s.setSessionCookie(w, session.Token, session.ExpiresAt)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"username": session.Username}, "csrfToken": session.CSRFToken, "mustChangePassword": session.MustChangePassword})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session, _ := r.Context().Value(sessionContextKey).(store.AuthSession)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"username": session.Username}, "csrfToken": session.CSRFToken, "mustChangePassword": session.MustChangePassword})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(s.sessionCookieName())
	if cookie != nil {
		if err := s.auth.Logout(r.Context(), cookie.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "logout_failed", "could not end session")
			return
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request")
		return
	}
	session, _ := r.Context().Value(sessionContextKey).(store.AuthSession)
	err := s.auth.ChangePassword(r.Context(), session.UserID, in.CurrentPassword, in.NewPassword)
	switch {
	case errors.Is(err, auth.ErrPasswordPolicy):
		writeError(w, http.StatusBadRequest, "password_policy", "password must contain 12 to 128 bytes")
		return
	case errors.Is(err, auth.ErrCurrentPassword):
		writeError(w, http.StatusUnauthorized, "current_password_invalid", "current password is incorrect")
		return
	case err != nil:
		s.log.Error("password change failed", "error", err)
		writeError(w, http.StatusInternalServerError, "password_change_failed", "could not change password")
		return
	}
	s.clearSessionCookie(w)
	if passwordFile := strings.TrimSpace(os.Getenv("CC_WEB_ADMIN_PASSWORD_FILE")); passwordFile != "" {
		_ = os.Remove(passwordFile)
	} else if dbPath := strings.TrimSpace(os.Getenv("CC_WEB_DB_PATH")); dbPath != "" {
		_ = os.Remove(dbPath + ".bootstrap-password")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"service": "cc-web", "api": "ready", "cliRuntime": "not_connected", "teamRuntime": "not_connected", "persistence": "sqlite"})
}
func (s *Server) projects(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"projects": s.store.Projects()})
}
func (s *Server) registerProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in.Path = strings.TrimSpace(in.Path)
	if in.Name == "" || in.Path == "" {
		writeError(w, http.StatusBadRequest, "invalid_project", "name and path are required")
		return
	}
	root := strings.TrimSpace(os.Getenv("CC_WEB_HOST_ROOT"))
	if root == "" {
		root = "/workspace"
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "workspace_unavailable", "configured workspace root is unavailable")
		return
	}
	canonicalPath, err := filepath.EvalSymlinks(in.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_project_path", "project path must exist and be accessible")
		return
	}
	rel, err := filepath.Rel(canonicalRoot, canonicalPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, http.StatusForbidden, "project_path_denied", "project path must remain inside the configured workspace root")
		return
	}
	info, err := os.Stat(canonicalPath)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "invalid_project_path", "project path must be a directory")
		return
	}
	id := filepath.Base(canonicalPath)
	project, err := s.store.RegisterProject(model.Project{ID: id, Name: in.Name, Path: canonicalPath})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "project_store_failed", "could not register project")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": project})
}
func (s *Server) project(w http.ResponseWriter, r *http.Request) {
	v, ok := s.store.Project(r.PathValue("projectID"))
	if !ok {
		writeError(w, http.StatusNotFound, "project_not_found", "project not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": v})
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.store.Sessions(r.URL.Query().Get("projectId"))})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
		Provider  string `json:"provider"`
		Mode      string `json:"mode"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if in.ProjectID == "" || strings.TrimSpace(in.Title) == "" {
		writeError(w, http.StatusBadRequest, "invalid_session", "projectId and title are required")
		return
	}
	if !s.store.ProjectExists(in.ProjectID) {
		writeError(w, http.StatusNotFound, "project_not_found", "project is not registered in the workspace")
		return
	}
	if in.Provider != "Codex" && in.Provider != "Claude" {
		writeError(w, http.StatusBadRequest, "unsupported_provider", "provider must be Codex or Claude")
		return
	}
	if in.Mode == "" {
		in.Mode = "single"
	}
	if in.Mode != "single" && in.Mode != "team" {
		writeError(w, http.StatusBadRequest, "unsupported_mode", "mode must be single or team")
		return
	}
	session := s.store.CreateSession(in.ProjectID, in.Title, in.Provider, in.Mode)
	writeJSON(w, http.StatusCreated, map[string]any{"session": session, "runtimeConnected": false, "message": "Session created. CLI runtime is not connected; no command was executed."})
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	v, ok := s.store.Session(r.PathValue("sessionID"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": v, "runtimeConnected": false})
}
func (s *Server) renameSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 160 {
		writeError(w, http.StatusBadRequest, "invalid_title", "title must be 1 to 160 characters")
		return
	}
	session, err := s.store.RenameSession(r.PathValue("sessionID"), in.Title)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session, "runtimeConnected": false})
}
func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, ok := s.store.Session(id); !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": s.store.Messages(id)})
}
func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, ok := s.store.Session(id); !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	var in struct {
		Content  string `json:"content"`
		TeamMode bool   `json:"teamMode"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" {
		writeError(w, http.StatusBadRequest, "empty_message", "content is required")
		return
	}
	m := s.store.AddMessage(id, "user", in.Content)
	writeJSON(w, http.StatusAccepted, map[string]any{"message": m, "runtimeConnected": false, "teamRequested": in.TeamMode, "dispatchStatus": "not_dispatched", "notice": "Message stored in volatile memory only. No CLI or agents were started."})
}
func (s *Server) teamRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, ok := s.store.Session(id); !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teamRuns": s.store.Teams(id), "runtimeConnected": false})
}
func (s *Server) createTeamRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, ok := s.store.Session(id); !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	var in struct {
		TaskPrompt string `json:"taskPrompt"`
		Members    []struct {
			Name string `json:"name"`
			Role string `json:"role"`
			Task string `json:"task"`
		} `json:"members"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in.TaskPrompt = strings.TrimSpace(in.TaskPrompt)
	if in.TaskPrompt == "" {
		writeError(w, http.StatusBadRequest, "missing_task_prompt", "taskPrompt is required")
		return
	}
	if len(in.Members) == 0 || len(in.Members) > 12 {
		writeError(w, http.StatusBadRequest, "invalid_team", "members must contain between 1 and 12 planned members")
		return
	}
	members := make([]model.TeamMember, 0, len(in.Members))
	for i, m := range in.Members {
		if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Role) == "" {
			writeError(w, http.StatusBadRequest, "invalid_team_member", "each member needs a name and role")
			return
		}
		members = append(members, model.TeamMember{ID: "planned-" + itoa(i+1), Name: m.Name, Role: m.Role, Status: "planned", CurrentTask: m.Task})
	}
	run, err := s.store.CreateTeamRun(id, in.TaskPrompt, members)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "team_mode_locked", "Agent Teams must be selected before the first user message")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"teamRun": run, "runtimeConnected": false, "dispatchStatus": "not_dispatched", "notice": "Plan recorded only. Agent runtime is not connected; these members have not started."})
}
func (s *Server) teamMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("teamRunID")
	if _, ok := s.store.Team(id); !ok {
		writeError(w, http.StatusNotFound, "team_run_not_found", "team run not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": s.store.TeamMessages(id), "runtimeConnected": false})
}
func (s *Server) createTeamMessage(w http.ResponseWriter, r *http.Request) {
	run, ok := s.store.Team(r.PathValue("teamRunID"))
	if !ok {
		writeError(w, http.StatusNotFound, "team_run_not_found", "team run not found")
		return
	}
	var in struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" {
		writeError(w, http.StatusBadRequest, "empty_message", "content is required")
		return
	}
	message, err := s.store.AddTeamMessage(run.ID, in.Content)
	if err != nil {
		writeError(w, http.StatusNotFound, "team_run_not_found", "team run not found")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"message": message, "teamRun": run, "runtimeConnected": false, "dispatchStatus": "not_dispatched", "notice": "Message recorded for the team request. Agent runtime is not connected; no members were started."})
}

func (s *Server) createApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if _, ok := s.store.Session(id); !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	var in struct {
		Source  string `json:"source"`
		Command string `json:"command"`
		Risk    string `json:"risk"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if in.Source == "" || in.Command == "" || in.Risk == "" {
		writeError(w, http.StatusBadRequest, "invalid_approval", "source, command and risk are required")
		return
	}
	a := s.store.CreateApproval(id, in.Source, in.Command, in.Risk)
	writeJSON(w, http.StatusCreated, map[string]any{"approval": a, "runtimeConnected": false, "notice": "Approval recorded; no command has been executed."})
}
func (s *Server) resolveApproval(w http.ResponseWriter, r *http.Request) {
	decision := r.PathValue("decision")
	if decision != "approve" && decision != "reject" {
		writeError(w, http.StatusBadRequest, "invalid_decision", "decision must be approve or reject")
		return
	}
	status := "rejected"
	if decision == "approve" {
		status = "approved"
	}
	a, err := s.store.ResolveApproval(r.PathValue("approvalID"), status)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "approval not found")
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "approval_already_resolved", "message": "approval already has a terminal decision"}, "approval": a, "execution": "not_executed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approval": a, "execution": "not_executed", "runtimeConnected": false, "notice": "Decision recorded. No CLI command was executed."})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON value")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
func isAllowedDevelopmentOrigin(origin string) bool {
	return origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" || origin == "http://localhost:5176" || origin == "http://127.0.0.1:5176" || origin == "http://localhost:5177" || origin == "http://127.0.0.1:5177"
}

func cors(next http.Handler, publicOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			developmentOrigin := publicOrigin == "" && os.Getenv("CC_WEB_ENV") == "development" && isAllowedDevelopmentOrigin(origin)
			if !developmentOrigin && origin != publicOrigin {
				writeError(w, http.StatusForbidden, "origin_denied", "origin is not allowed")
				return
			}
			if developmentOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 4)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

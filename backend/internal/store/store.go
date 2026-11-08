package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cc-web/backend/internal/model"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("state conflict")
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := path
	if path != ":memory:" {
		dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seed(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}
func New() *Store {
	s, err := Open(":memory:")
	if err != nil {
		panic(err)
	}
	return s
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate(ctx context.Context) error {
	const schema = `PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY,name TEXT NOT NULL,path TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,project_id TEXT NOT NULL REFERENCES projects(id),title TEXT NOT NULL,provider TEXT NOT NULL,mode TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS sessions_project_created ON sessions(project_id,created_at);
CREATE TABLE IF NOT EXISTS messages(id TEXT PRIMARY KEY,session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,role TEXT NOT NULL,content TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS messages_session_created ON messages(session_id,created_at);
CREATE TABLE IF NOT EXISTS team_runs(id TEXT PRIMARY KEY,session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,task_title TEXT NOT NULL,task_prompt TEXT NOT NULL,status TEXT NOT NULL,demo INTEGER NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS team_runs_session_created ON team_runs(session_id,created_at);
CREATE TABLE IF NOT EXISTS team_members(id TEXT PRIMARY KEY,team_run_id TEXT NOT NULL REFERENCES team_runs(id) ON DELETE CASCADE,name TEXT NOT NULL,role TEXT NOT NULL,status TEXT NOT NULL,current_task TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS team_messages(id TEXT PRIMARY KEY,team_run_id TEXT NOT NULL REFERENCES team_runs(id) ON DELETE CASCADE,content TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS team_messages_run_created ON team_messages(team_run_id,created_at);
CREATE TABLE IF NOT EXISTS approvals(id TEXT PRIMARY KEY,session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,source TEXT NOT NULL,command TEXT NOT NULL,risk TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS approvals_session_created ON approvals(session_id,created_at);
CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,?)", stamp(time.Now()))
	if err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return nil
}
func (s *Store) seed(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		now := time.Now().UTC()
		p := model.Project{ID: "cc-web", Name: "cc-web", Path: "~/work/cc-web"}
		session := model.Session{ID: "session-login-refresh", ProjectID: p.ID, Title: "优化登录态刷新与过期提示", Provider: "Codex", Mode: "single", Status: "idle", CreatedAt: now}
		if _, err = tx.ExecContext(ctx, "INSERT INTO projects(id,name,path) VALUES(?,?,?)", p.ID, p.Name, p.Path); err != nil {
			return err
		}
		if err = insertSession(ctx, tx, session); err != nil {
			return err
		}
		welcome := model.Message{ID: "message-welcome", SessionID: session.ID, Role: "assistant", Content: "当前工作台已启动。服务端 API 可用；CLI 运行时尚未连接。", CreatedAt: now}
		if err = insertMessage(ctx, tx, welcome); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func newID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000")
}
func insertSession(ctx context.Context, tx *sql.Tx, v model.Session) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO sessions(id,project_id,title,provider,mode,status,created_at) VALUES(?,?,?,?,?,?,?)", v.ID, v.ProjectID, v.Title, v.Provider, v.Mode, v.Status, stamp(v.CreatedAt))
	return err
}
func insertMessage(ctx context.Context, tx *sql.Tx, v model.Message) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO messages(id,session_id,role,content,created_at) VALUES(?,?,?,?,?)", v.ID, v.SessionID, v.Role, v.Content, stamp(v.CreatedAt))
	return err
}
func scanSession(row interface{ Scan(...any) error }) (model.Session, error) {
	var v model.Session
	var created string
	err := row.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Provider, &v.Mode, &v.Status, &created)
	if err == nil {
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	}
	return v, err
}
func (s *Store) Projects() []model.Project {
	rows, err := s.db.Query("SELECT id,name,path FROM projects ORDER BY name,id")
	if err != nil {
		return []model.Project{}
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var v model.Project
		if rows.Scan(&v.ID, &v.Name, &v.Path) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) RegisterProject(project model.Project) (model.Project, error) {
	if _, err := s.db.Exec("INSERT INTO projects(id,name,path) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,path=excluded.path", project.ID, project.Name, project.Path); err != nil {
		return model.Project{}, err
	}
	return project, nil
}
func (s *Store) Project(id string) (model.Project, bool) {
	var v model.Project
	err := s.db.QueryRow("SELECT id,name,path FROM projects WHERE id=?", id).Scan(&v.ID, &v.Name, &v.Path)
	return v, err == nil
}
func (s *Store) ProjectExists(id string) bool {
	var exists int
	return s.db.QueryRow("SELECT 1 FROM projects WHERE id=?", id).Scan(&exists) == nil
}
func (s *Store) Sessions(projectID string) []model.Session {
	query := "SELECT id,project_id,title,provider,mode,status,created_at FROM sessions"
	args := []any{}
	if projectID != "" {
		query += " WHERE project_id=?"
		args = append(args, projectID)
	}
	query += " ORDER BY created_at,id"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return []model.Session{}
	}
	defer rows.Close()
	out := []model.Session{}
	for rows.Next() {
		v, e := scanSession(rows)
		if e == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) Session(id string) (model.Session, bool) {
	v, err := scanSession(s.db.QueryRow("SELECT id,project_id,title,provider,mode,status,created_at FROM sessions WHERE id=?", id))
	return v, err == nil
}
func (s *Store) CreateSession(projectID, title, provider, mode string) model.Session {
	v := model.Session{ID: newID("session"), ProjectID: projectID, Title: title, Provider: provider, Mode: mode, Status: "idle", CreatedAt: time.Now().UTC()}
	_, _ = s.db.Exec("INSERT INTO sessions(id,project_id,title,provider,mode,status,created_at) VALUES(?,?,?,?,?,?,?)", v.ID, v.ProjectID, v.Title, v.Provider, v.Mode, v.Status, stamp(v.CreatedAt))
	return v
}
func (s *Store) Messages(sessionID string) []model.Message {
	rows, err := s.db.Query("SELECT id,session_id,role,content,created_at FROM messages WHERE session_id=? ORDER BY rowid", sessionID)
	if err != nil {
		return []model.Message{}
	}
	defer rows.Close()
	out := []model.Message{}
	for rows.Next() {
		var v model.Message
		var created string
		if rows.Scan(&v.ID, &v.SessionID, &v.Role, &v.Content, &created) == nil {
			v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) RenameSession(sessionID, title string) (model.Session, error) {
	res, err := s.db.Exec("UPDATE sessions SET title=? WHERE id=?", title, sessionID)
	if err != nil {
		return model.Session{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Session{}, ErrNotFound
	}
	v, ok := s.Session(sessionID)
	if !ok {
		return model.Session{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) AddMessage(sessionID, role, content string) model.Message {
	v := model.Message{ID: newID("message"), SessionID: sessionID, Role: role, Content: content, CreatedAt: time.Now().UTC()}
	_, _ = s.db.Exec("INSERT INTO messages(id,session_id,role,content,created_at) VALUES(?,?,?,?,?)", v.ID, v.SessionID, v.Role, v.Content, stamp(v.CreatedAt))
	return v
}
func (s *Store) CreateTeamRun(sessionID, taskPrompt string, members []model.TeamMember) (model.TeamRun, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.TeamRun{}, err
	}
	defer tx.Rollback()
	_, err = scanSession(tx.QueryRowContext(ctx, "SELECT id,project_id,title,provider,mode,status,created_at FROM sessions WHERE id=?", sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.TeamRun{}, ErrNotFound
	}
	if err != nil {
		return model.TeamRun{}, err
	}
	var userMessages int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE session_id=? AND role='user'", sessionID).Scan(&userMessages); err != nil {
		return model.TeamRun{}, err
	}
	if userMessages > 0 {
		return model.TeamRun{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, "UPDATE sessions SET mode='team',title=? WHERE id=?", taskPrompt, sessionID); err != nil {
		return model.TeamRun{}, err
	}
	now := time.Now().UTC()
	run := model.TeamRun{ID: newID("team"), SessionID: sessionID, TaskTitle: taskPrompt, TaskPrompt: taskPrompt, Status: "awaiting_runtime", Members: append([]model.TeamMember{}, members...), Demo: true, CreatedAt: now}
	if _, err = tx.ExecContext(ctx, "INSERT INTO team_runs(id,session_id,task_title,task_prompt,status,demo,created_at) VALUES(?,?,?,?,?,?,?)", run.ID, run.SessionID, run.TaskTitle, run.TaskPrompt, run.Status, 1, stamp(now)); err != nil {
		return model.TeamRun{}, err
	}
	for i := range run.Members {
		run.Members[i].SessionID = sessionID
		if _, err = tx.ExecContext(ctx, "INSERT INTO team_members(id,team_run_id,name,role,status,current_task) VALUES(?,?,?,?,?,?)", run.Members[i].ID, run.ID, run.Members[i].Name, run.Members[i].Role, run.Members[i].Status, run.Members[i].CurrentTask); err != nil {
			return model.TeamRun{}, err
		}
	}
	message := model.Message{ID: newID("message"), SessionID: sessionID, Role: "user", Content: taskPrompt, CreatedAt: now}
	if err = insertMessage(ctx, tx, message); err != nil {
		return model.TeamRun{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.TeamRun{}, err
	}
	return run, nil
}
func (s *Store) UpdateTeamRun(runID, status string) (model.TeamRun, error) {
	res, err := s.db.Exec("UPDATE team_runs SET status=? WHERE id=?", status, runID)
	if err != nil {
		return model.TeamRun{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.TeamRun{}, ErrNotFound
	}
	v, ok := s.Team(runID)
	if !ok {
		return model.TeamRun{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) AddTeamMessage(teamID, content string) (model.TeamMessage, error) {
	if _, ok := s.Team(teamID); !ok {
		return model.TeamMessage{}, ErrNotFound
	}
	v := model.TeamMessage{ID: newID("team-message"), TeamRunID: teamID, Content: content, CreatedAt: time.Now().UTC()}
	_, err := s.db.Exec("INSERT INTO team_messages(id,team_run_id,content,created_at) VALUES(?,?,?,?)", v.ID, v.TeamRunID, v.Content, stamp(v.CreatedAt))
	return v, err
}
func (s *Store) TeamMessages(teamID string) []model.TeamMessage {
	rows, err := s.db.Query("SELECT id,team_run_id,content,created_at FROM team_messages WHERE team_run_id=? ORDER BY created_at,id", teamID)
	if err != nil {
		return []model.TeamMessage{}
	}
	defer rows.Close()
	out := []model.TeamMessage{}
	for rows.Next() {
		var v model.TeamMessage
		var created string
		if rows.Scan(&v.ID, &v.TeamRunID, &v.Content, &created) == nil {
			v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) Team(id string) (model.TeamRun, bool) {
	var v model.TeamRun
	var created string
	var demo int
	err := s.db.QueryRow("SELECT id,session_id,task_title,task_prompt,status,demo,created_at FROM team_runs WHERE id=?", id).Scan(&v.ID, &v.SessionID, &v.TaskTitle, &v.TaskPrompt, &v.Status, &demo, &created)
	if err != nil {
		return v, false
	}
	v.Demo = demo != 0
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	v.Members = s.members(id)
	return v, true
}
func (s *Store) members(runID string) []model.TeamMember {
	rows, err := s.db.Query("SELECT id,name,role,status,current_task FROM team_members WHERE team_run_id=? ORDER BY rowid", runID)
	if err != nil {
		return []model.TeamMember{}
	}
	defer rows.Close()
	out := []model.TeamMember{}
	for rows.Next() {
		var v model.TeamMember
		if rows.Scan(&v.ID, &v.Name, &v.Role, &v.Status, &v.CurrentTask) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *Store) Teams(sessionID string) []model.TeamRun {
	rows, err := s.db.Query("SELECT id FROM team_runs WHERE session_id=? ORDER BY created_at,id", sessionID)
	if err != nil {
		return []model.TeamRun{}
	}
	defer rows.Close()
	out := []model.TeamRun{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			if v, ok := s.Team(id); ok {
				out = append(out, v)
			}
		}
	}
	return out
}
func (s *Store) CreateApproval(sessionID, source, command, risk string) model.Approval {
	v := model.Approval{ID: newID("approval"), SessionID: sessionID, Source: source, Command: command, Risk: risk, Status: "pending", CreatedAt: time.Now().UTC()}
	_, _ = s.db.Exec("INSERT INTO approvals(id,session_id,source,command,risk,status,created_at) VALUES(?,?,?,?,?,?,?)", v.ID, v.SessionID, v.Source, v.Command, v.Risk, v.Status, stamp(v.CreatedAt))
	return v
}
func (s *Store) ResolveApproval(id, status string) (model.Approval, error) {
	res, err := s.db.Exec("UPDATE approvals SET status=? WHERE id=? AND status='pending'", status, id)
	if err != nil {
		return model.Approval{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		a, ok := s.Approval(id)
		if !ok {
			return model.Approval{}, ErrNotFound
		}
		return a, ErrConflict
	}
	a, _ := s.Approval(id)
	return a, nil
}
func (s *Store) Approval(id string) (model.Approval, bool) {
	var v model.Approval
	var created string
	err := s.db.QueryRow("SELECT id,session_id,source,command,risk,status,created_at FROM approvals WHERE id=?", id).Scan(&v.ID, &v.SessionID, &v.Source, &v.Command, &v.Risk, &v.Status, &created)
	if err == nil {
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	}
	return v, err == nil
}

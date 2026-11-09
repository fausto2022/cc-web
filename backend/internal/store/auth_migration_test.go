package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAuthMigrationPreservesExistingRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM schema_migrations WHERE version=2;
DROP INDEX IF EXISTS auth_sessions_user_expiry;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS users;
INSERT OR IGNORE INTO projects(id,name,path) VALUES('kept','kept','/workspace/kept');
INSERT OR IGNORE INTO sessions(id,project_id,title,provider,mode,status,created_at) VALUES('kept-session','kept','kept','Codex','single','idle','2026-01-01T00:00:00Z');
INSERT OR IGNORE INTO messages(id,session_id,role,content,created_at) VALUES('kept-message','kept-session','user','kept','2026-01-01T00:00:00Z');
DELETE FROM schema_migrations WHERE version>1;`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if !migrated.ProjectExists("kept") {
		t.Fatal("project was lost during auth migration")
	}
	if _, ok := migrated.Session("kept-session"); !ok {
		t.Fatal("session was lost during auth migration")
	}
	messages := migrated.Messages("kept-session")
	if len(messages) != 1 || messages[0].Content != "kept" {
		t.Fatalf("message was lost during auth migration: %#v", messages)
	}
	var version int
	if err := migrated.db.QueryRowContext(context.Background(), "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("migration version=%d want 2", version)
	}
}

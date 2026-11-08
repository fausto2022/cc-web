package store

import (
	"path/filepath"
	"testing"

	"cc-web/backend/internal/model"
)

func TestSQLiteStateSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cc-web.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	session := st.CreateSession("cc-web", "persistent", "Codex", "single")
	message := st.AddMessage(session.ID, "user", "persist this message")
	approval := st.CreateApproval(session.ID, "Codex", "echo safe", "low")
	if _, err = st.ResolveApproval(approval.ID, "approved"); err != nil {
		t.Fatal(err)
	}
	team, err := st.CreateTeamRun("session-login-refresh", "persistent team task", []model.TeamMember{{ID: "planned-1", Name: "review", Role: "review", Status: "planned", CurrentTask: "review"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.AddTeamMessage(team.ID, "team follow-up"); err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, ok := reopened.Session(session.ID); !ok || got.Title != "persistent" {
		t.Fatalf("session not restored: %#v, %v", got, ok)
	}
	if got := reopened.Messages(session.ID); len(got) != 1 || got[0].ID != message.ID {
		t.Fatalf("messages not restored: %#v", got)
	}
	if got, ok := reopened.Approval(approval.ID); !ok || got.Status != "approved" {
		t.Fatalf("approval not restored: %#v, %v", got, ok)
	}
	if got, ok := reopened.Team(team.ID); !ok || got.Status != "awaiting_runtime" || len(got.Members) != 1 {
		t.Fatalf("team run not restored: %#v, %v", got, ok)
	}
	if got := reopened.TeamMessages(team.ID); len(got) != 1 || got[0].Content != "team follow-up" {
		t.Fatalf("team messages not restored: %#v", got)
	}
	if got := reopened.Sessions("cc-web"); len(got) != 2 {
		t.Fatalf("unexpected session count after reopen: %d", len(got))
	}
}

func TestCreateTeamRunIsAtomicAndFirstMessageOnly(t *testing.T) {
	st := New()
	defer st.Close()
	_, err := st.CreateTeamRun("session-login-refresh", "team task", []model.TeamMember{{ID: "m1", Name: "review", Role: "review", Status: "planned"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := st.Session("session-login-refresh"); !ok || got.Mode != "team" || got.Title != "team task" {
		t.Fatalf("session mode not updated: %#v", got)
	}
	if got := st.Messages("session-login-refresh"); len(got) != 2 || got[1].Content != "team task" {
		t.Fatalf("task message not written atomically: %#v", got)
	}
	if _, err := st.CreateTeamRun("session-login-refresh", "second team", []model.TeamMember{{ID: "m2", Name: "review", Role: "review"}}); err != ErrConflict {
		t.Fatalf("expected team-mode conflict, got %v", err)
	}
}

package tests

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSetSessionStatusRejectsTerminalToActive(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("status-transition-bucket"))
	defer app.close()
	ctx := t.Context()

	defaultIDs := getDefaultDBIDs(t, app.pool)
	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"status-transition-agent"}`)
	env := createEnvironment(t, app, `{"name":"status-transition-env"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)

	if err := app.db.SetSessionStatus(ctx, defaultIDs.WorkspaceUUID, session.ID, "running"); err != nil {
		t.Fatalf("idle → running: %v", err)
	}
	if err := app.db.SetSessionStatus(ctx, defaultIDs.WorkspaceUUID, session.ID, "terminated"); err != nil {
		t.Fatalf("running → terminated: %v", err)
	}
	err := app.db.SetSessionStatus(ctx, defaultIDs.WorkspaceUUID, session.ID, "idle")
	if !errors.Is(err, db.ErrInvalidStateTransition) {
		t.Fatalf("terminated → idle: err = %v, want ErrInvalidStateTransition", err)
	}
	if err := app.db.SetSessionStatus(ctx, defaultIDs.WorkspaceUUID, "sesn_missing", "idle"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("missing session: err = %v, want ErrNotFound", err)
	}
}

package deployments

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPrepareDeploymentExecutionRuntimeIdentity(t *testing.T) {
	deployment := db.Deployment{
		RuntimeUserUUID: "scheduler-user", Metadata: json.RawMessage(`{"label":"public"}`),
		InitialEvents: json.RawMessage(`[]`), Resources: json.RawMessage(`[]`),
		ResourceSecrets: json.RawMessage(`{}`), VaultIDs: json.RawMessage(`[]`),
	}
	for _, test := range []struct{ name, user string }{
		{"scheduled user", deployment.RuntimeUserUUID},
		{"manual invocation uses current user", "other-workspace-member"},
	} {
		t.Run(test.name, func(t *testing.T) {
			run, err := prepareDeploymentExecution(deployment, "", test.user, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			session := run.Session.Session
			if session.RuntimeUserUUID != test.user || session.CreatedByAPIKeyUUID != "" {
				t.Fatalf("execution identity = user %q key %q", session.RuntimeUserUUID, session.CreatedByAPIKeyUUID)
			}
			if string(session.Metadata) != string(deployment.Metadata) {
				t.Fatal("public metadata changed")
			}
		})
	}
}

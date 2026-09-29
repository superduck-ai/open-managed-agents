package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

// Record the deadline at the producer boundary, before Enqueuer can create a
// per-event timeout. Every event from one cascade must share this deadline.
type publicationDeadlineRecorder struct {
	deadlines []time.Time
	stall     bool
}

func (r *publicationDeadlineRecorder) Enqueue(ctx context.Context, _ webhooks.EnqueueInput) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	if r.stall && !deadline.IsZero() {
		<-ctx.Done()
	}
}

func (r *publicationDeadlineRecorder) check(t *testing.T, want int) {
	t.Helper()
	if len(r.deadlines) != want {
		t.Fatalf("notifications = %d, want %d", len(r.deadlines), want)
	}
	first := r.deadlines[0]
	if first.IsZero() || time.Until(first) > 5*time.Second {
		t.Fatalf("missing bounded cascade deadline: %v", first)
	}
	for _, deadline := range r.deadlines {
		if !deadline.Equal(first) {
			t.Fatalf("cascade renewed deadline: %v, first %v", deadline, first)
		}
	}
}

func TestWebhookVaultCascadePublicationDeadline(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("publication-deadline"))
	defer app.close()
	ids := getDefaultDBIDs(t, app.pool)
	for _, operation := range []string{"stall archive", "archive", "delete"} {
		t.Run(operation, func(t *testing.T) {
			vault := createVault(t, app, `{"display_name":"cascade deadline"}`)
			defer cleanupVaultRows(t, app, vault.ID)
			for _, name := range []string{"FIRST", "SECOND"} {
				createVaultCredential(t, app, vault.ID, environmentVariableBody(name, name, "value"))
			}
			recorder := &publicationDeadlineRecorder{stall: operation == "stall archive"}
			handler := vaults.NewHandler(app.cfg, app.db, app.vaultSecrets, recorder, nil)
			path, method := "/"+vault.ID, http.MethodDelete
			if operation != "delete" {
				path += "/archive"
				method = http.MethodPost
			}
			ctx := auth.WithPrincipal(t.Context(), auth.Principal{CredentialType: auth.CredentialTypeAPIKey, WorkspaceUUID: ids.WorkspaceUUID, OrganizationUUID: ids.OrganizationUUID})
			req := httptest.NewRequestWithContext(ctx, method, path+"?beta=true", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			want := 3
			if recorder.stall {
				want = 1
			}
			recorder.check(t, want)
			if ctx.Err() != nil {
				t.Fatal("notification timeout canceled business context")
			}
		})
	}
}

func TestWebhookAgentCascadePublicationDeadline(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("publication-deadline"))
	defer app.close()
	first := newWebhookDeployment(t, app)
	current, err := app.db.GetDeployment(t.Context(), getDefaultDBIDs(t, app.pool).WorkspaceUUID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := createDeployment(t, app, deploymentBodyWithInitialEvents(current.AgentExternalID, first.EnvironmentID, `[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]`))
	defer cleanupDeploymentRows(t, app, second.ID)
	recorder := &publicationDeadlineRecorder{}
	app.deployments.WithWebhooks(recorder)
	_, changed, err := app.deployments.ArchiveAgent(t.Context(), current.WorkspaceUUID, current.AgentExternalID)
	if err != nil || !changed {
		t.Fatalf("archive changed=%t err=%v", changed, err)
	}
	recorder.check(t, 2)
}

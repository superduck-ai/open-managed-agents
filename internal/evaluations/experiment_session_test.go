package evaluations

import (
	"context"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionEndTurnCompletesExperiment(t *testing.T) {
	store := readyExperimentStore()
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"})
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if created.Code != http.StatusOK || store.experiment.Status != db.EvaluationExperimentStatusRunning {
		t.Fatalf("create = %d status=%s body=%s", created.Code, store.experiment.Status, created.Body.String())
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"requires_action"}}`),
	}})
	if store.experiment.Status != db.EvaluationExperimentStatusRunning || store.experiment.Items[0].Status != db.EvaluationExperimentStatusRunning {
		t.Fatalf("requires_action status=%s item=%s", store.experiment.Status, store.experiment.Items[0].Status)
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_other",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if store.experiment.Items[0].Status != db.EvaluationExperimentStatusRunning {
		t.Fatalf("unrelated session changed item to %s", store.experiment.Items[0].Status)
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].ErrorMessage != "" {
		t.Fatalf("completed status=%s item=%#v", store.experiment.Status, store.experiment.Items[0])
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_terminated",
	}})
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("second terminal status=%s item=%s", store.experiment.Status, store.experiment.Items[0].Status)
	}
}

func TestSessionFailureFailsExperiment(t *testing.T) {
	store := readyExperimentStore()
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"})
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	store.experiment.Items = append(store.experiment.Items, db.EvaluationExperimentItem{
		ExternalID: "evalitem_open", SessionExternalID: "sesn_open", Status: db.EvaluationExperimentStatusRunning,
	})
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"retries_exhausted"}}`),
	}})
	if store.experiment.Status != db.EvaluationExperimentStatusRunning || store.experiment.Items[0].Status != db.EvaluationExperimentStatusFailed || store.experiment.Items[0].ErrorMessage != sessionRetriesExhaustedMessage {
		t.Fatalf("partial status=%s items=%#v", store.experiment.Status, store.experiment.Items)
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_open",
		EventType: "session.status_terminated",
	}})
	if store.experiment.Status != db.EvaluationExperimentStatusFailed || store.experiment.Items[1].ErrorMessage != sessionTerminatedMessage {
		t.Fatalf("failed status=%s items=%#v", store.experiment.Status, store.experiment.Items)
	}
}

func TestSessionOutcomeKeepsFailureOverLaterSuccess(t *testing.T) {
	outcomes := sessionOutcomes([]db.SessionEvent{
		{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "sesn_case", EventType: "session.status_terminated"},
		{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "sesn_case", EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`)},
		{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "sesn_case", EventType: "session.status_running"},
	})
	if len(outcomes) != 1 || outcomes[0].Status != db.EvaluationExperimentStatusFailed || outcomes[0].ErrorMessage != sessionTerminatedMessage {
		t.Fatalf("outcomes = %#v", outcomes)
	}
}

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

func TestSandboxResumeTelemetryOutcomes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	for _, outcome := range []string{"error", "disabled", "no_provider_id", "recovery_requested", "recovery_scheduled", "extended"} {
		t.Run(outcome, func(t *testing.T) {
			app := newTestAppWithStore(t, nil, newFakeStore("resume-telemetry-"+outcome))
			defer app.close()
			agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"resume-telemetry"}`)
			defer cleanupAgentRows(t, app.pool, agent.ID)
			environment := createEnvironment(t, app, `{"name":"resume-telemetry"}`)
			defer cleanupEnvironmentRows(t, app.pool, environment.ID)
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
			defer cleanupSession(t, app, session.ID)
			codeSessionID := launchLocalCodeSession(t, app, session.ID)
			epoch := registerCodeSessionWorker(t, app, codeSessionID)
			putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
			codeSession, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			switch outcome {
			case "error":
				app.sandboxTimeouts.setError(errors.New("private provider failure"))
			case "recovery_scheduled":
				app.sandboxTimeouts.setError(sandboxruntime.ErrSandboxNotFound)
			case "no_provider_id":
				empty := ""
				if err := app.db.UpdateEnvironmentSandboxState(t.Context(), sandbox.WorkspaceUUID, sandbox.ExternalID, "running", &empty, nil, nil); err != nil {
					t.Fatal(err)
				}
			case "recovery_requested":
				now := time.Now()
				if err := app.db.UpdateEnvironmentSandboxState(t.Context(), sandbox.WorkspaceUUID, sandbox.ExternalID, "stopped", nil, nil, &now); err != nil {
					t.Fatal(err)
				}
			}
			service := newCodeSessionService(app, nil, nil)
			if outcome != "disabled" {
				service.WithSandboxTimeoutExtender(app.sandboxTimeouts, app.cfg.E2B.SandboxTimeout)
			}
			record, found, err := app.db.GetSession(t.Context(), codeSession.WorkspaceUUID, session.ID)
			if err != nil || !found {
				t.Fatalf("session: %t, %v", found, err)
			}
			err = service.QueuePublicSessionEvents(t.Context(), record, []db.SessionEvent{{ExternalID: "evt_resume", EventType: "user.interrupt", CreatedAt: time.Now(), Payload: json.RawMessage(`{"id":"evt_resume","type":"user.interrupt"}`)}})
			if (err != nil) != (outcome == "error") {
				t.Fatalf("resume error: %v", err)
			}
			var observed sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended() {
				if span.Name() != "oma.sandbox.resume" {
					continue
				}
				for _, attr := range span.Attributes() {
					if attr.Key == "oma.code_session.id" && attr.Value.AsString() == codeSessionID {
						observed = span
					}
				}
			}
			if observed == nil {
				t.Fatal("resume span missing")
			}
			attrs := map[string]string{}
			for _, attr := range observed.Attributes() {
				attrs[string(attr.Key)] = attr.Value.Emit()
			}
			if attrs["oma.outcome"] != outcome || attrs["oma.session.id"] != session.ID || len(observed.Events()) != 0 {
				t.Fatalf("span attributes/events: %v, %v", attrs, observed.Events())
			}
			if outcome == "error" && (observed.Status().Code != codes.Error || observed.Status().Description != "error" || attrs["error.type"] != "error") {
				t.Fatalf("error status: %+v", observed.Status())
			}
		})
	}
}

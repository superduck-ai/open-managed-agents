package dsh

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type executionStub struct {
	calls int
	err   error
}

func (e *executionStub) Stream(w http.ResponseWriter, _ *http.Request, _ db.DSHScope) {
	e.calls++
	w.WriteHeader(http.StatusOK)
}
func (e *executionStub) Delivery(context.Context, db.DSHScope, []DeliveryUpdate) (DeliveryResponse, error) {
	e.calls++
	return DeliveryResponse{}, e.err
}
func (e *executionStub) Finish(_ context.Context, _ db.DSHScope, _ string, value FinishRequest) (FinishRequest, error) {
	e.calls++
	return value, e.err
}
func (e *executionStub) Permission(context.Context, db.DSHScope, PermissionRequest) (PermissionResponse, error) {
	e.calls++
	return PermissionResponse{}, e.err
}
func (e *executionStub) CancelPermission(context.Context, db.DSHScope, string, PermissionCancel) (PermissionResponse, error) {
	e.calls++
	return PermissionResponse{}, e.err
}
func (e *executionStub) Publish(context.Context, db.DSHScope, []json.RawMessage) (PublishResponse, error) {
	e.calls++
	return PublishResponse{}, e.err
}

func executionRouter(execution Execution) http.Handler {
	router := chi.NewRouter()
	NewExecutionHandler(execution, func(*http.Request, string) (db.DSHScope, error) {
		return db.DSHScope{CodeSessionID: "cse_root", Epoch: 7}, nil
	}, nil).RegisterRoutes(router)
	return router
}

func TestDSHExecutionRejectsMalformedCommandsBeforeSideEffects(t *testing.T) {
	tests := []struct{ path, body string }{
		{"/inputs/delivery", `{"updates":[]}`},
		{"/inputs/delivery", `{"updates":[{"id":"a","status":"received"},{"id":"a","status":"processed"}]}`},
		{"/inputs/delivery", `{"updates":[{"id":"a","status":"processing","error":"invalid_model"}]}`},
		{"/inputs/input/finish", `{"result":"success","failure_code":"execution_failed"}`},
		{"/inputs/input/finish", `{"result":"failed"}`},
		{"/inputs/input/finish", `{"result":"recovered","failure_code":"interrupted_recovery"}`},
		{"/inputs/input/finish", `{"result":"failed","reason":"turn_failed"}`},
		{"/inputs/input/finish", `{"result":"failed","failure_code":"unknown"}`},
		{"/permission-requests", `{"input_id":"input","turn":"01","dsh_session_id":"cse_root","tool_use_id":"call","tool":{"kind":"builtin","name":"bash"},"input":{}}`},
		{"/permission-requests", `{"input_id":"input","turn":"1","dsh_session_id":"cse_root","tool_use_id":"call","tool":{"kind":"mcp","name":"tool"},"input":{}}`},
		{"/permission-requests/wrong/cancel", `{"dsh_session_id":"cse_root","tool_use_id":"call"}`},
		{"/public-events", `{"events":[]}`},
		{"/public-events", `{"events":[],"worker_epoch":7}`},
	}
	for _, test := range tests {
		t.Run(test.path+test.body, func(t *testing.T) {
			stub := &executionStub{}
			response := transcriptRequest(executionRouter(stub), "POST", "/sessions/cse_root"+test.path, test.body, "1")
			if response.Code != 400 || stub.calls != 0 {
				t.Fatalf("response=%d %s calls=%d", response.Code, response.Body.String(), stub.calls)
			}
		})
	}
}

func TestDSHExecutionReturnsDistinctRecoveryErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{db.ErrDSHFinishRequired, "finish_required"}, {db.ErrDSHInputNotEligible, "input_not_eligible"}, {db.ErrDSHFinishConflict, "finish_idempotency_mismatch"},
		{db.ErrDSHPermissionConflict, "permission_idempotency_mismatch"}, {db.ErrDSHPublicConflict, "public_event_idempotency_mismatch"},
	} {
		stub := &executionStub{err: test.err}
		response := transcriptRequest(executionRouter(stub), "POST", "/sessions/cse_root/inputs/input/finish", `{"result":"success"}`, "1")
		if response.Code != 409 || response.Header().Get("DSR-Error") != test.code {
			t.Fatalf("response=%d %s", response.Code, response.Body.String())
		}
	}
}

func TestDSHPermissionIdentityUsesAnUnambiguousTuple(t *testing.T) {
	first := PermissionID("a", "b:c", "d")
	if len(first) != 57 || first != PermissionID("a", "b:c", "d") || first == PermissionID("a:b", "c", "d") {
		t.Fatalf("identity=%s", first)
	}
}

func TestDSHExecutionAcceptsBusinessOutcomes(t *testing.T) {
	for _, body := range []string{`{"result":"success"}`, `{"result":"cancelled"}`, `{"result":"failed","failure_code":"execution_failed"}`, `{"result":"failed","failure_code":"interrupted_recovery"}`} {
		stub := &executionStub{}
		response := transcriptRequest(executionRouter(stub), "POST", "/sessions/cse_root/inputs/input/finish", body, "1")
		if response.Code != 200 || stub.calls != 1 || response.Body.String() != body+"\n" {
			t.Fatalf("finish=%d %s calls=%d", response.Code, response.Body.String(), stub.calls)
		}
	}
}

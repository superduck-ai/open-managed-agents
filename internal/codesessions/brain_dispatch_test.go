package codesessions

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestWorkerRegistrationRejectsInvalidIdentity(t *testing.T) {
	for _, body := range []string{`{"session_id":"other"}`, `{"session_id":12}`, `{`} {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := validateCodeSessionWorkerRegisterBody(httptest.NewRecorder(), request, "cse_test"); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	for _, body := range []string{"", `{}`, `{"session_id":"cse_test"}`} {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := validateCodeSessionWorkerRegisterBody(httptest.NewRecorder(), request, "cse_test"); err != nil {
			t.Fatal("valid registration rejected")
		}
	}
}

func TestBrainTaskCarriesExistingCredentials(t *testing.T) {
	task := workerevents.BrainTask{SessionID: "cse_task", WorkerEpoch: 7, IngressToken: "ingress", ModelToken: "model"}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"session_id", "worker_epoch", "ingress_token", "model_token"} {
		if len(payload[field]) == 0 {
			t.Fatalf("missing task field %s", field)
		}
	}
	for _, field := range []string{"registration_token", "launch", "mcp_config"} {
		if len(payload[field]) != 0 {
			t.Fatalf("execution data in task: %s", field)
		}
	}
}

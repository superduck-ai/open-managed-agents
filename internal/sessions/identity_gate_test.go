package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestIdentityGateAdmitsOnlyUserMessages(t *testing.T) {
	if eventBatchStartsRound([]json.RawMessage{[]byte(`{"type":"agent.tool_result"}`)}) {
		t.Fatal("tool result started a round")
	}
	if !eventBatchStartsRound([]json.RawMessage{[]byte(`{"type":"user.message"}`)}) {
		t.Fatal("user message was ignored")
	}
	id, err := requestedIdentityID([]byte(`"idn_service"`))
	if err != nil || id != "idn_service" {
		t.Fatal(err, id)
	}
	if _, err := requestedIdentityID([]byte(`"file_other"`)); err == nil {
		t.Fatal("non-identity id accepted")
	}
	handler := &Handler{prepareIdentity: func(context.Context, auth.Principal, string) error {
		return errors.New("identity disabled")
	}}
	if _, err := handler.requestedSessionIdentity(context.Background(), auth.Principal{}, []byte(`"idn_service"`)); err == nil {
		t.Fatal("disabled identity reached session creation")
	}
}

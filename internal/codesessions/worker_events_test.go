package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestCodeSessionEventFromWorkerEnvelopePreservesSSEContract(t *testing.T) {
	envelope := workerevents.EventEnvelope(
		"cse_test",
		"csev_test",
		"payload-test",
		"user",
		"message",
		json.RawMessage(`{"text":"hello"}`),
		time.Now().Add(time.Hour),
	)
	envelope.SequenceNum = 9
	event := codeSessionEventFromWorkerEnvelope(envelope)
	if event.ExternalID != "csev_test" || event.CodeSessionExternalID != "cse_test" ||
		event.SequenceNum != 9 || event.EventType != "user" || event.EventSubtype != "message" ||
		event.PayloadUUID == nil || *event.PayloadUUID != "payload-test" || string(event.Payload) != `{"text":"hello"}` {
		t.Fatalf("event = %#v", event)
	}
}

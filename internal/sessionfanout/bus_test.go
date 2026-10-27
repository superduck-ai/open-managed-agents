package sessionfanout

import (
	"encoding/json"
	"testing"
)

func TestValidateEnvelopeKind(t *testing.T) {
	for _, kind := range []Kind{KindSessionEvents, KindCodeSessionStream} {
		envelope := Envelope{Kind: kind, Payload: json.RawMessage(`{}`)}
		if err := validateEnvelope(envelope); err != nil {
			t.Fatalf("validateEnvelope(%q) returned error: %v", kind, err)
		}
	}

	envelope := Envelope{Kind: Kind("unknown"), Payload: json.RawMessage(`{}`)}
	if err := validateEnvelope(envelope); err == nil {
		t.Fatal("validateEnvelope(unknown) error = nil, want unsupported kind error")
	}
}

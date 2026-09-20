package dreams

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestParseRequestedDreamInputsOfficialShape(t *testing.T) {
	parsed, err := parseRequestedDreamInputs([]dreamInputRequest{
		{Type: "sessions", SessionIDs: []string{"sesn_b", "sesn_a"}},
		{Type: "memory_store", MemoryStoreID: "memstore_taste"},
	})
	if err != nil {
		t.Fatalf("parseRequestedDreamInputs() = %v", err)
	}
	if parsed.MemoryStoreID != "memstore_taste" || len(parsed.SessionIDs) != 2 || parsed.SessionIDs[0] != "sesn_b" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestParseRequestedDreamInputsLegacyNested(t *testing.T) {
	parsed, err := parseRequestedDreamInputs([]dreamInputRequest{
		{Type: "memory_store", MemoryStoreID: "memstore_taste", SessionIDs: []string{"sesn_one"}},
	})
	if err != nil {
		t.Fatalf("legacy nested parse = %v", err)
	}
	if parsed.MemoryStoreID != "memstore_taste" || len(parsed.SessionIDs) != 1 || parsed.SessionIDs[0] != "sesn_one" {
		t.Fatalf("legacy nested parsed = %#v", parsed)
	}
}

func TestParseRequestedDreamInputsRejectsUnknownOrDuplicateTypes(t *testing.T) {
	if _, err := parseRequestedDreamInputs(nil); err == nil {
		t.Fatal("empty inputs = nil, want error")
	}
	if _, err := parseRequestedDreamInputs([]dreamInputRequest{{Type: "vault"}}); err == nil {
		t.Fatal("unknown type = nil, want error")
	}
	if _, err := parseRequestedDreamInputs([]dreamInputRequest{
		{Type: "memory_store", MemoryStoreID: "memstore_a"},
		{Type: "memory_store", MemoryStoreID: "memstore_b"},
	}); err == nil {
		t.Fatal("duplicate memory_store = nil, want error")
	}
	if _, err := parseRequestedDreamInputs([]dreamInputRequest{
		{Type: "memory_store", MemoryStoreID: "memstore_a"},
		{Type: "sessions", SessionIDs: []string{"sesn_a"}},
		{Type: "sessions", SessionIDs: []string{"sesn_b"}},
	}); err == nil {
		t.Fatal("duplicate sessions = nil, want error")
	}
}

func TestParseRequestedDreamInputsPrefersSessionsObject(t *testing.T) {
	parsed, err := parseRequestedDreamInputs([]dreamInputRequest{
		{Type: "memory_store", MemoryStoreID: "memstore_taste", SessionIDs: []string{"sesn_nested"}},
		{Type: "sessions", SessionIDs: []string{"sesn_official"}},
	})
	if err != nil {
		t.Fatalf("mixed shape parse = %v", err)
	}
	if len(parsed.SessionIDs) != 1 || parsed.SessionIDs[0] != "sesn_official" {
		t.Fatalf("sessions object should win, got %#v", parsed.SessionIDs)
	}
}

func TestMarshalOfficialDreamInputsOmitsNestedSessionIDs(t *testing.T) {
	encoded, err := marshalOfficialDreamInputs(dreamInputSelection{
		MemoryStoreID: "memstore_taste",
		SessionIDs:    []string{"sesn_one", "sesn_two"},
	})
	if err != nil {
		t.Fatalf("marshalOfficialDreamInputs() = %v", err)
	}
	want := `[{"type":"memory_store","memory_store_id":"memstore_taste"},{"type":"sessions","session_ids":["sesn_one","sesn_two"]}]`
	if string(encoded) != want {
		t.Fatalf("official inputs = %s, want %s", encoded, want)
	}
}

func TestParseStoredDreamInputsAcceptsLegacyAndOfficial(t *testing.T) {
	official, err := parseStoredDreamInputs(json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_in"},{"type":"sessions","session_ids":["sesn_a"]}]`))
	if err != nil || official.MemoryStoreID != "memstore_in" || official.SessionIDs[0] != "sesn_a" {
		t.Fatalf("official stored = %#v, %v", official, err)
	}
	legacy, err := parseStoredDreamInputs(json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_in","session_ids":["sesn_b"]}]`))
	if err != nil || legacy.SessionIDs[0] != "sesn_b" {
		t.Fatalf("legacy stored = %#v, %v", legacy, err)
	}
}

func TestParseDreamModelAcceptsStringAndObject(t *testing.T) {
	got, err := parseDreamModel(json.RawMessage(`" claude-opus-4-8 "`))
	if err != nil || got != "claude-opus-4-8" {
		t.Fatalf("string model = (%q, %v)", got, err)
	}
	got, err = parseDreamModel(json.RawMessage(`{"id":"claude-sonnet-4-6"}`))
	if err != nil || got != "claude-sonnet-4-6" {
		t.Fatalf("object model = (%q, %v)", got, err)
	}
	if _, err := parseDreamModel(nil); err == nil {
		t.Fatal("missing model = nil, want error")
	}
	if _, err := parseDreamModel(json.RawMessage(`{"name":"x"}`)); err == nil {
		t.Fatal("object without id = nil, want error")
	}
}

func TestDreamSessionCountError(t *testing.T) {
	if err := dreamSessionCountError(0); err == nil {
		t.Fatal("0 sessions = nil, want error")
	}
	if err := dreamSessionCountError(1); err != nil {
		t.Fatalf("1 session = %v", err)
	}
	if err := dreamSessionCountError(100); err != nil {
		t.Fatalf("100 sessions = %v", err)
	}
	if err := dreamSessionCountError(101); err == nil {
		t.Fatal("101 sessions = nil, want error")
	}
}

func TestResponseFromDreamMatchesOfficialPublicContract(t *testing.T) {
	endedAt := time.Date(2026, 4, 29, 17, 10, 0, 0, time.UTC)
	created := time.Date(2026, 4, 29, 17, 4, 10, 0, time.UTC)
	running := responseFromDream(db.Dream{
		ExternalID: "drm_01AbCDefGhIjKlMnOpQrStUv",
		Status:     "running",
		Model:      "claude-opus-4-8",
		Inputs:     json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_01Hx","session_ids":["sesn_01","sesn_02"]}]`),
		Outputs:    json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
		CreatedAt:  created,
		UpdatedAt:  endedAt,
	})
	if running.Model.ID != "claude-opus-4-8" {
		t.Fatalf("model = %#v", running.Model)
	}
	if running.SessionID == nil || *running.SessionID != "sesn_internal" {
		t.Fatalf("session_id = %v", running.SessionID)
	}
	if string(running.Inputs) != `[{"type":"memory_store","memory_store_id":"memstore_01Hx"},{"type":"sessions","session_ids":["sesn_01","sesn_02"]}]` {
		t.Fatalf("inputs = %s", running.Inputs)
	}
	if strings.Contains(string(running.Outputs), "internal_session_id") {
		t.Fatalf("public outputs leaked internal_session_id: %s", running.Outputs)
	}
	if string(running.Outputs) != `[{"type":"memory_store","memory_store_id":"memstore_out"}]` {
		t.Fatalf("outputs = %s", running.Outputs)
	}

	pending := responseFromDream(db.Dream{
		ExternalID: "drm_pending",
		Status:     "pending",
		Model:      "claude-opus-4-8",
		Inputs:     json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_01Hx"},{"type":"sessions","session_ids":["sesn_01"]}]`),
		Outputs:    json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
		CreatedAt:  created,
		UpdatedAt:  created,
	})
	if pending.SessionID != nil {
		t.Fatalf("pending session_id = %v, want null", pending.SessionID)
	}
	if string(pending.Outputs) != `[]` {
		t.Fatalf("pending outputs = %s, want []", pending.Outputs)
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"session_id":null`, `"model":{"id":"claude-opus-4-8"}`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("pending response %s does not contain %s", encoded, fragment)
		}
	}
}

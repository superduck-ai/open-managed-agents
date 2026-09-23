package transcriptarchive

import (
	"bytes"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCodecFailures(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Fatal("empty input accepted")
	}
	events := []db.CodeSessionInternalEvent{{SequenceNum: 4, Payload: []byte(`{"x":1}`), EventMetadata: []byte(`{}`)}}
	encoded, err := Encode(events)
	if err != nil {
		t.Fatal(err)
	}
	expect := SegmentExpectation{Size: int64(len(encoded.Body)), RawBytes: encoded.RawBytes, SHA256: encoded.SHA256, EventCount: 1, FromSequence: 4, ToSequence: 4}
	for _, change := range []func(*SegmentExpectation){
		func(e *SegmentExpectation) { e.SHA256 = "bad" }, func(e *SegmentExpectation) { e.EventCount++ }, func(e *SegmentExpectation) { e.Size++ }, func(e *SegmentExpectation) { e.RawBytes++ }, func(e *SegmentExpectation) { e.FromSequence++ }, func(e *SegmentExpectation) { e.ToSequence++ },
	} {
		bad := expect
		change(&bad)
		if _, err := Decode(bytes.NewReader(encoded.Body), bad); err == nil {
			t.Fatal("bad manifest accepted")
		}
	}
	if _, err := Decode(bytes.NewReader(encoded.Body[:len(encoded.Body)-1]), expect); err == nil {
		t.Fatal("truncation accepted")
	}
	if _, err := Encode(append(events, events...)); err == nil {
		t.Fatal("duplicate sequence accepted")
	}
}

func TestCodecPreservesRawBytes(t *testing.T) {
	for _, payload := range []string{" \n{\"中文\":  [1, 2],\n\"a\":\"<tag>\"} \t", `{"a":1e2,"b":"\u4e2d"}`, `{"long":"` + string(bytes.Repeat([]byte("x"), 9*1024*1024)) + `"}`} {
		event := db.CodeSessionInternalEvent{SequenceNum: 42, ExternalID: "ie_original", PayloadHash: "worker hash is deliberately different", Payload: []byte(payload), EventMetadata: []byte(`{"meta": true}`)}
		encoded, err := Encode([]db.CodeSessionInternalEvent{event})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(bytes.NewReader(encoded.Body), SegmentExpectation{Size: int64(len(encoded.Body)), RawBytes: encoded.RawBytes, SHA256: encoded.SHA256, EventCount: 1, FromSequence: 42, ToSequence: 42})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got[42].Payload, event.Payload) || got[42].PayloadHash != event.PayloadHash {
			t.Fatal("payload bytes changed")
		}
	}
	scope := db.TranscriptScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "session"}
	if got := ObjectKey(scope, "segment"); got != "transcript-archive/org/workspace/session/segment.jsonl.zst" {
		t.Fatal(got)
	}
}

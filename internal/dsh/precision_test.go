package dsh

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeBodyRejectsAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{`{"input":{"a":1,"a":2}}`, `{"input":{"a":1,"\u0061":2}}`, `{"input":"\ud800"}`, "{\"input\":\"\xff\"}", `{"input":{}} {}`, `[]`, `null`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		if _, err := decodeBody[struct {
			Input json.RawMessage `json:"input"`
		}](httptest.NewRecorder(), r); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestDecodeBodyPreservesOpaqueJSON(t *testing.T) {
	input := `{ "order_id":9007199254740993, "decimal":0.123456789012345678901, "nested":[18446744073709551615] }`
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"input":`+input+`}`))
	got, err := decodeBody[struct {
		Input json.RawMessage `json:"input"`
	}](httptest.NewRecorder(), r)
	if err != nil || string(got.Input) != input {
		t.Fatalf("input=%s err=%v", got.Input, err)
	}
}

func TestDSRJSONEndpointsPreserveNumbers(t *testing.T) {
	for _, test := range []struct{ path, body, expected string }{
		{"/permission-requests", `{"input_id":"input","turn":"1","dsh_session_id":"cse_root","tool_use_id":"call","tool":{"kind":"builtin","name":"bash"},"input":{ "id":9007199254740993 }}`, `{ "id":9007199254740993 }`},
		{"/public-events", `{"events":[{ "id":"result", "type":"tool.result", "content":[{"number":9007199254740993}] }]}`, `{ "id":"result", "type":"tool.result", "content":[{"number":9007199254740993}] }`},
	} {
		stub := &executionStub{}
		response := transcriptRequest(executionRouter(stub), "POST", "/sessions/cse_root"+test.path, test.body, "1")
		if response.Code != 200 || stub.calls != 1 {
			t.Fatalf("response=%d calls=%d", response.Code, stub.calls)
		}
		got := stub.permission.Input
		if len(stub.events) > 0 {
			got = stub.events[0]
		}
		if string(got) != test.expected {
			t.Fatalf("payload=%s want=%s", got, test.expected)
		}
	}
}

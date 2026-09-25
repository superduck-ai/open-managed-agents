package messages

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestResponseObservationFailures(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"truncated", "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_a\"}}\n\n", "incomplete_response"},
		{"malformed", "data: {broken}\n\n", "invalid_response"},
		{"error", "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"}}\n\n", "provider_error"},
		{"oversized", "data: " + strings.Repeat("x", 4*1024*1024), "observation_limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			observation := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
			_, _ = observation.Write([]byte(test.body))
			observation.finish()
			if observation.result.ErrorType != test.want || observation.result.EndedAt.IsZero() {
				t.Fatalf("result = %+v", observation.result)
			}
		})
	}
}

func TestResponseObservationPreservesStreamAndPerRequestUsage(t *testing.T) {
	body := "event: message_start\r\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_a\",\"usage\":{\"input_tokens\":12,\"output_tokens\":1,\"cache_read_input_tokens\":5}}}\r\n\r\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"data: {\"type\":\"message_delta\",\n" + "data: \"usage\":{\"output_tokens\":7}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	for _, chunk := range []int{1, 17, len(body)} {
		observation := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
		for offset := 0; offset < len(body); offset += chunk {
			_, _ = observation.Write([]byte(body[offset:min(offset+chunk, len(body))]))
		}
		observation.finish()
		result := observation.result
		if result.ErrorType != "" || *result.Usage.InputTokens != 12 || *result.Usage.OutputTokens != 7 || *result.Usage.CacheReadInputTokens != 5 {
			t.Fatalf("result=%+v", result)
		}
		if len(result.EventIDs) != 2 || result.EventIDs[0] != maevents.StableAssistantEventID("cse_test", "msg_a", 0, "agent.thinking") || result.EventIDs[1] != maevents.StableAssistantEventID("cse_test", "msg_a", 1, "agent.message") {
			t.Fatalf("event IDs=%v", result.EventIDs)
		}
	}
	observation := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(io.TeeReader(strings.NewReader(body), observation))}
	recorder := httptest.NewRecorder()
	if err := writeProxyResponse(recorder, response); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != body {
		t.Fatal("observer changed response bytes")
	}
}

func TestResponseObservationNonStreaming(t *testing.T) {
	observation := &responseObservation{request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
	_, _ = observation.Write([]byte(`{"id":"msg_json","type":"message","usage":{"input_tokens":2,"output_tokens":0},"content":[{"type":"text","text":"answer"}]}`))
	observation.finish()
	if observation.result.ErrorType != "" || *observation.result.Usage.OutputTokens != 0 || len(observation.result.EventIDs) != 1 {
		t.Fatalf("result=%+v", observation.result)
	}
}

func TestResponseObservationRetainsUsageWhenClientWriteFails(t *testing.T) {
	observation := &responseObservation{request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
	body := `{"id":"msg_json","type":"message","usage":{"input_tokens":2,"output_tokens":3},"content":[{"type":"text","text":"answer"}]}`
	reader, writer := io.Pipe()
	_ = reader.Close()
	defer writer.Close()
	_, err := io.Copy(writer, io.TeeReader(strings.NewReader(body), observation))
	if err == nil {
		t.Fatal("client write unexpectedly succeeded")
	}
	observation.result.ErrorType = "stream_error"
	observation.finish()
	if !observation.complete || observation.result.ErrorType != "stream_error" || *observation.result.Usage.InputTokens != 2 || *observation.result.Usage.OutputTokens != 3 {
		t.Fatalf("result = %+v", observation.result)
	}
}

func TestResponseObservationClosesAtMessageStopBeforeEOF(t *testing.T) {
	observation := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
	ended := false
	observation.onComplete = func() { observation.finish(); ended = true }
	_, _ = observation.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	if !ended || observation.result.EndedAt.IsZero() {
		t.Fatal("message_stop did not close the request")
	}
}

func TestMergeRequestUsagePreservesMissingAndExplicitZero(t *testing.T) {
	usage := codesessions.ModelRequestUsage{
		InputTokens: new(int64(9)), OutputTokens: new(int64(4)),
		CacheCreationInputTokens: new(int64(3)), CacheReadInputTokens: new(int64(2)),
	}
	mergeRequestUsage(&usage, codesessions.ModelRequestUsage{})
	if *usage.InputTokens != 9 || *usage.OutputTokens != 4 || *usage.CacheCreationInputTokens != 3 || *usage.CacheReadInputTokens != 2 {
		t.Fatal("missing delta fields overwrote known usage")
	}
	mergeRequestUsage(&usage, codesessions.ModelRequestUsage{
		InputTokens: new(int64(0)), OutputTokens: new(int64(0)),
		CacheCreationInputTokens: new(int64(0)), CacheReadInputTokens: new(int64(0)),
	})
	if *usage.InputTokens != 0 || *usage.OutputTokens != 0 || *usage.CacheCreationInputTokens != 0 || *usage.CacheReadInputTokens != 0 {
		t.Fatal("explicit zero did not overwrite prior usage")
	}
}

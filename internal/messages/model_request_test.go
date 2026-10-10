package messages

import (
	"encoding/json"
	"fmt"
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

func TestResponseObservationInvalidToolInput(t *testing.T) {
	observation := &responseObservation{request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
	_, _ = observation.Write([]byte(`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_test","name":"Bash","input":null}]}`))
	observation.finish()
	if observation.result.ErrorType != "invalid_response" || len(observation.result.ToolUses) != 1 || !observation.result.ToolUses[0].InputRejected {
		t.Fatalf("invalid tool input result = %+v", observation.result)
	}
}

func TestResponseObservationRejectedToolInputPreservesRaw(t *testing.T) {
	for _, test := range []struct {
		raw    string
		length int
	}{
		{raw: "{\"x\":\x01}", length: 7},
		{raw: "{\"command\":", length: 11},
		{raw: "null", length: 4},
		{raw: "[]", length: 2},
		{raw: `"text"`, length: 6},
		{raw: "{\"x\":\"中😀\",\"y\":\x01}", length: 17},
	} {
		raw := test.raw
		t.Run(fmt.Sprintf("raw=%q", raw), func(t *testing.T) {
			partial, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			body := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_rejected\"}}\n\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_rejected\",\"name\":\"Bash\",\"input\":{}}}\n\n" +
				fmt.Sprintf("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", partial) +
				"data: {\"type\":\"message_stop\"}\n\n"
			observation := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
			recorder := httptest.NewRecorder()
			observation.onComplete = func() {
				observation.finish()
				if strings.Contains(recorder.Body.String(), "message_stop") {
					t.Error("rejected call observed after forwarding message_stop")
				}
			}
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(io.TeeReader(strings.NewReader(body), observation))}
			if err := writeProxyResponse(recorder, response); err != nil {
				t.Fatal(err)
			}
			if recorder.Body.String() != body || observation.result.ErrorType != "invalid_response" || len(observation.result.ToolUses) != 1 {
				t.Fatalf("rejected response changed or call missing: %+v", observation.result)
			}
			tool := observation.result.ToolUses[0]
			var input struct {
				Value unparsedToolInput `json:"__unparsedToolInput"`
			}
			if err := json.Unmarshal(tool.Input, &input); err != nil {
				t.Fatal(err)
			}
			if !tool.InputRejected || tool.ID != "toolu_rejected" || tool.Name != "Bash" || input.Value.Raw != raw || input.Value.Length != test.length {
				t.Fatalf("rejected input changed: %+v", tool)
			}
		})
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

func TestResponseObservationPreservesThinking(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			body := `{"id":"msg_thinking","type":"message","content":[{"type":"thinking","thinking":"公开思考正文","signature":"secret"},{"type":"redacted_thinking","data":"encrypted"}]}`
			if streaming {
				body = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_thinking\"}}\n\n" +
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"公开\"}}\n\n" +
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"思考正文\"}}\n\n" +
					"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"secret\"}}\n\n" +
					"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"encrypted\"}}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n"
			}
			observation := &responseObservation{streaming: streaming, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
			for _, b := range []byte(body) {
				_, _ = observation.Write([]byte{b})
			}
			observation.finish()
			messages := observation.result.Messages
			if observation.result.ErrorType != "" || len(messages) != 2 || len(messages[0].Content) != 1 || messages[0].Content[0].Thinking == nil || *messages[0].Content[0].Thinking != "公开思考正文" || len(messages[1].Content) != 0 {
				t.Fatalf("thinking result = %+v", observation.result)
			}
		})
	}
}

func TestResponseObservationLimitsThinkingContent(t *testing.T) {
	observation := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
	observation.observeFrame([]byte(`{"type":"message_start","message":{"id":"msg_limit"}}`))
	observation.observeFrame([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`))
	delta := responseStreamEvent{Index: 0, Delta: responseContentBlock{Type: "thinking_delta", Thinking: strings.Repeat("a", 1024*1024)}}
	for range 5 {
		observation.observeContentDelta(delta)
	}
	observation.finish()
	if observation.result.ErrorType != "observation_limit" || !observation.malformed || observation.blocks[0].thinkingBuffer.Len() != 4*1024*1024 {
		t.Fatalf("thinking observation exceeded bound: error=%q bytes=%d", observation.result.ErrorType, observation.blocks[0].thinkingBuffer.Len())
	}
}

func TestResponseObservationPreservesServerToolUsage(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		body := `{"id":"msg_search","type":"message","usage":{"server_tool_use":{"web_search_requests":2,"web_fetch_requests":1}},"content":[{"type":"text","text":"结果"}]}`
		if streaming {
			body = "data: {\"type\":\"message_start\",\"message\":" + body + "}\n\n" +
				`data: {"type":"message_delta","usage":{"output_tokens":3,"server_tool_use":{"web_search_requests":4}}}` + "\n\n" +
				`data: {"type":"message_delta","usage":{"output_tokens":5}}` + "\n\n" +
				`data: {"type":"message_stop"}` + "\n\n"
		}
		observation := &responseObservation{streaming: streaming, request: &codesessions.ModelRequest{CodeSessionID: "cse_search"}}
		_, _ = observation.Write([]byte(body))
		observation.finish()
		want := int64(2)
		if streaming {
			want = 4
		}
		usage := observation.result.Usage.ServerToolUse
		if observation.result.ErrorType != "" || usage == nil || usage.WebSearchRequests == nil || *usage.WebSearchRequests != want || usage.WebFetchRequests == nil || *usage.WebFetchRequests != 1 {
			t.Fatalf("搜索用量丢失或累计值错误：%+v，streaming=%v", observation.result, streaming)
		}
	}
}

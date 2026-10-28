package metering

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

func TestTransportChatUsageAndCompletion(t *testing.T) {
	usage := `"usage":{"prompt_tokens":2000,"completion_tokens":1000,"prompt_tokens_details":{"cached_tokens":1000}}`
	finish := `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	tail := `{"choices":[],` + usage + `}`
	frame := func(value string) string { return "data: " + value + "\r\n\r\n" }
	for _, tc := range []struct {
		name, body, contentType string
		readError               bool
		complete, known         bool
	}{
		{"done without finish", frame(tail) + frame(`[DONE]`), "text/event-stream", false, false, true},
		{"finish without usage", frame(finish) + frame(`[DONE]`), "text/event-stream", false, false, false},
		{"usage without finish or done", frame(tail), "text/event-stream", false, false, true},
		{"provider error after usage", frame(tail) + frame(`{"error":{"message":"failed"}}`) + frame(finish) + frame(`[DONE]`), "text/event-stream", false, false, true},
		{"JSON read error", `{"object":"chat.completion","choices":[{"finish_reason":"stop"}],` + usage + `}`, "application/json", true, false, true},
		{"read error after finish", frame(finish) + frame(tail), "text/event-stream", true, false, true},
		{"malformed tail", frame(finish) + frame(tail) + "data: {broken\n\n" + frame(`[DONE]`), "text/event-stream", false, false, true},
		{"truncated tail", frame(finish) + frame(tail) + "data: {", "text/event-stream", false, false, true},
		{"invalid cache", frame(finish) + frame(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":3}}}`) + frame(`[DONE]`), "text/event-stream", false, false, false},
		{"negative usage", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":-1,"completion_tokens":2}}`, "application/json", false, false, false},
		{"json", `{"object":"chat.completion","choices":[{"finish_reason":"stop"}],` + usage + `}`, "application/json", false, true, true},
		{"stream done", frame(finish) + frame(tail) + frame(`[DONE]`), "text/event-stream", false, true, true},
		{"stream EOF after finish", frame(finish) + frame(tail), "text/event-stream", false, true, true},
		{"multiline event", "data: {\"choices\":[],\n" + "data: " + usage + "}\n\n" + frame(finish) + frame(`[DONE]`), "text/event-stream", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordStore{}
			transport := &Transport{Store: store, Source: "api", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				Calculator: billing.NewCalculator(map[string]billing.ModelPrice{"model": {Rates: &billing.Rates{Input: new("10"), Output: new("20"), CacheRead: new("2")}}}),
				Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
					var body io.Reader = fragmentedChatReader{strings.NewReader(tc.body)}
					if tc.readError {
						body = io.MultiReader(body, failedChatReader{})
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(body)}, nil
				}),
			}
			request := meteredTestRequest(t)
			request.URL.Path = "/provider-prefix/v1/chat/completions"
			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			if (err != nil) != tc.readError || string(body) != tc.body {
				t.Fatalf("response changed: %q %v", body, err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if len(store.records) != 2 {
				t.Fatalf("record count = %d", len(store.records))
			}
			record := store.records[1]
			if !record.Finalized || record.Complete != tc.complete {
				t.Fatalf("completion = %+v", record)
			}
			if tc.known {
				if record.InputTokens != 1000 || record.CacheReadTokens != 1000 || record.OutputTokens != 1000 || record.ModelCents == nil || *record.ModelCents != 3 {
					t.Fatalf("usage = %+v", record)
				}
			} else if record.ModelCents != nil {
				t.Fatalf("unknown usage was priced: %+v", record)
			}
		})
	}
}

type failedChatReader struct{}

func (failedChatReader) Read([]byte) (int, error) {
	return 0, errors.New("upstream disconnected")
}

type fragmentedChatReader struct {
	io.Reader
}

func (r fragmentedChatReader) Read(data []byte) (int, error) {
	return r.Reader.Read(data[:min(len(data), 7)])
}

func TestChatObservationRequiresConfirmedUsageAndTerminalRead(t *testing.T) {
	for _, tc := range []struct {
		name, body                              string
		streaming, eof, complete, usageComplete bool
	}{
		{"json closed before EOF", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, false, false, false, true},
		{"missing output", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":20}}`, false, true, true, false},
		{"missing usage", `{"choices":[{"finish_reason":"stop"}]}`, false, true, true, false},
		{"negative output", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":-1}}`, false, true, false, false},
		{"early close", "data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n", true, false, false, true},
		{"unfinished choice", "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"},{\"index\":1,\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n", true, true, false, true},
		{"zero usage", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, false, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &observation{chat: &chatObservation{}, streaming: tc.streaming, eof: tc.eof}
			o.write([]byte(tc.body))
			o.end()
			if (o.complete && !o.failed) != tc.complete || o.usage.Complete() != tc.usageComplete {
				t.Fatalf("completion=%v failed=%v usage=%+v", o.complete, o.failed, o.usage)
			}
			if tc.name == "missing output" && (o.usage.InputTokens == nil || *o.usage.InputTokens != 20 || o.usage.OutputTokens != nil) {
				t.Fatalf("partial usage lost or fabricated: %+v", o.usage)
			}
			if tc.name == "zero usage" {
				cents, priced := billing.NewCalculator(map[string]billing.ModelPrice{"model": {Rates: &billing.Rates{Input: new("10"), Output: new("20")}}}).ModelRequestCents("model", o.usage.Tokens())
				if !priced || cents != 0 {
					t.Fatalf("zero usage price = %d, %v", cents, priced)
				}
			}
		})
	}
}

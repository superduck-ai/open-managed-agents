package metering

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type recordStore struct {
	records []db.UsageRecord
	fail    bool
}

func (s *recordStore) InsertUsageRecord(_ context.Context, record db.UsageRecord) error {
	if s.fail {
		return errors.New("storage unavailable")
	}
	s.records = append(s.records, record)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportRejectsUnrecordedAttempt(t *testing.T) {
	store := &recordStore{fail: true}
	transport := &Transport{Store: store, Base: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("未持久化就调用上游"); return nil, nil })}
	if _, err := transport.RoundTrip(meteredTestRequest(t)); err == nil {
		t.Fatal("写入错误未向上传递")
	}
}

func TestTransportRecordsCancelledAndCompleteResponses(t *testing.T) {
	cases := []struct {
		name, body, contentType string
		complete                bool
		cents                   int64
	}{
		{"截断流", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1000,\"output_tokens\":0}}}\n\n", "text/event-stream", false, 1},
		{"普通响应", `{"type":"message","usage":{"input_tokens":1000,"output_tokens":1000,"cache_read_input_tokens":1000,"cache_creation_input_tokens":1000,"server_tool_use":{"web_search_requests":2}}}`, "application/json", true, 4},
		{"流式累计", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1000,\"output_tokens\":0}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":1000}}\n\ndata: {\"type\":\"message_stop\"}\n\n", "text/event-stream", true, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordStore{}
			calculator := billing.NewCalculator(map[string]billing.ModelPrice{"model": {Rates: &billing.Rates{Input: new("10"), Output: new("10"), CacheRead: new("10"), CacheWrite: new("10")}}})
			transport := &Transport{Store: store, Calculator: calculator, Source: "api", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Header.Get("Accept-Encoding") != "" {
					t.Fatal("压缩响应会隐藏计量帧")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			request := meteredTestRequest(t)
			request.Header.Set("Accept-Encoding", "gzip")
			response, err := transport.RoundTrip(request)
			if request.Header.Get("Accept-Encoding") != "gzip" {
				t.Fatal("调用方请求被修改")
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if string(body) != tc.body || len(store.records) != 2 {
				t.Fatalf("响应被改变或重复结算：%s %+v", body, store.records)
			}
			first, last := store.records[0], store.records[1]
			if len(first.PriceSnapshot) == 0 || string(first.PriceSnapshot) != string(last.PriceSnapshot) {
				t.Fatal("价格快照未保留")
			}
			if first.Finalized || !last.Finalized || first.AttemptID != last.AttemptID || last.Complete != tc.complete || last.ModelCents == nil || *last.ModelCents != tc.cents {
				t.Fatalf("计量不正确：%+v", last)
			}
			if tc.name == "普通响应" && last.SearchCents != 2 {
				t.Fatal("搜索未计费")
			}
		})
	}
}

func meteredTestRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: "organization", WorkspaceUUID: "workspace", APIKeyUUID: "key"}), http.MethodPost, "https://example.test/v1/messages", strings.NewReader(`{"model":"model"}`))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

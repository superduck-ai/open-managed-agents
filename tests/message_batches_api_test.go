package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/batches"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type batchResponse struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	ProcessingStatus string `json:"processing_status"`
	RequestCounts    struct {
		Processing int `json:"processing"`
		Succeeded  int `json:"succeeded"`
		Errored    int `json:"errored"`
		Canceled   int `json:"canceled"`
		Expired    int `json:"expired"`
	} `json:"request_counts"`
	ResultsURL *string `json:"results_url"`
}

func TestMessageBatchesAPI(t *testing.T) {
	store := newFakeStore("fake-bucket")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstreamAPIKey = "sk-ant-upstream-test"
	cfg.BatchWorkerConcurrency = 1
	cfg.BatchJobLeaseDuration = time.Minute
	cfg.BatchJobLeaseHeartbeatInterval = time.Hour
	app := newTestAppWithStore(t, &cfg, store)
	defer app.close()

	t.Run("failure delete in_progress", func(t *testing.T) {
		created := createBatch(t, app, defaultTestKey, minimalBatchBody("delete-pending-1"))
		defer cleanupBatchRows(t, app.db, created.ID)

		resp := doBatchRequest(t, app, http.MethodDelete, "/v1/messages/batches/"+created.ID, nil, defaultTestKey, "")
		assertError(t, resp, http.StatusConflict, "invalid_request_error")
	})

	t.Run("failure official sdk fixture create bypasses real validation", func(t *testing.T) {
		fixtureCfg := app.cfg
		fixtureCfg.AnthropicUpstreamAPIKey = ""
		fixtureApp := newTestAppWithStore(t, &fixtureCfg, newFakeStore("fixture-bucket"))
		defer fixtureApp.close()

		body := `{"requests":[{"custom_id":"my-custom-id-1","params":{"model":"claude-opus-4-6","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":true,"speed":"standard"}}]}`
		resp := doBatchRequest(t, fixtureApp, http.MethodPost, "/v1/messages/batches", strings.NewReader(body), config.OfficialSDKResourceAPIKey, "application/json")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("fixture create status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var batch batchResponse
		decodeJSON(t, resp.Body, &batch)
		if batch.Type != "message_batch" || batch.ProcessingStatus != "in_progress" {
			t.Fatalf("unexpected fixture batch: %+v", batch)
		}
	})

	t.Run("success create process retrieve results delete", func(t *testing.T) {
		created := createBatch(t, app, defaultTestKey, minimalBatchBody("success-1", "success-2"))
		defer cleanupBatchRows(t, app.db, created.ID)

		prioritizeBatchJob(t, app.db, created.ID)
		upstream := &fakeBatchUpstream{}
		if err := batches.RunBatchOnce(context.Background(), app.db, store, app.cfg, upstream, "batch-worker-test"); err != nil {
			t.Fatalf("run batch worker: %v", err)
		}
		if len(upstream.calls) != 2 {
			t.Fatalf("upstream calls = %d, want 2", len(upstream.calls))
		}

		resp := doBatchRequest(t, app, http.MethodGet, "/v1/messages/batches/"+created.ID, nil, defaultTestKey, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retrieve status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var ended batchResponse
		decodeJSON(t, resp.Body, &ended)
		if ended.ProcessingStatus != "ended" || ended.RequestCounts.Succeeded != 2 || ended.ResultsURL == nil {
			t.Fatalf("unexpected ended batch: %+v", ended)
		}

		var objectKey string
		if err := app.db.Pool.QueryRow(context.Background(), `
			select results_s3_key
			from message_batches
			where external_id = $1
		`, created.ID).Scan(&objectKey); err != nil {
			t.Fatalf("load results key: %v", err)
		}
		if !strings.Contains(objectKey, "/message_batches/") || !strings.HasSuffix(objectKey, "/results.jsonl") {
			t.Fatalf("results object key = %s, want message batch results path", objectKey)
		}

		resultsResp := doBatchRequest(t, app, http.MethodGet, "/v1/messages/batches/"+created.ID+"/results", nil, defaultTestKey, "")
		defer resultsResp.Body.Close()
		if resultsResp.StatusCode != http.StatusOK {
			t.Fatalf("results status = %d, want 200: %s", resultsResp.StatusCode, readAll(t, resultsResp.Body))
		}
		lines := strings.Split(strings.TrimSpace(string(readAll(t, resultsResp.Body))), "\n")
		if len(lines) != 2 {
			t.Fatalf("jsonl line count = %d, want 2: %v", len(lines), lines)
		}
		seen := map[string]bool{}
		for _, line := range lines {
			var decoded struct {
				CustomID string `json:"custom_id"`
				Result   struct {
					Type string `json:"type"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &decoded); err != nil {
				t.Fatalf("decode jsonl line %q: %v", line, err)
			}
			seen[decoded.CustomID] = decoded.Result.Type == "succeeded"
		}
		if !seen["success-1"] || !seen["success-2"] {
			t.Fatalf("jsonl results = %+v, want both succeeded", seen)
		}

		deleteResp := doBatchRequest(t, app, http.MethodDelete, "/v1/messages/batches/"+created.ID, nil, defaultTestKey, "")
		defer deleteResp.Body.Close()
		if deleteResp.StatusCode != http.StatusOK {
			t.Fatalf("delete status = %d, want 200: %s", deleteResp.StatusCode, readAll(t, deleteResp.Body))
		}
	})
}

type fakeBatchUpstream struct {
	calls []db.MessageBatchRequest
}

func (u *fakeBatchUpstream) Send(_ context.Context, batch db.MessageBatch, req db.MessageBatchRequest) (batches.UpstreamResult, error) {
	u.calls = append(u.calls, req)
	message := json.RawMessage(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-test","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`)
	result, _ := json.Marshal(map[string]json.RawMessage{
		"type":    json.RawMessage(`"succeeded"`),
		"message": message,
	})
	return batches.UpstreamResult{Status: "succeeded", Result: result, UpstreamRequestID: "req_upstream_test"}, nil
}

func createBatch(t *testing.T, app *testApp, key string, body string) batchResponse {
	t.Helper()
	resp := doBatchRequest(t, app, http.MethodPost, "/v1/messages/batches", strings.NewReader(body), key, "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create batch status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var batch batchResponse
	decodeJSON(t, resp.Body, &batch)
	if batch.ID == "" {
		t.Fatalf("create batch returned empty id: %+v", batch)
	}
	return batch
}

func minimalBatchBody(customIDs ...string) string {
	var buf bytes.Buffer
	buf.WriteString(`{"requests":[`)
	for i, customID := range customIDs {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedID, _ := json.Marshal(customID)
		buf.WriteString(`{"custom_id":`)
		buf.Write(encodedID)
		buf.WriteString(`,"params":{"model":"claude-opus-4-6","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}}`)
	}
	buf.WriteString(`]}`)
	return buf.String()
}

func doBatchRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if strings.Contains(path, "beta=true") {
		req.Header.Set("anthropic-beta", "message-batches-2024-09-24")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do batch request: %v", err)
	}
	return resp
}

func prioritizeBatchJob(t *testing.T, database *db.DB, batchID string) {
	t.Helper()
	if _, err := database.Pool.Exec(context.Background(), `
		update jobs
		set run_after = '2000-01-01T00:00:00Z', created_at = '2000-01-01T00:00:00Z'
		where type = 'message_batch_process'
			and payload->>'message_batch_external_id' = $1
	`, batchID); err != nil {
		t.Fatalf("prioritize batch job: %v", err)
	}
}

func cleanupBatchRows(t *testing.T, database *db.DB, batchID string) {
	t.Helper()
	if _, err := database.Pool.Exec(context.Background(), `
		delete from jobs
		where type = 'message_batch_process'
			and payload->>'message_batch_external_id' = $1
	`, batchID); err != nil {
		t.Fatalf("cleanup batch jobs: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `
		delete from message_batch_requests
		where message_batch_id in (select id from message_batches where external_id = $1)
	`, batchID); err != nil {
		t.Fatalf("cleanup batch requests: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from message_batches where external_id = $1`, batchID); err != nil {
		t.Fatalf("cleanup message batch: %v", err)
	}
}

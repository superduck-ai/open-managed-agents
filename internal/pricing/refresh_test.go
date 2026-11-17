package pricing

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type stoppingLogWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *stoppingLogWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.cancel()
	return n, err
}

func TestRefreshStructuredLogs(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model","pricing":{"prompt":"0.000001","completion":"0"}}]}`))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			output := &stoppingLogWriter{cancel: cancel}
			s := testStore(t)
			s.logger = slog.New(slog.NewJSONHandler(output, nil))
			s.refreshLoop(ctx, priceSource{"openrouter", server.URL, Parse})
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["source"] != "openrouter" {
				t.Fatalf("缺少日志来源: %+v", record)
			}
			if status == http.StatusOK {
				if record["level"] != "INFO" || record["revision"] == nil || record["fetched_at"] == nil || record["model_count"] != float64(1) {
					t.Fatalf("成功日志缺少元数据: %+v", record)
				}
			} else if record["level"] != "WARN" || record["failures"] != float64(1) || record["error"] == nil {
				t.Fatalf("失败日志缺少元数据: %+v", record)
			}
		})
	}
}

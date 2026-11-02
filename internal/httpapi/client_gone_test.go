package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogFailure(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		name     string
		ctx      context.Context
		level    string
		canceled bool
	}{
		{"active request", t.Context(), "ERROR", false},
		{"client canceled", canceled, "INFO", true},
	} {
		var logs bytes.Buffer
		request := httptest.NewRequest(http.MethodPost, "/v1/code/sessions/cse_a/worker/events", nil).WithContext(tc.ctx)
		if ClientGone(request) != tc.canceled {
			t.Fatalf("%s: client gone = %v", tc.name, !tc.canceled)
		}
		LogFailure(slog.New(slog.NewJSONHandler(&logs, nil)), request, "append code session worker events", "code_session_id", "cse_a", "error", context.Canceled)
		var record map[string]any
		if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record["level"] != tc.level || record["code_session_id"] != "cse_a" || (record["client_canceled"] == true) != tc.canceled {
			t.Fatalf("%s: %v", tc.name, record)
		}
	}
}

package memory

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemoryRejectsUnicodeSeparatorsBeforeDataAccess(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		for _, path := range []string{"/sentinel.md\u2028line", "/sentinel.md\u2029paragraph"} {
			t.Run(operation+path, func(t *testing.T) {
				body, err := json.Marshal(map[string]any{"path": path, "content": "controlled regression"})
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
				recorder := httptest.NewRecorder()
				func() {
					defer func() {
						if recover() != nil {
							t.Error("SDK-forbidden path reached data access")
						}
					}()
					h := &Handler{}
					if operation == "create" {
						h.createMemory(recorder, request, "memstore_test")
					} else {
						h.updateMemory(recorder, request, "memstore_test", "mem_test")
					}
				}()
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("status=%d want400", recorder.Code)
				}
				var response struct {
					Error struct {
						Type string `json:"type"`
					} `json:"error"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Error.Type != "invalid_request_error" {
					t.Fatalf("wrong error envelope: %s", recorder.Body.String())
				}
			})
		}
	}
}

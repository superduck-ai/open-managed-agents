package sessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestMapFileResourcePersistenceErrorMapsTypedConflicts(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
	}{
		{
			name:       "resource limit",
			err:        &db.SessionFileResourceLimitError{Limit: db.MaxSessionFileResources},
			wantStatus: http.StatusBadRequest,
			wantType:   "invalid_request_error",
		},
		{
			name: "managed resource path conflict",
			err: &db.SessionFileMountConflictError{
				Path:            "/uploads/workspace",
				ConflictingPath: "/uploads/workspace/data.csv",
			},
			wantStatus: http.StatusBadRequest,
			wantType:   "invalid_request_error",
		},
		{
			name:       "ordinary Filestore path conflict",
			err:        db.ErrFilestorePathExists,
			wantStatus: http.StatusConflict,
			wantType:   "conflict_error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/sessions/session_test/resources", nil)
			mapped, ok := mapFileResourcePersistenceError(test.err)
			if !ok {
				t.Fatal("mapFileResourcePersistenceError() did not handle error")
			}
			httpapi.NewErrorAdapter(nil).Write(recorder, request, mapped)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			var response struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Error.Type != test.wantType {
				t.Fatalf("error type = %q, want %q", response.Error.Type, test.wantType)
			}
		})
	}
}

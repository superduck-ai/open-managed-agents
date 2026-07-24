package sessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

func TestWriteFileResourcePersistenceErrorMapsTypedConflicts(t *testing.T) {
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
			if !writeFileResourcePersistenceError(recorder, request, test.err) {
				t.Fatal("writeFileResourcePersistenceError() did not handle error")
			}
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

func TestValidateSessionResourceMounts(t *testing.T) {
	t.Run("rejects too many files", func(t *testing.T) {
		resources := make([]db.SessionResource, 0, db.MaxSessionFileResources+1)
		for index := 0; index <= db.MaxSessionFileResources; index++ {
			resources = append(resources, testFileResource("/workspace/files/"+strings.Repeat("x", index+1)))
		}
		if err := validateSessionResourceMounts(resources); err == nil {
			t.Fatal("validateSessionResourceMounts() accepted more than 100 files")
		}
	})
	t.Run("rejects duplicate paths", func(t *testing.T) {
		resources := []db.SessionResource{
			testFileResource("/workspace/data.csv"),
			testFileResource("/workspace/data.csv"),
		}
		if err := validateSessionResourceMounts(resources); err == nil {
			t.Fatal("validateSessionResourceMounts() accepted duplicate paths")
		}
	})
	t.Run("allows paths that only overlap repositories outside uploads", func(t *testing.T) {
		resources := []db.SessionResource{
			testMountResource("github_repository", "/workspace/repository"),
			testFileResource("/workspace/repository/data.csv"),
		}
		if err := validateSessionResourceMounts(resources); err != nil {
			t.Fatalf("validateSessionResourceMounts(): %v", err)
		}
	})
	t.Run("accepts distinct paths", func(t *testing.T) {
		resources := []db.SessionResource{
			testMountResource("github_repository", "/workspace/repository"),
			testFileResource("/workspace/data.csv"),
			testFileResource("/workspace/input/config.json"),
		}
		if err := validateSessionResourceMounts(resources); err != nil {
			t.Fatalf("validateSessionResourceMounts(): %v", err)
		}
	})
}

func testFileResource(mountPath string) db.SessionResource {
	return testMountResource("file", mountPath)
}

func testMountResource(resourceType, mountPath string) db.SessionResource {
	payload := map[string]any{
		"id":         "sesrsc_test",
		"type":       resourceType,
		"mount_path": mountPath,
	}
	if resourceType == "file" {
		payload["file_id"] = "file_test"
		payload["source"] = sandboxmount.FileSource
	}
	raw, _ := json.Marshal(payload)
	return db.SessionResource{
		ExternalID:   "sesrsc_test",
		ResourceType: resourceType,
		Payload:      raw,
	}
}

package sessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
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

func TestValidateNormalizedSessionResources(t *testing.T) {
	t.Run("rejects too many files", func(t *testing.T) {
		resources := make([]normalizedSessionResource, 0, db.MaxSessionFileResources+1)
		for index := 0; index <= db.MaxSessionFileResources; index++ {
			resources = append(resources, testNormalizedFileResource(t, "/workspace/files/"+strings.Repeat("x", index+1)))
		}
		if err := validateNormalizedSessionResources(resources); err == nil {
			t.Fatalf("validateNormalizedSessionResources() accepted more than %d files", db.MaxSessionFileResources)
		}
	})
	t.Run("rejects duplicate paths", func(t *testing.T) {
		resources := []normalizedSessionResource{
			testNormalizedFileResource(t, "/workspace/data.csv"),
			testNormalizedFileResource(t, "/workspace/data.csv"),
		}
		if err := validateNormalizedSessionResources(resources); err == nil {
			t.Fatal("validateNormalizedSessionResources() accepted duplicate paths")
		}
	})
	t.Run("allows paths that only overlap repositories outside uploads", func(t *testing.T) {
		resources := []normalizedSessionResource{
			{resource: db.SessionResource{ResourceType: "github_repository"}},
			testNormalizedFileResource(t, "/workspace/repository/data.csv"),
		}
		if err := validateNormalizedSessionResources(resources); err != nil {
			t.Fatalf("validateNormalizedSessionResources(): %v", err)
		}
	})
	t.Run("accepts distinct paths", func(t *testing.T) {
		resources := []normalizedSessionResource{
			{resource: db.SessionResource{ResourceType: "github_repository"}},
			testNormalizedFileResource(t, "/workspace/data.csv"),
			testNormalizedFileResource(t, "/workspace/input/config.json"),
		}
		if err := validateNormalizedSessionResources(resources); err != nil {
			t.Fatalf("validateNormalizedSessionResources(): %v", err)
		}
	})
}

func TestPlanSessionResourceWritesSharesFileBinding(t *testing.T) {
	resource := testNormalizedFileResource(t, "/workspace/data.csv")
	resource.fileMimeType = "text/csv"
	plan, err := planSessionResourceWrites([]normalizedSessionResource{resource})
	if err != nil {
		t.Fatalf("planSessionResourceWrites() error = %v", err)
	}
	if len(plan.inputs) != 1 || plan.inputs[0].FileMount == nil || len(plan.eventBindings) != 1 {
		t.Fatalf("planSessionResourceWrites() = %+v", plan)
	}
	mount := plan.inputs[0].FileMount
	binding := plan.eventBindings[0]
	if binding.FileID != mount.FileExternalID || binding.Path != mount.Path || binding.MimeType != "text/csv" {
		t.Fatalf("event binding = %+v, file mount = %+v", binding, mount)
	}
}

func testNormalizedFileResource(t *testing.T, mountPath string) normalizedSessionResource {
	t.Helper()
	raw, err := json.Marshal(mountPath)
	if err != nil {
		t.Fatalf("marshal mount path: %v", err)
	}
	spec, err := sessionresource.NormalizeFileSpec("file_test", "data.csv", nil, raw)
	if err != nil {
		t.Fatalf("normalize FileSpec: %v", err)
	}
	return normalizedSessionResource{
		resource: db.SessionResource{
			ExternalID:   "sesrsc_test",
			ResourceType: sessionresource.FileType,
		},
		fileSpec: new(spec),
	}
}

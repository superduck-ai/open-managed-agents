package deployments

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionResourcesFromDeploymentRejectsInvalidSecrets(t *testing.T) {
	_, err := sessionResourcesFromDeployment(db.Deployment{ResourceSecrets: json.RawMessage(`[]`)}, time.Time{})
	if err == nil {
		t.Fatal("sessionResourcesFromDeployment() error = nil")
	}
}

func TestDeploymentResponseUsesEmptyDescription(t *testing.T) {
	response, err := json.Marshal(deploymentResponse{})
	if err != nil || !strings.Contains(string(response), `"description":""`) {
		t.Fatalf("deployment response = %s, error = %v", response, err)
	}
}

func TestDeploymentResourcesResponse(t *testing.T) {
	t.Run("rejects invalid stored resources", func(t *testing.T) {
		if _, err := deploymentResourcesResponse(json.RawMessage(`{"type":"file"}`)); err == nil {
			t.Fatal("deploymentResourcesResponse() error = nil")
		}
	})

	t.Run("removes internal and write-only fields", func(t *testing.T) {
		response, err := deploymentResourcesResponse(json.RawMessage(`[
			{"type":"file","file_id":"file_default","source":"/uploads","mount_path":"/file_default","_oma_mount_path_defaulted":true},
			{"type":"github_repository","url":"https://github.com/example/repo.git","authorization_token":"secret"}
		]`))
		if err != nil {
			t.Fatalf("deploymentResourcesResponse() error = %v", err)
		}
		if strings.Contains(string(response), "source") || strings.Contains(string(response), "authorization_token") || strings.Contains(string(response), deploymentMountPathDefaulted) {
			t.Fatalf("response leaks internal fields: %s", response)
		}
	})

	t.Run("uses official default mount path without rewriting explicit path", func(t *testing.T) {
		response, err := deploymentResourcesResponse(json.RawMessage(`[
			{"type":"file","file_id":"file_default","source":"/uploads","mount_path":"/file_default","_oma_mount_path_defaulted":true},
			{"type":"file","file_id":"file_explicit","source":"/uploads","mount_path":"/file_explicit","_oma_mount_path_defaulted":false},
			{"type":"file","file_id":"file_legacy_explicit","source":"/uploads","mount_path":"/file_legacy_explicit"}
		]`))
		if err != nil {
			t.Fatalf("deploymentResourcesResponse() error = %v", err)
		}
		if !strings.Contains(string(response), `"mount_path":"/mnt/session/uploads/file_default"`) {
			t.Fatalf("default mount path is not public: %s", response)
		}
		if !strings.Contains(string(response), `"mount_path":"/file_explicit"`) {
			t.Fatalf("explicit mount path was rewritten: %s", response)
		}
		if !strings.Contains(string(response), `"mount_path":"/file_legacy_explicit"`) {
			t.Fatalf("unmarked explicit mount path was rewritten: %s", response)
		}
	})
}

func TestPatchDeploymentMetadata(t *testing.T) {
	t.Run("rejects top-level null", func(t *testing.T) {
		if _, err := patchDeploymentMetadata(json.RawMessage(`{"old":"value"}`), json.RawMessage(`null`)); err == nil {
			t.Fatal("patchDeploymentMetadata() error = nil")
		}
	})

	t.Run("rejects non-string values", func(t *testing.T) {
		if _, err := patchDeploymentMetadata(json.RawMessage(`{"old":"value"}`), json.RawMessage(`{"new":1}`)); err == nil {
			t.Fatal("patchDeploymentMetadata() error = nil")
		}
	})

	t.Run("upserts empty strings and deletes only null values", func(t *testing.T) {
		patched, err := patchDeploymentMetadata(
			json.RawMessage(`{"delete":"value","keep":"value"}`),
			json.RawMessage(`{"delete":null,"empty":""}`),
		)
		if err != nil {
			t.Fatalf("patchDeploymentMetadata() error = %v", err)
		}
		if strings.Contains(string(patched), `"delete"`) || !strings.Contains(string(patched), `"empty":""`) || !strings.Contains(string(patched), `"keep":"value"`) {
			t.Fatalf("patched metadata = %s", patched)
		}
	})
}

func TestNormalizeInitialEvents(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "system message without preceding user message",
			raw:  `[{"type":"system.message","content":[{"type":"text","text":"context"}]}]`,
		},
		{
			name: "system message before final event",
			raw:  `[{"type":"user.message","content":[{"type":"text","text":"first"}]},{"type":"system.message","content":[{"type":"text","text":"context"}]},{"type":"user.message","content":[{"type":"text","text":"second"}]}]`,
		},
		{
			name: "duplicate system messages",
			raw:  `[{"type":"user.message","content":[{"type":"text","text":"first"}]},{"type":"system.message","content":[{"type":"text","text":"context"}]},{"type":"system.message","content":[{"type":"text","text":"duplicate"}]}]`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeInitialEvents(json.RawMessage(test.raw)); err == nil {
				t.Fatal("normalizeInitialEvents() error = nil")
			}
		})
	}

	t.Run("accepts final system message after user message", func(t *testing.T) {
		if _, err := normalizeInitialEvents(json.RawMessage(`[{"type":"user.message","content":[{"type":"text","text":"hello"}]},{"type":"system.message","content":[{"type":"text","text":"context"}]}]`)); err != nil {
			t.Fatalf("normalizeInitialEvents() error = %v", err)
		}
	})
}

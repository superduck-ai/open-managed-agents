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

func TestSessionResourcesFromDeploymentRejectsNullResource(t *testing.T) {
	_, err := sessionResourcesFromDeployment(db.Deployment{Resources: json.RawMessage(`[null]`)}, time.Time{})
	if err == nil {
		t.Fatal("sessionResourcesFromDeployment() error = nil")
	}
}

func TestSessionResourcesFromDeploymentBuildsTypedPayload(t *testing.T) {
	resources, err := sessionResourcesFromDeployment(db.Deployment{
		Resources:       json.RawMessage(`[{"type":"github_repository","url":"https://github.com/example/repo.git","mount_path":"/workspace/repo","checkout":{"type":"branch","name":"main"}}]`),
		ResourceSecrets: json.RawMessage(`{"0":{"authorization_token":"secret"}}`),
	}, time.Time{})
	if err != nil {
		t.Fatalf("sessionResourcesFromDeployment() error = %v", err)
	}
	if len(resources) != 1 || resources[0].Resource.ResourceType != "github_repository" {
		t.Fatalf("resources = %+v", resources)
	}
	var payload struct {
		ID       string              `json:"id"`
		Type     string              `json:"type"`
		URL      string              `json:"url"`
		Checkout *deploymentCheckout `json:"checkout"`
	}
	if err := json.Unmarshal(resources[0].Resource.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.ID == "" || payload.Type != "github_repository" || payload.URL == "" || payload.Checkout == nil || payload.Checkout.Name != "main" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestDeploymentResponseUsesEmptyDescription(t *testing.T) {
	response, err := json.Marshal(deploymentResponse{})
	if err != nil || !strings.Contains(string(response), `"description":""`) {
		t.Fatalf("deployment response = %s, error = %v", response, err)
	}
}

func TestDeploymentRunResponseBuildsOfficialTriggerContext(t *testing.T) {
	manual, err := responseFromRun(db.DeploymentRun{TriggerType: "manual"})
	if err != nil {
		t.Fatalf("responseFromRun(manual): %v", err)
	}
	if manual.TriggerContext.Type != "manual" || manual.TriggerContext.ScheduledAt != "" {
		t.Fatalf("manual trigger context = %+v", manual.TriggerContext)
	}

	scheduledAt := time.Date(2026, time.August, 6, 14, 20, 0, 0, time.UTC)
	scheduled, err := responseFromRun(db.DeploymentRun{TriggerType: "schedule", ScheduledAt: &scheduledAt})
	if err != nil {
		t.Fatalf("responseFromRun(schedule): %v", err)
	}
	if scheduled.TriggerContext.Type != "schedule" || scheduled.TriggerContext.ScheduledAt != "2026-08-06T14:20:00Z" {
		t.Fatalf("schedule trigger context = %+v", scheduled.TriggerContext)
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
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		if strings.Contains(string(encoded), "source") || strings.Contains(string(encoded), "authorization_token") || strings.Contains(string(encoded), "_oma_mount_path_defaulted") {
			t.Fatalf("response leaks internal fields: %s", encoded)
		}
	})

	t.Run("maps every file mount path into the uploads namespace", func(t *testing.T) {
		response, err := deploymentResourcesResponse(json.RawMessage(`[
			{"type":"file","file_id":"file_default","source":"/uploads","mount_path":"/file_default","_oma_mount_path_defaulted":true},
			{"type":"file","file_id":"file_explicit","source":"/uploads","mount_path":"/file_explicit","_oma_mount_path_defaulted":false},
			{"type":"file","file_id":"file_legacy_explicit","source":"/uploads","mount_path":"/file_legacy_explicit"}
		]`))
		if err != nil {
			t.Fatalf("deploymentResourcesResponse() error = %v", err)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		if !strings.Contains(string(encoded), `"mount_path":"/uploads/file_default"`) {
			t.Fatalf("default mount path is not public: %s", encoded)
		}
		if !strings.Contains(string(encoded), `"mount_path":"/uploads/file_explicit"`) {
			t.Fatalf("explicit mount path is not mapped into uploads: %s", encoded)
		}
		if !strings.Contains(string(encoded), `"mount_path":"/uploads/file_legacy_explicit"`) {
			t.Fatalf("unmarked mount path is not mapped into uploads: %s", encoded)
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

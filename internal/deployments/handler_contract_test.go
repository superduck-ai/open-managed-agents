package deployments

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestPlanDeploymentSessionResourcesRejectsInvalidSecrets(t *testing.T) {
	_, err := planDeploymentSessionResources(
		db.Deployment{ResourceSecrets: json.RawMessage(`[]`)},
		nil,
		time.Time{},
	)
	if err == nil {
		t.Fatal("planDeploymentSessionResources() error = nil")
	}
}

func TestParseDeploymentRunResourcesRejectsNullResource(t *testing.T) {
	_, err := parseDeploymentRunResources(json.RawMessage(`[null]`))
	if err == nil {
		t.Fatal("parseDeploymentRunResources() error = nil")
	}
}

func TestParseDeploymentRunResources(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "top level is not an array", raw: `{"type":"file"}`},
		{name: "resource is null", raw: `[null]`},
		{name: "type is not a string", raw: `[{"type":1,"file_id":"file_test"}]`},
		{name: "unsupported type", raw: `[{"type":"directory"}]`},
		{name: "file ID is missing", raw: `[{"type":"file"}]`},
		{name: "file ID is not a string", raw: `[{"type":"file","file_id":1}]`},
		{name: "memory store ID is missing", raw: `[{"type":"memory_store"}]`},
		{name: "memory store ID is not a string", raw: `[{"type":"memory_store","memory_store_id":1}]`},
		{name: "GitHub URL is missing", raw: `[{"type":"github_repository"}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDeploymentRunResources(json.RawMessage(test.raw)); err == nil {
				t.Fatal("parseDeploymentRunResources() error = nil")
			}
		})
	}

	t.Run("accepts normalized resource references", func(t *testing.T) {
		resources, err := parseDeploymentRunResources(json.RawMessage(`[
			{"type":"file","file_id":"file_test","source":"/uploads","mount_path":"/uploads/file.txt"},
			{"type":"memory_store","memory_store_id":"mem_test"},
			{"type":"github_repository","url":"https://github.com/example/repo.git"}
		]`))
		if err != nil {
			t.Fatalf("parseDeploymentRunResources() error = %v", err)
		}
		if len(resources) != 3 || resources[0].fileSpec == nil ||
			resources[0].fileSpec.FileID() != "file_test" ||
			resources[1].payload.MemoryStoreID != "mem_test" ||
			resources[2].payload.Type != "github_repository" {
			t.Fatalf("parseDeploymentRunResources() = %+v", resources)
		}
	})
}

func TestPlanDeploymentSessionResourcesSharesFileBinding(t *testing.T) {
	stored, err := parseDeploymentRunResources(json.RawMessage(`[
		{"type":"file","file_id":"file_test","source":"/uploads","mount_path":"/workspace/data.csv"}
	]`))
	if err != nil {
		t.Fatalf("parseDeploymentRunResources() error = %v", err)
	}
	stored[0].file = db.FileRecord{ExternalID: "file_test", MimeType: "text/csv"}
	plan, err := planDeploymentSessionResources(
		db.Deployment{},
		stored,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("planDeploymentSessionResources() error = %v", err)
	}
	if len(plan.resources) != 1 || plan.resources[0].FileMount == nil || len(plan.eventBindings) != 1 {
		t.Fatalf("planDeploymentSessionResources() = %+v", plan)
	}
	mount := plan.resources[0].FileMount
	want := sessioncontract.EventFileBinding{FileID: mount.FileExternalID, Path: mount.Path, MimeType: "text/csv"}
	if plan.eventBindings[0] != want {
		t.Fatalf("event binding = %+v, want %+v", plan.eventBindings[0], want)
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

func TestScheduleResponseKeepsInvalidStoredCron(t *testing.T) {
	response := scheduleResponse(
		json.RawMessage(`{"type":"cron","expression":"bad","timezone":"UTC"}`),
		nil,
		time.Now(),
		false,
	)
	if response == nil || response.Expression != "bad" || len(response.UpcomingRunsAt) != 0 {
		t.Fatalf("scheduleResponse() = %+v", response)
	}
}

func TestDeploymentResponseShowsUpcomingRunsWhilePaused(t *testing.T) {
	response, err := responseFromDeployment(db.Deployment{
		ExternalID: "deployment_test",
		Status:     "paused",
		Schedule:   json.RawMessage(`{"type":"cron","expression":"0 9 * * *","timezone":"UTC"}`),
	}, time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("responseFromDeployment() error = %v", err)
	}
	if response.Schedule == nil || len(response.Schedule.UpcomingRunsAt) != upcomingRunCount {
		t.Fatalf("paused upcoming_runs_at = %#v", response.Schedule)
	}
	if response.Schedule.UpcomingRunsAt[0] != "2026-08-19T09:00:00Z" {
		t.Fatalf("paused upcoming_runs_at[0] = %q", response.Schedule.UpcomingRunsAt[0])
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

	t.Run("maps every file mount path into the uploads namespace", func(t *testing.T) {
		response, err := deploymentResourcesResponse(json.RawMessage(`[
			{"type":"file","file_id":"file_default","source":"/uploads","mount_path":"/file_default","_oma_mount_path_defaulted":true},
			{"type":"file","file_id":"file_explicit","source":"/uploads","mount_path":"/file_explicit","_oma_mount_path_defaulted":false},
			{"type":"file","file_id":"file_legacy_explicit","source":"/uploads","mount_path":"/file_legacy_explicit"}
		]`))
		if err != nil {
			t.Fatalf("deploymentResourcesResponse() error = %v", err)
		}
		if !strings.Contains(string(response), `"mount_path":"/uploads/file_default"`) {
			t.Fatalf("default mount path is not public: %s", response)
		}
		if !strings.Contains(string(response), `"mount_path":"/uploads/file_explicit"`) {
			t.Fatalf("explicit mount path is not mapped into uploads: %s", response)
		}
		if !strings.Contains(string(response), `"mount_path":"/uploads/file_legacy_explicit"`) {
			t.Fatalf("unmarked mount path is not mapped into uploads: %s", response)
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

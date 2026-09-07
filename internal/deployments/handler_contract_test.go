package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestSessionResourcesFromDeploymentRejectsInvalidSecrets(t *testing.T) {
	_, err := sessionResourcesFromDeployment(db.Deployment{ResourceSecrets: json.RawMessage(`[]`)}, time.Time{}, nil)
	if err == nil {
		t.Fatal("sessionResourcesFromDeployment() error = nil")
	}
}

func TestSessionResourcesFromDeploymentRejectsNullResource(t *testing.T) {
	_, err := sessionResourcesFromDeployment(db.Deployment{Resources: json.RawMessage(`[null]`)}, time.Time{}, nil)
	if err == nil {
		t.Fatal("sessionResourcesFromDeployment() error = nil")
	}
}

func TestSessionResourcesFromDeploymentSnapshotsMemoryStores(t *testing.T) {
	resources, err := sessionResourcesFromDeployment(db.Deployment{
		OrganizationUUID: "org",
		WorkspaceUUID:    "ws",
		Resources: json.RawMessage(`[
			{"type":"github_repository","url":"https://github.com/example/repo.git","mount_path":"/repo"},
			{"type":"memory_store","memory_store_id":"memstore_one","access":"read_only","instructions":"keep notes"}
		]`),
	}, time.Time{}, map[string]db.MemoryStore{
		"memstore_one": {
			ExternalID:  "memstore_one",
			Name:        "Product Docs-Draft!!",
			Description: "personal taste",
		},
	})
	if err != nil {
		t.Fatalf("sessionResourcesFromDeployment() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %d, want 2", len(resources))
	}
	var github map[string]any
	if err := json.Unmarshal(resources[0].Resource.Payload, &github); err != nil {
		t.Fatalf("decode github payload: %v", err)
	}
	if github["type"] != "github_repository" ||
		github["url"] != "https://github.com/example/repo.git" ||
		github["mount_path"] != "/repo" {
		t.Fatalf("github payload = %#v", github)
	}
	if _, ok := github["id"].(string); !ok || github["id"] == "" {
		t.Fatalf("github id = %#v", github["id"])
	}
	var payload map[string]any
	if err := json.Unmarshal(resources[1].Resource.Payload, &payload); err != nil {
		t.Fatalf("decode memory payload: %v", err)
	}
	if payload["type"] != "memory_store" ||
		payload["memory_store_id"] != "memstore_one" ||
		payload["access"] != "read_only" ||
		payload["instructions"] != "keep notes" ||
		payload["name"] != "Product Docs-Draft!!" ||
		payload["description"] != "personal taste" ||
		payload["mount_path"] != "/mnt/memory/product-docs-draft" {
		t.Fatalf("memory payload = %#v", payload)
	}
}

func TestSessionResourcesFromDeploymentRejectsMalformedMemoryFields(t *testing.T) {
	stores := map[string]db.MemoryStore{
		"memstore_one": {ExternalID: "memstore_one", Name: "notes"},
	}
	for _, test := range []struct {
		name        string
		resource    string
		wantErr     error
		wantMessage string
	}{
		{
			name:     "access is not a string",
			resource: `{"type":"memory_store","memory_store_id":"memstore_one","access":7}`,
			wantErr:  sessionresource.ErrMemoryStoreAccess,
		},
		{
			name:     "access is an unknown value",
			resource: `{"type":"memory_store","memory_store_id":"memstore_one","access":"write"}`,
			wantErr:  sessionresource.ErrMemoryStoreAccess,
		},
		{
			name:        "memory_store_id is missing",
			resource:    `{"type":"memory_store","access":"read_only"}`,
			wantMessage: "memory_store_id",
		},
		{
			name:        "instructions is not a string",
			resource:    `{"type":"memory_store","memory_store_id":"memstore_one","instructions":7}`,
			wantMessage: "instructions",
		},
		{
			name:        "store was not loaded for snapshot",
			resource:    `{"type":"memory_store","memory_store_id":"memstore_missing"}`,
			wantMessage: "memory store not found",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := sessionResourcesFromDeployment(db.Deployment{
				OrganizationUUID: "org",
				WorkspaceUUID:    "ws",
				Resources:        json.RawMessage("[" + test.resource + "]"),
			}, time.Time{}, stores)
			if err == nil {
				t.Fatal("sessionResourcesFromDeployment() error = nil, want rejection")
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantMessage != "" && !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("error = %v, want it to mention %q", err, test.wantMessage)
			}
		})
	}
}

func TestSessionResourcesFromDeploymentDefaultsAbsentMemoryAccess(t *testing.T) {
	resources, err := sessionResourcesFromDeployment(db.Deployment{
		OrganizationUUID: "org",
		WorkspaceUUID:    "ws",
		Resources:        json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_one"}]`),
	}, time.Time{}, map[string]db.MemoryStore{
		"memstore_one": {ExternalID: "memstore_one", Name: "notes"},
	})
	if err != nil {
		t.Fatalf("sessionResourcesFromDeployment() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(resources[0].Resource.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["access"] != sessionresource.MemoryAccessReadWrite {
		t.Fatalf("access = %#v, want read_write", payload["access"])
	}
}

func TestLoadDeploymentMemoryStores(t *testing.T) {
	ctx := context.Background()

	t.Run("empty and null resources need no database", func(t *testing.T) {
		for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`)} {
			stores, err := loadDeploymentMemoryStores(ctx, nil, "ws", raw)
			if err != nil || stores != nil {
				t.Fatalf("loadDeploymentMemoryStores(%s) = (%v, %v)", raw, stores, err)
			}
		}
	})

	t.Run("rejects invalid stored resources", func(t *testing.T) {
		_, err := loadDeploymentMemoryStores(ctx, nil, "ws", json.RawMessage(`{"type":"memory_store"}`))
		if err == nil || !strings.Contains(err.Error(), "stored resources are invalid") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("skips non-memory resources without loading", func(t *testing.T) {
		stores, err := loadDeploymentMemoryStores(
			ctx,
			nil,
			"ws",
			json.RawMessage(`[{"type":"file","file_id":"file_1"},{"type":"github_repository","url":"https://github.com/example/repo.git"}]`),
		)
		if err != nil {
			t.Fatalf("loadDeploymentMemoryStores() error = %v", err)
		}
		if len(stores) != 0 {
			t.Fatalf("stores = %#v, want empty", stores)
		}
	})
}

func TestMemoryStoreLoadFailure(t *testing.T) {
	notFound := memoryStoreLoadFailure(db.ErrNotFound)
	if notFound == nil || notFound.Type != "session_resource_not_found_error" {
		t.Fatalf("not found = %+v", notFound)
	}
	archived := memoryStoreLoadFailure(db.ErrInvalidState)
	if archived == nil || archived.Type != "memory_store_archived_error" {
		t.Fatalf("archived = %+v", archived)
	}
	if failure := memoryStoreLoadFailure(errors.New("database unavailable")); failure != nil {
		t.Fatalf("unexpected failure = %+v", failure)
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

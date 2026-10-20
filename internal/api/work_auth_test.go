package api

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
	"time"
)

func TestWorkCredentialLeastPrivilege(t *testing.T) {
	work := db.EnvironmentWork{ExternalID: "work_owned", EnvironmentExternalID: "env_owned", SessionExternalID: "sesn_owned"}
	resources := []db.SessionResource{
		{ResourceType: db.SessionResourceTypeMemoryStore, Payload: json.RawMessage(`{"memory_store_id":"mem_read","access":"read_only"}`)},
		{ResourceType: db.SessionResourceTypeMemoryStore, Payload: json.RawMessage(`{"memory_store_id":"mem_write","access":"read_write"}`)},
	}
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/v1/sessions/sesn_owned", true}, {"GET", "/v1/sessions/sesn_other", false},
		{"GET", "/v1/sessions/sesn_owned/events/stream", true}, {"POST", "/v1/sessions/sesn_owned/events", true},
		{"DELETE", "/v1/sessions/sesn_owned", false}, {"POST", "/v1/sessions/sesn_owned/resources", false},
		{"POST", "/v1/environments/env_owned/work/work_owned/heartbeat", true},
		{"POST", "/v1/environments/env_owned/work/work_owned/stop", true},
		{"POST", "/v1/environments/env_owned/work/work_other/stop", false},
		{"GET", "/v1/environments/env_owned/work", false},
		{"GET", "/v1/memory_stores/mem_read/memories", true},
		{"GET", "/v1/memory_stores/mem_read/memories/id/versions", true},
		{"POST", "/v1/memory_stores/mem_read/memories", false},
		{"PATCH", "/v1/memory_stores/mem_read/memories/id", false},
		{"DELETE", "/v1/memory_stores/mem_read/memories/id", false},
		{"PATCH", "/v1/memory_stores/mem_write/memories/id", true},
		{"POST", "/v1/memory_stores/mem_write/memories", true},
		{"DELETE", "/v1/memory_stores/mem_write/memories/id", true},
		{"DELETE", "/v1/memory_stores/mem_write", false},
		{"GET", "/v1/memory_stores/mem_other/memories", false},
		{"GET", "/v1/memory_stores/mem_read_extra/memories", false},
		{"GET", "/v1/vaults", false}, {"GET", "/v1/files", false}, {"POST", "/v1/messages", false},
	} {
		if got := workCredentialAllows(tc.method, tc.path, work, resources); got != tc.want {
			t.Errorf("%s %s = %v", tc.method, tc.path, got)
		}
	}
	deleted := time.Now()
	resources[0].DeletedAt = &deleted
	if workCredentialAllows("GET", "/v1/memory_stores/mem_read/memories", work, resources) {
		t.Fatal("detached store allowed")
	}
}

func TestWorkCredentialRevocation(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	ttl := 30
	work := db.EnvironmentWork{State: "starting", ClaimExpiresAt: &future}
	if !workCredentialActive(work, now) {
		t.Fatal("live claim rejected")
	}
	work.ClaimExpiresAt = &past
	if workCredentialActive(work, now) {
		t.Fatal("expired claim accepted")
	}
	work.AcknowledgedAt = &now
	if !workCredentialActive(work, now) || workCredentialActive(work, now.Add(61*time.Second)) {
		t.Fatal("ack bootstrap lifetime failed")
	}
	work.AcknowledgedAt = nil
	work.State = "active"
	work.LatestHeartbeatAt = &now
	work.HeartbeatTTLSeconds = &ttl
	if !workCredentialActive(work, now) || workCredentialActive(work, now.Add(time.Minute)) {
		t.Fatal("heartbeat lifetime failed")
	}
	for _, state := range []string{"queued", "stopping", "stopped", "failed"} {
		work.State = state
		if workCredentialActive(work, now) {
			t.Fatal("terminal work accepted")
		}
	}
	work.State = "active"
	work.StopRequestedAt = &now
	if workCredentialActive(work, now) {
		t.Fatal("stop did not revoke")
	}
}

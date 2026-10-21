package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	testOrganization = "00000000-0000-0000-0000-000000000001"
	testWorkspace    = "00000000-0000-0000-0000-000000000002"
)

type fakeStore struct {
	session db.CodeSession
	findErr error
	merges  []json.RawMessage
	scope   []string
}

func (s *fakeStore) GetCodeSessionBySessionExternalID(_ context.Context, workspaceUUID, sessionExternalID string) (db.CodeSession, error) {
	if s.findErr != nil {
		return db.CodeSession{}, s.findErr
	}
	if workspaceUUID != s.session.WorkspaceUUID || sessionExternalID != s.session.SessionExternalID {
		return db.CodeSession{}, db.ErrNotFound
	}
	return s.session, nil
}

func (s *fakeStore) MergeCodeSessionWorkerExternalMetadata(_ context.Context, organizationUUID, workspaceUUID, codeSessionExternalID string, patch json.RawMessage) error {
	s.scope = []string{organizationUUID, workspaceUUID, codeSessionExternalID}
	s.merges = append(s.merges, patch)
	return nil
}

func blockedStore() *fakeStore {
	return &fakeStore{session: db.CodeSession{
		ExternalID: "cse_blocked", OrganizationUUID: testOrganization, WorkspaceUUID: testWorkspace, SessionExternalID: "sesn_blocked",
		WorkerExternalMetadata: json.RawMessage(`{"model":"m","unknown_tool_outcomes":[{"tool_use_id":"toolu_a","tool_name":"Bash"},{"tool_use_id":"toolu_b","tool_name":"Write","session_thread_id":"sthr_child"}]}`),
	}}
}

func request(acknowledge ...string) reconcileRequest {
	return reconcileRequest{
		OrganizationUUID: testOrganization, WorkspaceUUID: testWorkspace, SessionExternalID: "sesn_blocked",
		Acknowledge: acknowledge, Now: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC),
	}
}

func TestParseRequestRejectsInvalidScope(t *testing.T) {
	for _, args := range [][]string{
		{"-organization", "not-a-uuid", "-workspace", testWorkspace, "-session", "sesn_x"},
		{"-organization", testOrganization, "-workspace", "", "-session", "sesn_x"},
		{"-organization", testOrganization, "-workspace", testWorkspace},
	} {
		if _, err := parseRequest(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestReconcileRejectsForeignOrganizationSession(t *testing.T) {
	store := blockedStore()
	store.session.OrganizationUUID = "00000000-0000-0000-0000-000000000009"

	err := reconcile(context.Background(), store, request("toolu_a"), &bytes.Buffer{})

	if !errors.Is(err, db.ErrNotFound) || len(store.merges) != 0 {
		t.Fatalf("err=%v merges=%d", err, len(store.merges))
	}
}

func TestReconcileRejectsToolUseIDsThatAreNotUnknownOutcomes(t *testing.T) {
	store := blockedStore()

	err := reconcile(context.Background(), store, request("toolu_a", "toolu_missing"), &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "toolu_missing") || len(store.merges) != 0 {
		t.Fatalf("err=%v merges=%d", err, len(store.merges))
	}
}

func TestReconcileRejectsUnappliedRequestForAnotherThread(t *testing.T) {
	store := blockedStore()
	store.session.WorkerExternalMetadata = json.RawMessage(`{"unknown_tool_outcomes":[{"tool_use_id":"toolu_a"},{"tool_use_id":"toolu_b","session_thread_id":"sthr_child"}],"tool_outcome_reconciliation":{"tool_use_ids":["toolu_b"],"requested_at":"2026-10-21T07:00:00Z","session_thread_id":"sthr_child"}}`)

	err := reconcile(context.Background(), store, request("toolu_a"), &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "2026-10-21T07:00:00Z") || len(store.merges) != 0 {
		t.Fatalf("err=%v merges=%d", err, len(store.merges))
	}
}

func TestReconcileMergesUnappliedRequestForTheSameThread(t *testing.T) {
	store := blockedStore()
	store.session.WorkerExternalMetadata = json.RawMessage(`{"unknown_tool_outcomes":[{"tool_use_id":"toolu_a"},{"tool_use_id":"toolu_b"}],"tool_outcome_reconciliation":{"tool_use_ids":["toolu_a"],"requested_at":"2026-10-21T07:00:00Z"}}`)

	if err := reconcile(context.Background(), store, request("toolu_b", "toolu_a"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	want := `{"tool_outcome_reconciliation":{"tool_use_ids":["toolu_a","toolu_b"],"requested_at":"2026-10-21T08:00:00Z"}}`
	if len(store.merges) != 1 || string(store.merges[0]) != want {
		t.Fatalf("merges = %s", store.merges)
	}
}

func TestReconcileListsUnknownOutcomesWithoutWriting(t *testing.T) {
	store := blockedStore()
	var output bytes.Buffer

	if err := reconcile(context.Background(), store, request(), &output); err != nil {
		t.Fatal(err)
	}

	var report reconcileReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(store.merges) != 0 || report.CodeSessionID != "cse_blocked" || len(report.UnknownOutcomes) != 2 || report.UnknownOutcomes[1].SessionThreadID != "sthr_child" || report.PendingReconciliation != nil {
		t.Fatalf("merges=%d report=%+v", len(store.merges), report)
	}
}

func TestReconcileRecordsAcknowledgedToolUseIDs(t *testing.T) {
	store := blockedStore()
	var output bytes.Buffer

	if err := reconcile(context.Background(), store, request("toolu_b"), &output); err != nil {
		t.Fatal(err)
	}

	if len(store.merges) != 1 || strings.Join(store.scope, ",") != testOrganization+","+testWorkspace+",cse_blocked" {
		t.Fatalf("merges=%d scope=%v", len(store.merges), store.scope)
	}
	want := `{"tool_outcome_reconciliation":{"tool_use_ids":["toolu_b"],"requested_at":"2026-10-21T08:00:00Z"}}`
	if string(store.merges[0]) != want {
		t.Fatalf("patch = %s", store.merges[0])
	}
	if !strings.Contains(output.String(), `"tool_use_ids": [`) {
		t.Fatalf("output = %s", output.String())
	}
}

func TestParseRequestDeduplicatesAcknowledgedIDs(t *testing.T) {
	parsed, err := parseRequest([]string{"-organization", testOrganization, "-workspace", testWorkspace, "-session", " sesn_x ", "-acknowledge", "toolu_a, toolu_b,,toolu_a"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SessionExternalID != "sesn_x" || strings.Join(parsed.Acknowledge, ",") != "toolu_a,toolu_b" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestReconcileRejectsAmbiguousToolUseIDs(t *testing.T) {
	store := blockedStore()
	store.session.WorkerExternalMetadata = json.RawMessage(`{"unknown_tool_outcomes":[{"tool_use_id":"shared","session_thread_id":"sthr_alpha"},{"tool_use_id":"shared","session_thread_id":"sthr_beta"}]}`)
	err := reconcile(context.Background(), store, request("shared"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || len(store.merges) != 0 {
		t.Fatalf("ambiguous acknowledgement accepted: error=%v merges=%d", err, len(store.merges))
	}
}

func TestReconcileSelectsOneThreadForDuplicateToolUseIDs(t *testing.T) {
	store := blockedStore()
	store.session.WorkerExternalMetadata = json.RawMessage(`{"unknown_tool_outcomes":[{"tool_use_id":"shared","session_thread_id":"sthr_alpha"},{"tool_use_id":"shared","session_thread_id":"sthr_beta"}]}`)
	parsed, err := parseRequest([]string{"-organization", testOrganization, "-workspace", testWorkspace, "-session", "sesn_blocked", "-acknowledge", "shared", "-thread", "sthr_alpha"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcile(context.Background(), store, parsed, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(store.merges) != 1 || !strings.Contains(string(store.merges[0]), `"session_thread_id":"sthr_alpha"`) {
		t.Fatalf("missing thread scope: %s", store.merges)
	}
}

func TestReconcileRejectsToolUseIDFromAnotherThread(t *testing.T) {
	store := blockedStore()
	parsed, err := parseRequest([]string{"-organization", testOrganization, "-workspace", testWorkspace, "-session", "sesn_blocked", "-acknowledge", "toolu_b", "-thread", "primary"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcile(context.Background(), store, parsed, &bytes.Buffer{}); err == nil || len(store.merges) != 0 {
		t.Fatalf("foreign thread acknowledged: error=%v merges=%d", err, len(store.merges))
	}
}

func TestReconcileSelectsPrimaryThread(t *testing.T) {
	store := blockedStore()
	parsed, err := parseRequest([]string{"-organization", testOrganization, "-workspace", testWorkspace, "-session", "sesn_blocked", "-acknowledge", "toolu_a", "-thread", "primary"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcile(context.Background(), store, parsed, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(store.merges) != 1 || !strings.Contains(string(store.merges[0]), `"session_thread_id":""`) {
		t.Fatalf("missing primary scope: %s", store.merges)
	}
}

func TestParseRequestRejectsBlankThread(t *testing.T) {
	_, err := parseRequest([]string{"-organization", testOrganization, "-workspace", testWorkspace, "-session", "sesn_blocked", "-thread", " "}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("blank thread accepted")
	}
}

//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestMergeCodeSessionWorkerExternalMetadataPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	codeSessionID := seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	execClaimTest(t, ctx, database, `update code_sessions set worker_external_metadata = '{"model":"m","unknown_tool_outcomes":[{"tool_use_id":"toolu_a"}]}' where external_id = $1`, codeSessionID)
	patch := json.RawMessage(`{"tool_outcome_reconciliation":{"tool_use_ids":["toolu_a"]}}`)

	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, "53000000-0000-0000-0000-000000000009", claimTestWorkspace, codeSessionID, patch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign organization err = %v", err)
	}
	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, claimTestOrganization, "53000000-0000-0000-0000-000000000009", codeSessionID, patch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign workspace err = %v", err)
	}
	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, claimTestOrganization, claimTestWorkspace, "cse_missing", patch); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing code session err = %v", err)
	}
	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, json.RawMessage(`[]`)); err == nil {
		t.Fatal("non-object patch accepted")
	}

	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, patch); err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	var raw []byte
	if err := database.QueryRowContext(ctx, `select worker_external_metadata from code_sessions where external_id = $1`, codeSessionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &merged); err != nil {
		t.Fatal(err)
	}
	if merged["model"] != "m" || merged["unknown_tool_outcomes"] == nil || merged["tool_outcome_reconciliation"] == nil {
		t.Fatalf("merged metadata = %s", raw)
	}

	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, json.RawMessage(`{"tool_outcome_reconciliation":null}`)); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `select worker_external_metadata from code_sessions where external_id = $1`, codeSessionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"model": "m", "unknown_tool_outcomes": [{"tool_use_id": "toolu_a"}]}` {
		t.Fatalf("cleared metadata = %s", raw)
	}

	execClaimTest(t, ctx, database, `update code_sessions set status = 'ended' where external_id = $1`, codeSessionID)
	if err := store.MergeCodeSessionWorkerExternalMetadata(ctx, claimTestOrganization, claimTestWorkspace, codeSessionID, patch); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("inactive code session err = %v", err)
	}
}

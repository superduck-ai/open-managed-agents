package tests

import (
	"bytes"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestVerifyTranscriptBoundary(t *testing.T) {
	f := newTranscriptVerification(t, 1, true)
	agent := "agent_boundary"
	seedArchiveEvents(t, f.app, f.session, []db.AppendCodeSessionInternalEventInput{
		{AgentID: &agent}, {IsCompaction: true}, {AgentID: &agent, IsCompaction: true}, {}, {AgentID: &agent},
	})
	f.before = f.export(t)
	if _, err := f.app.pool.Exec(t.Context(), "update sessions set archived_at=null,status='running' where uuid=$1", f.session.SessionUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.pool.Exec(t.Context(), "update code_sessions set worker_status='running',worker_lease_expires_at=now()+interval '1 hour' where uuid=$1", f.session.UUID); err != nil {
		t.Fatal(err)
	}
	foreground := transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events")
	subagent := transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events?subagents=true")
	if err := f.service.Archive(t.Context(), f.scope, false); err != nil {
		t.Fatal(err)
	}
	f.rows(t, 6, 4)
	archives, err := f.app.db.ListTranscriptArchives(t.Context(), f.scope, 0, 100, true)
	if err != nil || len(archives) == 0 {
		t.Fatalf("interleaved scopes produced no archives: %v", err)
	}
	seen := make(map[int64]bool)
	for _, archive := range archives {
		records, err := f.service.ReadSegment(t.Context(), archive)
		if err != nil {
			t.Fatal(err)
		}
		for sequence, record := range records {
			if seen[sequence] || sequence < 1 || sequence > 2 || (sequence == 1 && record.AgentID != nil) || (sequence == 2 && (record.AgentID == nil || *record.AgentID != agent)) {
				t.Fatal("archive duplicated events or crossed a scope's compaction boundary")
			}
			seen[sequence] = true
		}
	}
	if len(seen) != 2 {
		t.Fatal("archive omitted an eligible foreground or subagent event")
	}
	assertReads := func() {
		if !bytes.Equal(foreground, transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events")) || !bytes.Equal(subagent, transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events?subagents=true")) {
			t.Fatal("archive changed visible compaction boundary or HTTP bytes")
		}
		f.assertExport(t)
	}
	assertReads()
	transcriptVerifyProof(t, f.started, "scope_boundaries_preserved")
	f.ageDeleted(t)
	f.hardDelete(t)
	f.rows(t, 4, 4)
	assertReads()
	transcriptVerifyProof(t, f.started, "boundary_delete_preserves_reads")
	for range 2 {
		if err := f.service.Restore(t.Context(), f.scope); err != nil {
			t.Fatal(err)
		}
	}
	f.rows(t, 6, 6)
	assertReads()
	transcriptVerifyProof(t, f.started, "boundary_restore_matches")
}

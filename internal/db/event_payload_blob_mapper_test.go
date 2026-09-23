package db

import (
	"github.com/superduck-ai/yourbatis"
	"testing"
)

func TestEventPayloadBlobMapperBindings(t *testing.T) {
	blob := EventPayloadBlob{UUID: "blob", WorkspaceUUID: "workspace", SHA256: "hash"}
	for _, contract := range []mapperBuilderContract{
		{statement: eventPayloadBlobMapperInsertStatement, bound: buildEventPayloadBlobMapperInsert(yourbatis.DialectPostgres, blob), wantID: "EventPayloadBlobMapper.Insert", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"blob.UUID", "blob.ExternalID", "blob.OrganizationUUID", "blob.WorkspaceUUID", "blob.Bucket", "blob.Key", "blob.Size", "blob.SHA256"}, wantSensitiveArgumentNames: []string{"blob.SHA256"}, wantSQLFragments: []string{"INSERT INTO event_payload_blobs"}},
		{statement: eventPayloadBlobMapperAttachStatement, bound: buildEventPayloadBlobMapperAttach(yourbatis.DialectPostgres, "workspace", "blob"), wantID: "EventPayloadBlobMapper.Attach", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"workspaceUUID", "blobUUID"}, wantSQLFragments: []string{"state = 'pending'", "workspace_uuid = $1", "uuid = $2"}},
		{statement: eventPayloadBlobMapperClaimCleanupStatement, bound: buildEventPayloadBlobMapperClaimCleanup(yourbatis.DialectPostgres, 10), wantID: "EventPayloadBlobMapper.ClaimCleanup", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"limit"}, wantSQLFragments: []string{"FOR UPDATE OF b SKIP LOCKED", "b.state IN ('pending', 'attached')", "code_session_internal_events", "session_events", "state = 'deleting'"}},
	} {
		t.Run(contract.wantID, func(t *testing.T) { assertMapperBuilderContract(t, contract) })
	}
}

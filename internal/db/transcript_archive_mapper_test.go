package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestTranscriptArchiveMapperBindings(t *testing.T) {
	a := TranscriptArchive{}
	scope := TranscriptScope{}
	for _, contract := range []mapperBuilderContract{
		{statement: transcriptArchiveMapperInsertStatement, bound: buildTranscriptArchiveMapperInsert(yourbatis.DialectPostgres, a), wantID: "TranscriptArchiveMapper.Insert", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"archive.UUID", "archive.ExternalID", "archive.OrganizationUUID", "archive.WorkspaceUUID", "archive.CodeSessionUUID", "archive.CodeSessionExternalID", "archive.FromSequence", "archive.ToSequence", "archive.EventCount", "archive.Codec", "archive.Bucket", "archive.Key", "archive.Size", "archive.RawBytes", "archive.SHA256"}, wantSensitiveArgumentNames: []string{"archive.SHA256"}, wantSQLFragments: []string{"INSERT INTO transcript_archives"}},
		{statement: transcriptArchiveMapperAttachStatement, bound: buildTranscriptArchiveMapperAttach(yourbatis.DialectPostgres, scope, "archive"), wantID: "TranscriptArchiveMapper.Attach", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "archiveUUID"}, wantSQLFragments: []string{"state = 'pending'", "interval '24 hours'"}},
		{statement: transcriptArchiveMapperClaimCleanupStatement, bound: buildTranscriptArchiveMapperClaimCleanup(yourbatis.DialectPostgres, 10), wantID: "TranscriptArchiveMapper.ClaimCleanup", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"limit"}, wantSQLFragments: []string{"FOR UPDATE SKIP LOCKED", "state = 'pending'", "state = 'deleting'"}},
	} {
		t.Run(contract.wantID, func(t *testing.T) { assertMapperBuilderContract(t, contract) })
	}
}

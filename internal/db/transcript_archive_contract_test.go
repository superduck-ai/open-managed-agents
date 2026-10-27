package db

import (
	"context"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestTranscriptArchiveQueryBindings(t *testing.T) {
	scope := TranscriptScope{}
	scopeNames := []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}
	for _, contract := range []mapperBuilderContract{
		{statement: transcriptArchiveMapperLockScopeStatement, bound: buildTranscriptArchiveMapperLockScope(yourbatis.DialectPostgres, "transcript/scope"), wantID: "TranscriptArchiveMapper.LockScope", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"key"}, wantSQLFragments: []string{"pg_advisory_xact_lock"}},
		{statement: transcriptArchiveMapperOverlapsStatement, bound: buildTranscriptArchiveMapperOverlaps(yourbatis.DialectPostgres, TranscriptArchive{}), wantID: "TranscriptArchiveMapper.Overlaps", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"archive.OrganizationUUID", "archive.WorkspaceUUID", "archive.CodeSessionUUID", "archive.ToSequence", "archive.FromSequence"}, wantSQLFragments: []string{"state <> 'deleting'"}},
		{statement: transcriptArchiveMapperFindByRangeStatement, bound: buildTranscriptArchiveMapperFindByRange(yourbatis.DialectPostgres, scope, 5), wantID: "TranscriptArchiveMapper.FindByRange", wantKind: yourbatis.StatementSelect, wantArgumentNames: append(append([]string{}, scopeNames...), "fromSequence"), wantSQLFragments: []string{"from_sequence_num = $4"}},
		{statement: transcriptArchiveMapperLockAttachedStatement, bound: buildTranscriptArchiveMapperLockAttached(yourbatis.DialectPostgres, scope, "archive"), wantID: "TranscriptArchiveMapper.LockAttached", wantKind: yourbatis.StatementSelect, wantArgumentNames: append(append([]string{}, scopeNames...), "archiveUUID"), wantSQLFragments: []string{"FOR SHARE", "state = 'attached'"}},
		{statement: transcriptArchiveMapperHasAttachedCoveringStatement, bound: buildTranscriptArchiveMapperHasAttachedCovering(yourbatis.DialectPostgres, scope, "archive", 5), wantID: "TranscriptArchiveMapper.HasAttachedCovering", wantKind: yourbatis.StatementSelect, wantArgumentNames: append(append([]string{}, scopeNames...), "archiveUUID", "sequence"), wantSQLFragments: []string{"BETWEEN from_sequence_num AND to_sequence_num"}},
	} {
		t.Run(contract.wantID, func(t *testing.T) { assertMapperBuilderContract(t, contract) })
	}
	for _, attached := range []bool{false, true} {
		fragments := []string{"ORDER BY from_sequence_num", "LIMIT $5"}
		if attached {
			fragments = append(fragments, "state = 'attached'")
		}
		assertMapperBuilderContract(t, mapperBuilderContract{statement: transcriptArchiveMapperListBySessionStatement, bound: buildTranscriptArchiveMapperListBySession(yourbatis.DialectPostgres, scope, 5, 100, attached), wantID: "TranscriptArchiveMapper.ListBySession", wantKind: yourbatis.StatementSelect, wantArgumentNames: append(append([]string{}, scopeNames...), "after", "limit"), wantSQLFragments: fragments})
	}
}

func TestTranscriptArchiveMapperExecutionErrors(t *testing.T) {
	ctx := context.Background()
	scope := TranscriptScope{}
	for _, contract := range []mapperExecutionErrorContract{
		{statementID: "TranscriptArchiveMapper.Insert", kind: yourbatis.StatementInsert, call: func(ex yourbatis.Executor) error {
			return NewTranscriptArchiveMapper(ex).Insert(ctx, TranscriptArchive{})
		}},
		{statementID: "TranscriptArchiveMapper.Attach", kind: yourbatis.StatementUpdate, call: func(ex yourbatis.Executor) error {
			_, err := NewTranscriptArchiveMapper(ex).Attach(ctx, scope, "archive")
			return err
		}},
		{statementID: "TranscriptArchiveMapper.FindByRange", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, _, err := NewTranscriptArchiveMapper(ex).FindByRange(ctx, scope, 1)
			return err
		}},
		{statementID: "TranscriptArchiveMapper.ListBySession", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewTranscriptArchiveMapper(ex).ListBySession(ctx, scope, 0, 10, true)
			return err
		}},
		{statementID: "TranscriptArchiveMapper.LockAttached", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, _, err := NewTranscriptArchiveMapper(ex).LockAttached(ctx, scope, "archive")
			return err
		}},
		{statementID: "TranscriptArchiveMapper.HasAttachedCovering", kind: yourbatis.StatementSelect, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewTranscriptArchiveMapper(ex).HasAttachedCovering(ctx, scope, "archive", 1)
			return err
		}},
		{statementID: "TranscriptArchiveMapper.ClaimCleanup", kind: yourbatis.StatementUpdate, query: true, call: func(ex yourbatis.Executor) error {
			_, err := NewTranscriptArchiveMapper(ex).ClaimCleanup(ctx, 10)
			return err
		}},
	} {
		t.Run(contract.statementID, func(t *testing.T) { assertMapperExecutionError(t, contract) })
	}
}

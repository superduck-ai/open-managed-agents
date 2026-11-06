package db

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHLockModesAndBindings(t *testing.T) {
	scope := DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code"}
	dialect := yourbatis.DialectPostgres
	for _, tc := range []mapperBuilderContract{
		{statement: dSHTranscriptMapperShareWorkerStatement, bound: buildDSHTranscriptMapperShareWorker(dialect, scope), wantID: "DSHTranscriptMapper.ShareWorker", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"AS MATERIALIZED", "FOR KEY SHARE", "clock_timestamp()", "AS lease_valid"}, wantSQLAbsent: []string{"FOR UPDATE", "FOR SHARE"}},
		{statement: dSHTranscriptMapperLockHeartbeatStatement, bound: buildDSHTranscriptMapperLockHeartbeat(dialect, scope), wantID: "DSHTranscriptMapper.LockHeartbeat", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"AS MATERIALIZED", "FOR NO KEY UPDATE", "clock_timestamp()"}, wantSQLAbsent: []string{"FOR UPDATE"}},
		{statement: dSHTranscriptMapperLockTranscriptStatement, bound: buildDSHTranscriptMapperLockTranscript(dialect, scope, "child"), wantID: "DSHTranscriptMapper.LockTranscript", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "dshSessionID"}, wantSQLFragments: []string{"FROM dsh_transcripts", "dsh_session_id = $4", "FOR UPDATE"}},
	} {
		t.Run(tc.wantID, func(t *testing.T) { assertMapperBuilderContract(t, tc) })
	}
}

func TestOwnershipMutationsAcquireExclusiveFence(t *testing.T) {
	dialect := yourbatis.DialectPostgres
	cases := map[string]yourbatis.BoundSQL{
		"register":               buildCodeSessionMapperRegisterWorker(dialect, registerCodeSessionWorkerParams{}),
		"rotate":                 buildCodeSessionMapperRotateCredentials(dialect, rotateCodeSessionCredentialsParams{}),
		"terminate session":      buildCodeSessionMapperTerminateBySession(dialect, "org", "workspace", "session"),
		"terminate code session": buildCodeSessionMapperTerminateByExternalID(dialect, "org", "workspace", "code"),
		"terminate launch":       buildCodeSessionMapperTerminateLaunch(dialect, "org", "workspace", "code", 7),
		"claim":                  buildCodeSessionClaimMapperClaim(dialect, claimCodeSessionParams{}),
		"hold exhausted":         buildCodeSessionClaimMapperHoldExhausted(dialect, "code", time.Time{}),
		"fail permanently":       buildCodeSessionClaimMapperFailPermanently(dialect, failCodeSessionClaimParams{}),
		"release retry":          buildCodeSessionClaimMapperRelease(dialect, releaseCodeSessionClaimParams{Kind: CodeSessionClaimReleaseRetry}),
		"release handoff":        buildCodeSessionClaimMapperRelease(dialect, releaseCodeSessionClaimParams{Kind: CodeSessionClaimReleaseHandoff}),
		"release idle":           buildCodeSessionClaimMapperRelease(dialect, releaseCodeSessionClaimParams{Kind: CodeSessionClaimReleaseIdle}),
		"sandbox reclaim":        buildSandboxLifecycleMapperClaim(dialect, "code", "sandbox", time.Time{}),
	}
	for name, bound := range cases {
		t.Run(name, func(t *testing.T) {
			alias := "code_sessions"
			if name == "sandbox reclaim" {
				alias = "cs"
			}
			if !strings.Contains(bound.SQL, "ownership.uuid = "+alias+".uuid FOR UPDATE") {
				t.Fatalf("mutation lacks worker fence: %s", bound.SQL)
			}
		})
	}
}

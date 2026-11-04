package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHDeliveryMapperContracts(t *testing.T) {
	scope := DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code"}
	item := DSHDeliveryLease{UUID: "delivery", OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code", LeaseToken: "token"}
	dialect := yourbatis.DialectPostgres
	names := []string{"item.OrganizationUUID", "item.WorkspaceUUID", "item.CodeSessionUUID", "item.UUID", "item.LeaseToken"}
	for _, test := range []mapperBuilderContract{
		{statement: dSHDeliveryMapperPruneDiscardedStatement, bound: buildDSHDeliveryMapperPruneDiscarded(dialect), wantID: "DSHDeliveryMapper.PruneDiscarded", wantKind: yourbatis.StatementDelete, wantArgumentNames: []string{}, wantSQLFragments: []string{"INTERVAL '7 days'", "LIMIT 256"}},
		{statement: dSHDeliveryMapperRecordStatement, bound: buildDSHDeliveryMapperRecord(dialect, scope, "input", "secret", ""), wantID: "DSHDeliveryMapper.Record", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"ackSubject", "scope.WorkspaceUUID", "cleanupJobID", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "inputID"}, wantSensitiveArgumentNames: []string{"ackSubject"}, wantSQLFragments: []string{"ON CONFLICT", "ack_subject IS DISTINCT FROM EXCLUDED.ack_subject"}},
		{statement: dSHDeliveryMapperFindStatement, bound: buildDSHDeliveryMapperFind(dialect, scope, "input"), wantID: "DSHDeliveryMapper.Find", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "inputID"}},
		{statement: dSHDeliveryMapperReadyStatement, bound: buildDSHDeliveryMapperReady(dialect, scope, "input"), wantID: "DSHDeliveryMapper.Ready", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "inputID"}, wantSQLFragments: []string{"status = 'waiting'"}},
		{statement: dSHDeliveryMapperLeaseStatement, bound: buildDSHDeliveryMapperLease(dialect), wantID: "DSHDeliveryMapper.Lease", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{}, wantSQLFragments: []string{"FOR UPDATE SKIP LOCKED", "INTERVAL '30 seconds'", "LIMIT 16"}},
		{statement: dSHDeliveryMapperDeleteAcknowledgedStatement, bound: buildDSHDeliveryMapperDeleteAcknowledged(dialect, item), wantID: "DSHDeliveryMapper.DeleteAcknowledged", wantKind: yourbatis.StatementDelete, wantArgumentNames: names, wantSQLFragments: []string{"status = 'pending'", "lease_token = $5"}},
		{statement: dSHDeliveryMapperRetryStatement, bound: buildDSHDeliveryMapperRetry(dialect, item), wantID: "DSHDeliveryMapper.Retry", wantKind: yourbatis.StatementUpdate, wantArgumentNames: names, wantSQLFragments: []string{"attempts = attempts + 1", "LEAST(60"}},
		{statement: dSHDeliveryMapperExpediteCleanupStatement, bound: buildDSHDeliveryMapperExpediteCleanup(dialect, item), wantID: "DSHDeliveryMapper.ExpediteCleanup", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"item.WorkspaceUUID", "item.CleanupJobUUID"}},
		{statement: dSHDeliveryMapperDiscardRetiredStatement, bound: buildDSHDeliveryMapperDiscardRetired(dialect), wantID: "DSHDeliveryMapper.DiscardRetired", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{}, wantSQLFragments: []string{"status = 'discarded'", "c.organization_uuid = d.organization_uuid"}},
	} {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
}

package db

import (
	"database/sql/driver"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestChannelArtifactCheckpointMapper(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"input_event_id"}, rows: [][]driver.Value{{"input"}}})
	scope := ChannelDeliveryScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", ChannelExternalID: "channel"}
	_, found, err := NewChannelRecordMapper(executor).SaveArtifactDelivery(t.Context(), scope, "input", "owner", `{"files":[]}`)
	if err != nil || !found {
		t.Fatalf("保存产物交接: found=%v err=%v", found, err)
	}
	assertMapperBuilderContract(t, mapperBuilderContract{statement: executor.statement, bound: executor.bound,
		wantID: "ChannelRecordMapper.SaveArtifactDelivery", wantKind: yourbatis.StatementSelect,
		wantArgumentNames:          []string{"receipt", "scope.ChannelExternalID", "scope.OrganizationUUID", "scope.WorkspaceUUID", "inputID", "owner"},
		wantSensitiveArgumentNames: []string{"receipt"},
		wantSQLFragments:           []string{"artifact_delivery = CAST($1 AS jsonb)", "d.lease_owner = $6", "d.state = 'sent'", "d.lease_until > now()"}})
}

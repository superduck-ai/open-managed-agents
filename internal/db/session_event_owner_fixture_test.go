//go:build integration

package db

import (
	"context"
	"database/sql"
	"testing"
)

func seedSessionEventOwner(t *testing.T, ctx context.Context, database *sql.DB, organizationUUID, workspaceUUID, sessionUUID, sessionExternalID string) {
	t.Helper()
	execClaimTest(t, ctx, database, `INSERT INTO sessions(uuid,external_id,environment_external_id,agent_external_id,agent_version,agent_snapshot,organization_uuid,workspace_uuid,environment_uuid,agent_uuid)
		VALUES($1,$2,'env','agent',1,'{}',$3,$4,gen_random_uuid(),gen_random_uuid())`, sessionUUID, sessionExternalID, organizationUUID, workspaceUUID)
}

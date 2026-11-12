package db

import (
	"github.com/superduck-ai/yourbatis"
	"testing"
)

func TestDSHPayloadBlobPublicationMapperScope(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: eventPayloadBlobMapperFindForPublicationStatement,
		bound:     buildEventPayloadBlobMapperFindForPublication(yourbatis.DialectPostgres, "organization", "workspace", "blob"),
		wantID:    "EventPayloadBlobMapper.FindForPublication", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "blobUUID"},
		wantSQLFragments:  []string{"organization_uuid = $1", "workspace_uuid = $2", "uuid = $3", "state IN ('pending','attached')"},
	})
}

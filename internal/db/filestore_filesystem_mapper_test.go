package db

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
)

func TestFilestoreFilesystemMapperBuildsProvisionQueries(t *testing.T) {
	codeSessionUUID := "00000000-0000-0000-0000-000000000004"
	apiKeyUUID := "00000000-0000-0000-0000-000000000005"
	params := filestoreFilesystemProvisionParameters(ProvisionFilestoreFilesystemInput{
		UUID:                "00000000-0000-0000-0000-000000000001",
		ExternalID:          "claude_chat_mapper",
		OrganizationUUID:    "00000000-0000-0000-0000-000000000002",
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000003",
		SessionUUID:         "00000000-0000-0000-0000-000000000006",
		CodeSessionUUID:     &codeSessionUUID,
		CreatedByAPIKeyUUID: &apiKeyUUID,
		Now:                 time.Date(2026, time.July, 23, 17, 0, 0, 0, time.UTC),
	})
	filesystemUUID := tryParseDBUUIDIdentifier(*params.FilesystemUUID)
	tests := []struct {
		name         string
		bound        yourbatis.BoundSQL
		wantArgCount int
	}{
		{
			name:         "provision advisory lock",
			bound:        buildFilestoreFilesystemMapperLockProvision(yourbatis.DialectPostgres, params.WorkspaceUUID, params.FilesystemExternalID),
			wantArgCount: 2,
		},
		{
			name:         "session binding",
			bound:        buildFilestoreFilesystemMapperValidateSessionBinding(yourbatis.DialectPostgres, params),
			wantArgCount: 7,
		},
		{
			name:         "workspace lock",
			bound:        buildFilestoreFilesystemMapperLockWorkspace(yourbatis.DialectPostgres, params.WorkspaceUUID),
			wantArgCount: 1,
		},
		{
			name:         "lookup by external ID",
			bound:        buildFilestoreFilesystemMapperFindProvisionedByIdentifier(yourbatis.DialectPostgres, params.WorkspaceUUID, params.FilesystemExternalID, filesystemUUID),
			wantArgCount: 4,
		},
		{
			name:         "lookup by Session",
			bound:        buildFilestoreFilesystemMapperFindProvisionedBySession(yourbatis.DialectPostgres, params.WorkspaceUUID, params.SessionUUID),
			wantArgCount: 2,
		},
		{
			name:         "insert filesystem",
			bound:        buildFilestoreFilesystemMapperInsertProvisioned(yourbatis.DialectPostgres, params),
			wantArgCount: 9,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(test.bound.SQL, "#{") {
				t.Fatalf("query retains mapper placeholder: %q", test.bound.SQL)
			}
			if strings.Contains(test.bound.SQL, "::") {
				t.Fatalf("query uses PostgreSQL colon cast syntax: %q", test.bound.SQL)
			}
			if len(test.bound.Args) != test.wantArgCount {
				t.Fatalf("bound argument count = %d, want %d", len(test.bound.Args), test.wantArgCount)
			}
		})
	}
}

func TestFilestoreFilesystemMapperBuildsLifecycleQueries(t *testing.T) {
	workspaceUUID := "00000000-0000-0000-0000-000000000003"
	filesystemUUID := "00000000-0000-0000-0000-000000000001"
	createdAt := time.Date(2026, time.July, 23, 17, 0, 0, 0, time.UTC)
	insertParams := sessionFilesystemInsertParams{
		FilesystemExternalID: "claude_chat_mapper",
		SessionUUID:          "00000000-0000-0000-0000-000000000006",
		OrganizationUUID:     "00000000-0000-0000-0000-000000000002",
		WorkspaceUUID:        workspaceUUID,
		CreatedByAPIKeyUUID:  "00000000-0000-0000-0000-000000000005",
		CreatedAt:            createdAt,
	}
	tests := []struct {
		name         string
		bound        yourbatis.BoundSQL
		wantArgCount int
	}{
		{
			name:         "namespace lock",
			bound:        buildFilestoreFilesystemMapperLockFilesystem(yourbatis.DialectPostgres, filesystemUUID),
			wantArgCount: 1,
		},
		{
			name:         "retire session filesystem",
			bound:        buildFilestoreFilesystemMapperRetireSessionFilesystem(yourbatis.DialectPostgres, workspaceUUID, insertParams.OrganizationUUID, insertParams.SessionUUID, createdAt),
			wantArgCount: 5,
		},
		{
			name:         "insert session filesystem",
			bound:        buildFilestoreFilesystemMapperInsertSessionFilesystem(yourbatis.DialectPostgres, insertParams),
			wantArgCount: 7,
		},
		{
			name:         "detect external ID conflict",
			bound:        buildFilestoreFilesystemMapperSessionFilesystemExternalIDExists(yourbatis.DialectPostgres, workspaceUUID, insertParams.FilesystemExternalID),
			wantArgCount: 2,
		},
		{
			name: "session token scope",
			bound: buildFilestoreFilesystemMapperFindSessionTokenScope(
				yourbatis.DialectPostgres,
				workspaceUUID,
				"session_test",
			),
			wantArgCount: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(test.bound.SQL, "#{") || strings.Contains(test.bound.SQL, "::") {
				t.Fatalf("invalid generated query: %q", test.bound.SQL)
			}
			if len(test.bound.Args) != test.wantArgCount {
				t.Fatalf("bound argument count = %d, want %d", len(test.bound.Args), test.wantArgCount)
			}
		})
	}

	withoutUUID := buildFilestoreFilesystemMapperFindFilesystemByIdentifier(
		yourbatis.DialectPostgres,
		workspaceUUID,
		"claude_chat_mapper",
		uuid.NullUUID{},
	)
	if strings.Contains(withoutUUID.SQL, "OR uuid =") || len(withoutUUID.Args) != 3 {
		t.Fatalf("external-ID-only lookup = %#v", withoutUUID)
	}
}

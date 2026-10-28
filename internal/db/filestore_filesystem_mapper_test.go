package db

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

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
	filesystemUUID := tryParseDBUUIDIdentifierString(*params.FilesystemUUID)
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
		CreatedByAPIKeyUUID:  nullableString("00000000-0000-0000-0000-000000000005"),
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
		"",
	)
	if strings.Contains(withoutUUID.SQL, "OR uuid =") || len(withoutUUID.Args) != 3 {
		t.Fatalf("external-ID-only lookup = %#v", withoutUUID)
	}
}

func TestFilestoreRuntimeIdentitySQL(t *testing.T) {
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: filestoreFilesystemMapperFindTokenScopeStatement,
		bound:     buildFilestoreFilesystemMapperFindTokenScope(yourbatis.DialectPostgres, "org", "account", "workspace", "tagged", "tagged", "filesystem", ""),
		wantID:    "FilestoreFilesystemMapper.FindTokenScope", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"organizationUUID", "accountUUID", "workspaceUUID", "workspaceTaggedID", "resolvedWorkspaceTaggedID", "filesystemID", "filesystemID"},
		wantSQLFragments:  []string{"u.deleted_at IS NULL", "u.organization_uuid = o.uuid OR u.uuid = CASE WHEN s.metadata->>'_oma_runtime_user_uuid' ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN CAST(s.metadata->>'_oma_runtime_user_uuid' AS uuid) END", "s.uuid = fs.session_uuid", "s.organization_uuid = o.uuid", "s.workspace_uuid = w.uuid"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: filestoreFilesystemMapperFindSessionTokenScopeStatement,
		bound:     buildFilestoreFilesystemMapperFindSessionTokenScope(yourbatis.DialectPostgres, "workspace", "session"),
		wantID:    "FilestoreFilesystemMapper.FindSessionTokenScope", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"workspaceUUID", "sessionExternalID"},
		wantSQLFragments:  []string{"s.metadata->>'_oma_runtime_user_uuid' IS NOT NULL OR u.organization_uuid = o.uuid", "u.deleted_at IS NULL", "fs.session_uuid = s.uuid", "CASE WHEN s.metadata->>'_oma_runtime_user_uuid' IS NULL THEN ak.created_by_user_uuid ELSE CASE WHEN s.metadata->>'_oma_runtime_user_uuid' ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN CAST(s.metadata->>'_oma_runtime_user_uuid' AS uuid) END END"},
	})
}

func TestRuntimeIdentityMapperUUIDGuard(t *testing.T) {
	queries := []yourbatis.BoundSQL{
		buildFilestoreFilesystemMapperFindTokenScope(yourbatis.DialectPostgres, "org", "account", "workspace", "tagged", "tagged", "filesystem", ""),
		buildFilestoreFilesystemMapperFindSessionTokenScope(yourbatis.DialectPostgres, "workspace", "session"),
		buildCodeSessionMapperFindCredentialForIssue(yourbatis.DialectPostgres, "org", "workspace", "code"),
	}
	for index, query := range queries {
		pattern := regexp.MustCompile(`CASE WHEN s.metadata->>'_oma_runtime_user_uuid' ~ '([^']+)' THEN CAST\(s.metadata->>'_oma_runtime_user_uuid' AS uuid\) END`).FindStringSubmatch(query.SQL)
		if len(pattern) != 2 {
			t.Fatalf("查询 %d 缺少安全转换: %s", index, query.SQL)
		}
		guard := regexp.MustCompile(pattern[1])
		for _, tc := range []struct {
			metadata string
			valid    bool
		}{
			{`{}`, false},
			{`{"_oma_runtime_user_uuid":""}`, false},
			{`{"_oma_runtime_user_uuid":"not-a-uuid"}`, false},
			{`{"_oma_runtime_user_uuid":"63000000-0000-4000-8000-000000000004"}`, true},
		} {
			var metadata struct {
				RuntimeUserUUID string `json:"_oma_runtime_user_uuid"`
			}
			if err := json.Unmarshal([]byte(tc.metadata), &metadata); err != nil {
				t.Fatal(err)
			}
			if guard.MatchString(metadata.RuntimeUserUUID) != tc.valid {
				t.Fatalf("查询 %d 元数据 %s 校验不符", index, tc.metadata)
			}
		}
	}
}

package db

import (
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestResourceCreatorNullableBindings(t *testing.T) {
	dialect := yourbatis.DialectPostgres
	for _, creator := range []*string{nil, nullableString("00000000-0000-4000-8000-000000000001")} {
		for name, bound := range map[string]yourbatis.BoundSQL{
			"agent":          buildAgentMapperInsert(dialect, insertAgentParams{CreatedByAPIKeyUUID: creator}),
			"file":           buildFileMapperInsertFile(dialect, fileMapperRecordParams{CreatedByAPIKeyUUID: creator}),
			"environment":    buildEnvironmentMapperInsert(dialect, environmentWriteParams{CreatedByAPIKeyUUID: creator}),
			"vault":          buildVaultMapperInsert(dialect, insertVaultParams{CreatedByAPIKeyUUID: creator}),
			"memory":         buildMemoryStoreMapperInsert(dialect, insertMemoryStoreParams{CreatedByAPIKeyUUID: creator}),
			"webhook":        buildWebhookEndpointMapperInsert(dialect, insertWebhookEndpointParams{CreatedByAPIKeyUUID: creator}),
			"batch":          buildMessageBatchMapperInsert(dialect, insertMessageBatchParams{CreatedByAPIKeyUUID: creator}),
			"skill":          buildSkillMapperInsert(dialect, insertSkillParams{CreatedByAPIKeyUUID: creator}),
			"skill version":  buildSkillVersionMapperInsert(dialect, insertSkillVersionParams{CreatedByAPIKeyUUID: creator}),
			"session":        buildSessionMapperInsert(dialect, sessionWriteParams{CreatedByAPIKeyUUID: creator}),
			"deployment":     buildDeploymentMapperInsert(dialect, deploymentWriteParams{CreatedByAPIKeyUUID: creator}),
			"deployment run": buildDeploymentRunMapperInsert(dialect, deploymentRunWriteParams{CreatedByAPIKeyUUID: creator}),
			"filesystem":     buildFilestoreFilesystemMapperInsertSessionFilesystem(dialect, sessionFilesystemInsertParams{CreatedByAPIKeyUUID: creator}),
		} {
			t.Run(name, func(t *testing.T) {
				found := false
				for _, argument := range bound.Args {
					if argument.Name != "params.CreatedByAPIKeyUUID" {
						continue
					}
					found = true
					if !reflect.DeepEqual(argument.Value, creator) {
						t.Fatalf("creator binding = %#v, want %#v", argument.Value, creator)
					}
				}
				if !found {
					t.Fatal("missing creator binding")
				}
				if name == "filesystem" && strings.Contains(bound.SQL, "AND ak.uuid IS NOT NULL") != (creator != nil) {
					t.Fatal("filesystem must validate supplied keys without requiring a key")
				}
			})
		}
	}
	if nullableString("") != nil || stringFromNullable(nil) != "" {
		t.Fatal("absent creator must map to SQL NULL")
	}
}

func TestPlatformIdentityQueryDoesNotLookUpKeys(t *testing.T) {
	bound := buildPlatformAuthUserMapperResolveSessionIdentity(yourbatis.DialectPostgres, "org", "user", nil)
	if strings.Contains(bound.SQL, "api_keys") {
		t.Fatal("platform identity still depends on an API key")
	}
}

func TestRuntimeIdentitySQLBoundaries(t *testing.T) {
	userUUID := "00000000-0000-4000-8000-000000000001"
	bound := buildSessionMapperInsert(yourbatis.DialectPostgres, sessionWriteParams{RuntimeUserUUID: &userUUID})
	found := false
	for _, arg := range bound.Args {
		if arg.Name == "params.RuntimeUserUUID" {
			found = reflect.DeepEqual(arg.Value, &userUUID)
		}
	}
	if !found || !strings.Contains(bound.SQL, "AS text") {
		t.Fatal("runtime UUID must be independently bound with a concrete JSON input type")
	}
	for _, bound := range []yourbatis.BoundSQL{
		buildSessionMapperPatchMetadata(yourbatis.DialectPostgres, "workspace", "session", nil),
		buildSessionMapperMergeMetadata(yourbatis.DialectPostgres, sessionMetadataPatchParams{}),
	} {
		if !strings.Contains(bound.SQL, "AS jsonb) - '_oma_runtime_user_uuid'") {
			t.Fatal("metadata patches must exclude the runtime identity")
		}
	}
}

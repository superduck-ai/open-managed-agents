package tests

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// TestUUIDAuthAndStringAdminPostgres is intentionally backed by PostgreSQL.
// It exercises Mapper UUID binding/string scanning and the string-based HTTP
// protocol boundary used by API key authentication and Admin responses.
func TestUUIDAuthAndStringAdminPostgres(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("typed-uuid-auth-admin"))
	defer app.close()

	ctx := context.Background()
	key, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load API key through typed UUID row: %v", err)
	}
	if key.UUID == uuid.Nil() || key.OrganizationUUID == uuid.Nil() || key.WorkspaceUUID == uuid.Nil() {
		t.Fatalf("GetAPIKey() returned nil UUIDs: %+v", key)
	}

	organization, err := app.db.GetAdminOrganization(ctx, key.OrganizationUUID)
	if err != nil {
		t.Fatalf("load organization with typed UUID parameter: %v", err)
	}
	if organization.UUID != key.OrganizationUUID.String() {
		t.Fatalf("organization UUID = %s, want %s", organization.UUID, key.OrganizationUUID)
	}

	var response adminObject
	adminDecodeOK(
		t,
		adminDo(t, app, http.MethodGet, "/v1/organizations/me", nil, defaultTestKey, ""),
		&response,
	)
	if response.ID != key.OrganizationUUID.String() || response.Type != "organization" {
		t.Fatalf("organization response = %+v, want UUID %s", response, key.OrganizationUUID)
	}
}

// TestStringUUIDAdminConsoleWorkbenchPostgres keeps the three phase-two paths in
// one PostgreSQL transaction surface while still exercising their public DB
// methods. The created records use unique protocol IDs so this remains safe
// against a developer's existing local test database.
func TestStringUUIDAdminConsoleWorkbenchPostgres(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("typed-uuid-admin-console-workbench"))
	defer app.close()

	ctx := context.Background()
	ids := getDefaultDBIDs(t, app.pool)

	workspace, err := app.db.GetAdminWorkspace(ctx, ids.OrganizationUUID, "workspace_default")
	if err != nil {
		t.Fatalf("load Admin workspace through typed UUID row: %v", err)
	}
	if workspace.UUID == "" || workspace.OrganizationUUID == "" {
		t.Fatalf("Admin workspace returned empty UUIDs: %+v", workspace)
	}

	adminKeys, _, err := app.db.ListAdminAPIKeysPage(ctx, db.ListAdminAPIKeysParams{
		OrganizationUUID: ids.OrganizationUUID,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("list Admin API keys through typed UUID rows: %v", err)
	}
	if len(adminKeys) == 0 || adminKeys[0].WorkspaceUUID == "" {
		t.Fatalf("Admin API key rows did not include workspace UUID: %+v", adminKeys)
	}

	consoleWorkspaces, err := app.db.ListConsoleWorkspaces(ctx, ids.OrganizationUUID, false)
	if err != nil {
		t.Fatalf("list Console workspaces through typed UUID rows: %v", err)
	}
	if len(consoleWorkspaces) == 0 || consoleWorkspaces[0].UUID == "" {
		t.Fatalf("Console workspaces = %+v, want at least one typed UUID row", consoleWorkspaces)
	}

	consoleAPIKeyCountBeforeCreate, err := app.db.CountConsoleAPIKeys(ctx, ids.OrganizationUUID, ids.WorkspaceUUID)
	if err != nil {
		t.Fatalf("count Console API keys before create: %v", err)
	}
	createdKey, err := app.db.CreateConsoleAPIKey(ctx, platform.CreateConsoleAPIKeyInput{
		OrgUUID:            ids.OrganizationUUID,
		WorkspaceUUID:      ids.WorkspaceUUID,
		WorkspaceDisplayID: "workspace_default",
		Name:               "typed UUID PostgreSQL integration",
	})
	if err != nil {
		t.Fatalf("create Console API key with nullable creator UUID: %v", err)
	}
	if createdKey.APIKey.CreatedByUserUUID != nil || createdKey.APIKey.WorkspaceUUID != ids.WorkspaceUUID {
		t.Fatalf("created Console API key = %+v, want null creator and workspace %s", createdKey.APIKey, ids.WorkspaceUUID)
	}
	defer func() {
		if _, cleanupErr := app.pool.Exec(
			context.Background(),
			"delete from console_api_keys where external_id = $1",
			createdKey.APIKey.ID,
		); cleanupErr != nil {
			t.Errorf("delete Console API key fixture: %v", cleanupErr)
		}
		if _, cleanupErr := app.pool.Exec(
			context.Background(),
			"delete from api_keys where external_id = $1",
			createdKey.APIKey.ID,
		); cleanupErr != nil {
			t.Errorf("delete core API key fixture: %v", cleanupErr)
		}
	}()

	consoleAPIKeys, err := app.db.ListConsoleAPIKeys(ctx, ids.OrganizationUUID, &ids.WorkspaceUUID)
	if err != nil {
		t.Fatalf("list Console API keys after create: %v", err)
	}
	if !containsConsoleAPIKey(consoleAPIKeys, createdKey.APIKey.ID, "active") {
		t.Fatalf("Console API key list does not contain active key %q: %+v", createdKey.APIKey.ID, consoleAPIKeys)
	}
	consoleAPIKeyCountAfterCreate, err := app.db.CountConsoleAPIKeys(ctx, ids.OrganizationUUID, ids.WorkspaceUUID)
	if err != nil {
		t.Fatalf("count Console API keys after create: %v", err)
	}
	if consoleAPIKeyCountAfterCreate != consoleAPIKeyCountBeforeCreate+1 {
		t.Fatalf(
			"Console API key count after create = %d, want %d",
			consoleAPIKeyCountAfterCreate,
			consoleAPIKeyCountBeforeCreate+1,
		)
	}
	archivedKey, err := app.db.UpdateConsoleAPIKeyStatus(ctx, platform.UpdateConsoleAPIKeyStatusInput{
		OrgUUID:       ids.OrganizationUUID,
		WorkspaceUUID: ids.WorkspaceUUID,
		APIKeyID:      createdKey.APIKey.ID,
		Status:        "archived",
	})
	if err != nil {
		t.Fatalf("archive Console API key: %v", err)
	}
	if archivedKey.Status != "archived" || archivedKey.ArchivedAt == nil {
		t.Fatalf("archived Console API key = %+v", archivedKey)
	}
	consoleAPIKeyCountAfterArchive, err := app.db.CountConsoleAPIKeys(ctx, ids.OrganizationUUID, ids.WorkspaceUUID)
	if err != nil {
		t.Fatalf("count Console API keys after archive: %v", err)
	}
	if consoleAPIKeyCountAfterArchive != consoleAPIKeyCountBeforeCreate {
		t.Fatalf(
			"Console API key count after archive = %d, want %d",
			consoleAPIKeyCountAfterArchive,
			consoleAPIKeyCountBeforeCreate,
		)
	}

	promptID := "prompt_typed_uuid_" + uuid.NewV4().String()
	revisionID := "revision_typed_uuid_" + uuid.NewV4().String()
	if _, err := app.db.UpsertWorkbenchPrompt(ctx, platform.WorkbenchPromptRecord{
		OrgUUID:            ids.OrganizationUUID,
		PromptUUID:         promptID,
		WorkspaceUUID:      ids.WorkspaceUUID,
		WorkspaceDisplayID: "workspace_default",
		Name:               "typed UUID integration",
		LatestRevisionUUID: &revisionID,
	}); err != nil {
		t.Fatalf("upsert Workbench prompt with typed UUID parameters: %v", err)
	}
	defer func() {
		if err := app.db.DeleteWorkbenchPromptState(
			context.Background(),
			ids.OrganizationUUID,
			promptID,
			ids.WorkspaceUUID,
			"workspace_default",
		); err != nil {
			t.Errorf("delete Workbench prompt state: %v", err)
		}
	}()

	prompt, err := app.db.GetWorkbenchPrompt(ctx, ids.OrganizationUUID, promptID)
	if err != nil {
		t.Fatalf("get Workbench prompt through typed UUID row: %v", err)
	}
	if prompt.OrgUUID != ids.OrganizationUUID || prompt.WorkspaceUUID != ids.WorkspaceUUID {
		t.Fatalf("Workbench prompt UUIDs = (%s, %s), want (%s, %s)", prompt.OrgUUID, prompt.WorkspaceUUID, ids.OrganizationUUID, ids.WorkspaceUUID)
	}

	if err := app.db.UpsertWorkbenchRevision(ctx, platform.WorkbenchRevisionRecord{
		OrgUUID:      ids.OrganizationUUID,
		PromptUUID:   promptID,
		RevisionUUID: revisionID,
		Payload:      map[string]any{"model": "test"},
	}); err != nil {
		t.Fatalf("upsert Workbench revision: %v", err)
	}
	revision, err := app.db.GetWorkbenchRevision(ctx, ids.OrganizationUUID, promptID, revisionID)
	if err != nil {
		t.Fatalf("get Workbench revision = (%+v, %v)", revision, err)
	}
	if revision.Payload["model"] != "test" {
		t.Fatalf("Workbench revision payload = %+v", revision.Payload)
	}

	kvRecord := platform.WorkbenchKVRecord{
		OrgUUID:    ids.OrganizationUUID,
		PromptUUID: promptID,
		Key:        "model",
		Value:      "test",
		Version:    map[string]any{"number": 1},
	}
	if err := app.db.UpsertWorkbenchKV(ctx, kvRecord); err != nil {
		t.Fatalf("upsert Workbench key value: %v", err)
	}
	keyValue, err := app.db.GetWorkbenchKV(ctx, ids.OrganizationUUID, promptID, kvRecord.Key)
	if err != nil {
		t.Fatalf("get Workbench key value = (%+v, %v)", keyValue, err)
	}
	version, versionOK := keyValue.Version.(map[string]any)
	if !versionOK || version["number"] != float64(1) {
		t.Fatalf("get Workbench key value = (%+v, %v)", keyValue, err)
	}
	kvRecord.Version = nil
	if err := app.db.UpsertWorkbenchKV(ctx, kvRecord); err != nil {
		t.Fatalf("upsert Workbench key value with nullable version: %v", err)
	}
	keyValue, err = app.db.GetWorkbenchKV(ctx, ids.OrganizationUUID, promptID, kvRecord.Key)
	if err != nil {
		t.Fatalf("get Workbench key value with nullable version = (%+v, %v)", keyValue, err)
	}
	if keyValue.Version != nil {
		t.Fatalf("get Workbench key value with nullable version = (%+v, %v)", keyValue, err)
	}

	evaluationID := "evaluation_typed_uuid_" + uuid.NewV4().String()
	if err := app.db.UpsertWorkbenchEvaluation(ctx, platform.WorkbenchEvaluationRecord{
		OrgUUID:        ids.OrganizationUUID,
		RevisionUUID:   revisionID,
		EvaluationUUID: evaluationID,
		Payload:        map[string]any{"score": 1},
	}); err != nil {
		t.Fatalf("upsert Workbench evaluation: %v", err)
	}
	revisionIDs, err := app.db.ListWorkbenchEvaluationRevisionIDs(ctx, ids.OrganizationUUID)
	if err != nil || !slices.Contains(revisionIDs, revisionID) {
		t.Fatalf("list Workbench evaluation revision IDs = (%+v, %v)", revisionIDs, err)
	}
	evaluations, err := app.db.ListWorkbenchEvaluations(ctx, ids.OrganizationUUID, revisionID)
	if err != nil || len(evaluations) != 1 || evaluations[0].EvaluationUUID != evaluationID {
		t.Fatalf("list Workbench evaluations = (%+v, %v)", evaluations, err)
	}
	evaluation, err := app.db.GetWorkbenchEvaluation(ctx, ids.OrganizationUUID, evaluationID)
	if err != nil {
		t.Fatalf("get Workbench evaluation = (%+v, %v)", evaluation, err)
	}
	if evaluation.Payload["score"] != float64(1) {
		t.Fatalf("get Workbench evaluation = (%+v, %v)", evaluation, err)
	}
	deletedEvaluation, err := app.db.DeleteWorkbenchEvaluation(ctx, ids.OrganizationUUID, evaluationID)
	if err != nil {
		t.Fatalf("delete Workbench evaluation = (%+v, %v)", deletedEvaluation, err)
	}
	if deletedEvaluation.EvaluationUUID != evaluationID {
		t.Fatalf("delete Workbench evaluation = (%+v, %v)", deletedEvaluation, err)
	}

	generatedValues := map[string]any{"input": "value", "expected": "result"}
	if err := app.db.AppendWorkbenchGeneratedTestCase(ctx, ids.OrganizationUUID, generatedValues); err != nil {
		t.Fatalf("append Workbench generated test case: %v", err)
	}
	takenValues, found, err := app.db.TakeWorkbenchGeneratedTestCase(ctx, ids.OrganizationUUID, map[string]any{"input": nil})
	if err != nil || !found || takenValues["expected"] != "result" {
		t.Fatalf("take Workbench generated test case = (%+v, %t, %v)", takenValues, found, err)
	}
	if err := app.db.DeleteWorkbenchKV(ctx, ids.OrganizationUUID, promptID, kvRecord.Key); err != nil {
		t.Fatalf("delete Workbench key value: %v", err)
	}
}

func containsConsoleAPIKey(keys []platform.ConsoleAPIKey, keyID string, status string) bool {
	for _, key := range keys {
		if key.ID == keyID && key.Status == status {
			return true
		}
	}
	return false
}

// TestTypedUUIDResourceFamiliesPostgres exercises each resource family changed
// by the UUID-boundary migration against PostgreSQL. It deliberately covers
// inserts, scans, UUID-keyed follow-up queries, cursors, and a nullable UUID.
func TestTypedUUIDResourceFamiliesPostgres(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("typed-uuid-resource-families"))
	defer app.close()

	ctx := context.Background()
	ids := getDefaultDBIDs(t, app.pool)
	suffix := uuid.NewV4().String()
	now := time.Now().UTC()

	agentID := "agent_typed_uuid_" + suffix
	agent, err := app.db.CreateAgent(ctx, db.Agent{
		UUID:                uuid.NewV4().String(),
		ExternalID:          agentID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		CurrentVersion:      1,
		Name:                "typed UUID PostgreSQL agent",
		Model:               []byte(`{"model":"claude-sonnet-4-5"}`),
		MCPServers:          []byte(`[]`),
		Metadata:            []byte(`{}`),
		Multiagent:          []byte(`{}`),
		Skills:              []byte(`[]`),
		Tools:               []byte(`[]`),
		CreatedAt:           now,
		UpdatedAt:           now,
	}, "agentver_typed_uuid_"+suffix)
	if err != nil {
		t.Fatalf("create Agent through typed UUID parameters: %v", err)
	}
	if loaded, err := app.db.GetAgent(ctx, ids.WorkspaceUUID, agentID); err != nil || loaded.UUID != agent.UUID {
		t.Fatalf("get Agent through typed UUID row = (%+v, %v), want %s", loaded, err, agent.UUID)
	}
	if agents, _, err := app.db.ListAgentsPage(ctx, db.ListAgentsPageParams{
		WorkspaceUUID: ids.WorkspaceUUID,
		Limit:         1,
		Cursor:        &db.AgentPageCursor{CreatedAt: agent.CreatedAt.Add(time.Second), UUID: uuid.MustParse(agent.UUID).String()},
	}); err != nil || len(agents) == 0 {
		t.Fatalf("list Agents with typed UUID cursor = (%+v, %v)", agents, err)
	}
	nextAgent := agent
	nextAgent.Name = "string UUID PostgreSQL agent"
	nextAgent.UpdatedAt = now.Add(time.Minute)
	updatedAgent, err := app.db.UpdateAgent(
		ctx,
		ids.WorkspaceUUID,
		agentID,
		1,
		nextAgent,
		"agentver_string_uuid_"+suffix,
	)
	if err != nil || updatedAgent.CurrentVersion != 2 || updatedAgent.Name != nextAgent.Name {
		t.Fatalf("update Agent through string UUID mapper parameters = (%+v, %v)", updatedAgent, err)
	}
	if version, err := app.db.GetAgentVersion(ctx, ids.WorkspaceUUID, agentID, 1); err != nil || version.Name != agent.Name {
		t.Fatalf("get initial Agent version through string UUID mapper parameters = (%+v, %v)", version, err)
	}
	if versions, _, err := app.db.ListAgentVersionsPage(ctx, db.ListAgentVersionsPageParams{
		WorkspaceUUID:   ids.WorkspaceUUID,
		AgentExternalID: agentID,
		Limit:           10,
	}); err != nil || len(versions) != 2 || versions[0].CurrentVersion != 2 {
		t.Fatalf("list Agent versions through string UUID mapper parameters = (%+v, %v)", versions, err)
	}
	if archived, err := app.db.ArchiveAgent(ctx, ids.WorkspaceUUID, agentID); err != nil || archived.ArchivedAt == nil {
		t.Fatalf("archive Agent through string UUID mapper parameters = (%+v, %v)", archived, err)
	}

	skillID := "skill_typed_uuid_" + suffix
	skillTitle := "typed UUID " + suffix
	skill, skillVersion, err := app.db.CreateSkillWithVersion(ctx, db.Skill{
		UUID:                uuid.NewV4().String(),
		ExternalID:          skillID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		DisplayTitle:        &skillTitle,
		CreatedAt:           now,
	}, db.SkillVersion{
		UUID:                uuid.NewV4().String(),
		ExternalID:          "skillver_typed_uuid_" + suffix,
		WorkspaceUUID:       ids.WorkspaceUUID,
		Version:             "1.0.0",
		Name:                "typed-uuid",
		Description:         "PostgreSQL UUID boundary",
		Directory:           "typed-uuid",
		S3Bucket:            "test",
		S3Key:               "typed-uuid/" + suffix,
		SizeBytes:           1,
		SHA256:              "typed-uuid",
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		CreatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create Skill through typed UUID parameters: %v", err)
	}
	if versions, _, err := app.db.ListSkillVersionsPage(ctx, db.ListSkillVersionsPageParams{
		WorkspaceUUID:   ids.WorkspaceUUID,
		SkillExternalID: skillID,
		Limit:           10,
	}); err != nil || len(versions) != 1 || versions[0].UUID != skillVersion.UUID {
		t.Fatalf("list Skill versions through typed UUID rows = (%+v, %v)", versions, err)
	}
	updatedSkill, secondSkillVersion, err := app.db.CreateSkillVersion(
		ctx,
		ids.WorkspaceUUID,
		skillID,
		db.SkillVersion{
			UUID:                uuid.NewV4().String(),
			ExternalID:          "skillver_typed_uuid_second_" + suffix,
			Version:             "2.0.0",
			Name:                "typed-uuid-v2",
			Description:         "PostgreSQL UUID boundary second version",
			Directory:           "typed-uuid",
			S3Bucket:            "test",
			S3Key:               "typed-uuid/second/" + suffix,
			SizeBytes:           2,
			SHA256:              "typed-uuid-v2",
			CreatedByAPIKeyUUID: ids.APIKeyUUID,
			CreatedAt:           now.Add(time.Second),
		},
	)
	if err != nil || updatedSkill.LatestVersion == nil || *updatedSkill.LatestVersion != secondSkillVersion.Version {
		t.Fatalf("create second Skill version = (%+v, %+v, %v)", updatedSkill, secondSkillVersion, err)
	}
	if loaded, err := app.db.GetSkillVersion(ctx, ids.WorkspaceUUID, skillID, skillVersion.Version); err != nil || loaded.UUID != skillVersion.UUID {
		t.Fatalf("get Skill version through string UUID mapper parameters = (%+v, %v)", loaded, err)
	}
	if latest, err := app.db.GetLatestSkillVersion(ctx, ids.WorkspaceUUID, skillID); err != nil || latest.UUID != secondSkillVersion.UUID {
		t.Fatalf("get latest Skill version through string UUID mapper parameters = (%+v, %v)", latest, err)
	}

	environmentID := "env_typed_uuid_" + suffix
	environment, err := app.db.CreateEnvironment(ctx, db.Environment{
		UUID:                uuid.NewV4().String(),
		ExternalID:          environmentID,
		OrganizationUUID:    ids.OrganizationUUID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		Name:                "typed UUID " + suffix,
		Description:         "PostgreSQL UUID boundary",
		Config:              []byte(`{}`),
		Metadata:            []byte(`{}`),
		Provider:            "e2b",
		ResolvedTemplate:    "base",
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create Environment through typed UUID parameters: %v", err)
	}
	sandbox, err := app.db.CreateEnvironmentSandbox(ctx, db.EnvironmentSandbox{
		UUID:                  uuid.NewV4().String(),
		ExternalID:            "sbx_typed_uuid_" + suffix,
		OrganizationUUID:      ids.OrganizationUUID,
		WorkspaceUUID:         ids.WorkspaceUUID,
		EnvironmentUUID:       environment.UUID,
		EnvironmentExternalID: environment.ExternalID,
		Provider:              "e2b",
		Template:              "base",
		State:                 "creating",
		Metadata:              []byte(`{}`),
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	if err != nil || sandbox.WorkUUID != nil {
		t.Fatalf("create Environment sandbox with nullable work UUID = (%+v, %v)", sandbox, err)
	}
	if loaded, err := app.db.GetEnvironmentByUUID(ctx, ids.WorkspaceUUID, environment.UUID); err != nil || loaded.ExternalID != environmentID {
		t.Fatalf("get Environment by typed UUID = (%+v, %v)", loaded, err)
	}

	deploymentID := "dep_typed_uuid_" + suffix
	deployment, err := app.db.CreateDeployment(ctx, db.Deployment{
		UUID:                  uuid.NewV4().String(),
		ExternalID:            deploymentID,
		OrganizationUUID:      ids.OrganizationUUID,
		WorkspaceUUID:         ids.WorkspaceUUID,
		CreatedByAPIKeyUUID:   ids.APIKeyUUID,
		EnvironmentUUID:       environment.UUID,
		EnvironmentExternalID: environment.ExternalID,
		AgentUUID:             agent.UUID,
		AgentExternalID:       agent.ExternalID,
		AgentVersion:          agent.CurrentVersion,
		AgentSnapshot:         []byte(`{}`),
		Name:                  "typed UUID PostgreSQL deployment",
		Metadata:              []byte(`{}`),
		InitialEvents:         []byte(`[]`),
		Resources:             []byte(`[]`),
		ResourceSecrets:       []byte(`[]`),
		VaultIDs:              []byte(`[]`),
		Status:                "active",
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	if err != nil {
		t.Fatalf("create Deployment through typed UUID parameters: %v", err)
	}
	if loaded, err := app.db.GetDeployment(ctx, ids.WorkspaceUUID, deploymentID); err != nil || loaded.UUID != deployment.UUID {
		t.Fatalf("get Deployment through typed UUID row = (%+v, %v)", loaded, err)
	}

	vaultID := "vlt_typed_uuid_" + suffix
	vault, err := app.db.CreateVault(ctx, db.Vault{
		UUID:                uuid.NewV4().String(),
		ExternalID:          vaultID,
		OrganizationUUID:    ids.OrganizationUUID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		DisplayName:         "typed UUID PostgreSQL vault",
		Metadata:            []byte(`{}`),
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create Vault through typed UUID parameters: %v", err)
	}
	credential, err := app.db.CreateVaultCredential(ctx, db.VaultCredential{
		UUID:             uuid.NewV4().String(),
		ExternalID:       "vcrd_typed_uuid_" + suffix,
		OrganizationUUID: ids.OrganizationUUID,
		WorkspaceUUID:    ids.WorkspaceUUID,
		VaultExternalID:  vault.ExternalID,
		DisplayName:      "nullable creator",
		Metadata:         []byte(`{}`),
		AuthType:         "static_bearer",
		CredentialKey:    "typed_uuid_" + suffix,
		Auth:             []byte(`{"type":"bearer"}`),
		SecretEnvelope: &secrets.Envelope{
			Ciphertext:    []byte("typed-uuid-cipher"),
			Nonce:         []byte("nonce-12byte"),
			WrappedDEK:    []byte("typed-uuid-wrap"),
			FormatVersion: 1,
			KeyProvider:   "local",
			KeyVersion:    1,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil || credential.CreatedByAPIKeyUUID != "" || credential.VaultUUID != vault.UUID {
		t.Fatalf("create Vault credential with nullable creator UUID = (%+v, %v)", credential, err)
	}

	memoryStoreID := "memstore_typed_uuid_" + suffix
	memoryStore, err := app.db.CreateMemoryStore(ctx, db.MemoryStore{
		UUID:                uuid.NewV4().String(),
		ExternalID:          memoryStoreID,
		OrganizationUUID:    ids.OrganizationUUID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		Name:                "typed UUID " + suffix,
		Description:         "PostgreSQL UUID boundary",
		Metadata:            []byte(`{}`),
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create Memory store through typed UUID parameters: %v", err)
	}
	memoryPath := "/typed-uuid-" + suffix + ".txt"
	memorySHA := strings.Repeat("a", 64)
	memoryBucket := "test"
	memoryKey := "memory/" + suffix
	memory, err := app.db.CreateMemory(ctx, db.Memory{
		UUID:                  uuid.NewV4().String(),
		ExternalID:            "mem_typed_uuid_" + suffix,
		WorkspaceUUID:         ids.WorkspaceUUID,
		MemoryStoreExternalID: memoryStore.ExternalID,
		Path:                  memoryPath,
		ContentSizeBytes:      1,
		ContentSHA256:         memorySHA,
		S3Bucket:              memoryBucket,
		S3Key:                 memoryKey,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, db.MemoryVersion{
		UUID:             uuid.NewV4().String(),
		ExternalID:       "memver_typed_uuid_" + suffix,
		Operation:        "created",
		Path:             &memoryPath,
		ContentSizeBytes: func() *int64 { value := int64(1); return &value }(),
		ContentSHA256:    &memorySHA,
		S3Bucket:         &memoryBucket,
		S3Key:            &memoryKey,
		CreatedBy:        db.MemoryActor{Type: "api_actor", APIKeyUUID: ids.APIKeyUUID, APIKeyExternalID: "api_key_default"},
		CreatedAt:        now,
	})
	if err != nil || memory.MemoryStoreUUID != memoryStore.UUID || memory.CurrentVersionUUID == "" {
		t.Fatalf("create Memory and version through typed UUIDs = (%+v, %v)", memory, err)
	}

	batch, err := app.db.CreateMessageBatch(ctx, db.MessageBatch{
		UUID:                uuid.NewV4().String(),
		ExternalID:          "msgbatch_typed_uuid_" + suffix,
		OrganizationUUID:    ids.OrganizationUUID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		APIVariant:          "stable",
		AnthropicVersion:    "2023-06-01",
		CreatedAt:           now,
		ExpiresAt:           now.Add(time.Hour),
	}, []db.NewBatchRequest{{
		ExternalID:    "msgbatchreq_typed_uuid_" + suffix,
		WorkspaceUUID: ids.WorkspaceUUID,
		RequestIndex:  0,
		CustomID:      "typed-uuid",
		Params:        []byte(`{"model":"claude-sonnet-4-5","max_tokens":1,"messages":[]}`),
	}})
	if err != nil {
		t.Fatalf("create Message Batch through typed UUID parameters: %v", err)
	}
	if request, err := app.db.GetMessageBatchRequestByIndex(ctx, batch.UUID, 0); err != nil || request.MessageBatchUUID != batch.UUID {
		t.Fatalf("get Message Batch request through typed UUID row = (%+v, %v)", request, err)
	}

	endpoint, err := app.db.CreateWebhookEndpoint(ctx, db.WebhookEndpoint{
		UUID:                uuid.NewV4().String(),
		ExternalID:          "wh_typed_uuid_" + suffix,
		OrganizationUUID:    ids.OrganizationUUID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		URL:                 "https://example.com/typed-uuid",
		Name:                "typed UUID PostgreSQL webhook",
		EnabledEvents:       []string{"typed_uuid." + suffix},
		SigningSecret:       "secret",
		Status:              "enabled",
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create Webhook endpoint through typed UUID parameters: %v", err)
	}
	eventType := "typed_uuid." + suffix
	if err := app.db.EnqueueWebhookDeliveryJobForEndpoint(ctx, ids.WorkspaceUUID, eventType, []byte(`{"type":"typed_uuid"}`), endpoint.UUID); err != nil {
		t.Fatalf("enqueue Webhook delivery through typed UUID parameters: %v", err)
	}
	jobs, err := app.db.LeaseWebhookDeliveryJobs(ctx, "typed-uuid-"+suffix, 100, time.Minute)
	if err != nil {
		t.Fatalf("lease Webhook deliveries through typed UUID rows: %v", err)
	}
	foundJob := false
	for _, job := range jobs {
		if job.EventType == eventType {
			foundJob = job.WorkspaceUUID == ids.WorkspaceUUID &&
				job.WebhookEndpointUUID != nil &&
				*job.WebhookEndpointUUID == endpoint.UUID
			if err := app.db.CompleteWebhookDeliveryJob(ctx, job.UUID); err != nil {
				t.Fatalf("complete Webhook job through typed UUID parameter: %v", err)
			}
		}
	}
	if !foundJob {
		t.Fatalf("leased Webhook jobs did not contain typed UUID endpoint %s: %+v", endpoint.UUID, jobs)
	}

	if loadedSkill, err := app.db.GetSkill(ctx, ids.WorkspaceUUID, skillID); err != nil || loadedSkill.UUID != skill.UUID {
		t.Fatalf("get Skill through typed UUID row = (%+v, %v)", loadedSkill, err)
	}
	deletedVersion, latestVersion, err := app.db.SoftDeleteSkillVersion(
		ctx,
		ids.WorkspaceUUID,
		skillID,
		secondSkillVersion.Version,
	)
	if err != nil || deletedVersion.UUID != secondSkillVersion.UUID || latestVersion == nil || *latestVersion != skillVersion.Version {
		t.Fatalf("soft delete Skill version = (%+v, %+v, %v)", deletedVersion, latestVersion, err)
	}
	deletedSkill, deletedVersions, err := app.db.SoftDeleteSkill(ctx, ids.WorkspaceUUID, skillID)
	if err != nil || deletedSkill.UUID != skill.UUID || len(deletedVersions) != 1 || deletedVersions[0].UUID != skillVersion.UUID {
		t.Fatalf("soft delete Skill = (%+v, %+v, %v)", deletedSkill, deletedVersions, err)
	}
}

// TestTypedUUIDSessionsAndRuntimePostgres covers the transaction-heavy session
// and code-session paths plus the smaller runtime stores. All assertions cross
// a real PostgreSQL bind/scan boundary; nullable UUIDs are represented as SQL
// NULL and converted to strings only after the row has been scanned.
func TestTypedUUIDSessionsAndRuntimePostgres(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("typed-uuid-sessions-runtime"))
	t.Cleanup(app.close)

	ctx := context.Background()
	ids := getDefaultDBIDs(t, app.pool)
	now := time.Now().UTC()
	suffix := strings.ReplaceAll(uuid.NewV4().String(), "-", "")

	input := filestoreSessionCreateInput(ids.OrganizationUUID, ids.WorkspaceUUID, ids.APIKeyUUID)
	session, thread, _, work, err := app.db.CreateSession(ctx, input)
	if err != nil {
		t.Fatalf("create Session transaction through typed UUID parameters: %v", err)
	}
	cleanupFilestoreSession(t, app, ids.WorkspaceUUID, session.ExternalID)
	if session.UUID != input.Session.UUID || session.DeploymentUUID != nil ||
		thread.UUID != input.Thread.UUID || thread.ParentThreadUUID != nil ||
		work.UUID != input.Work.UUID {
		t.Fatalf("typed Session transaction rows = (%+v, %+v, %+v)", session, thread, work)
	}

	sessions, _, err := app.db.ListSessionsPage(ctx, db.ListSessionsPageParams{
		WorkspaceUUID: ids.WorkspaceUUID,
		Limit:         10,
		Cursor: &db.SessionPageCursor{
			CreatedAt: session.CreatedAt.Add(time.Second),
			UUID:      uuid.NewV4().String(),
		},
	})
	if err != nil || len(sessions) == 0 {
		t.Fatalf("list Sessions with typed UUID cursor = (%+v, %v)", sessions, err)
	}
	threads, _, err := app.db.ListSessionThreadsPage(ctx, db.ListSessionThreadsPageParams{
		WorkspaceUUID:     ids.WorkspaceUUID,
		SessionExternalID: session.ExternalID,
		Limit:             10,
		Cursor: &db.SessionThreadPageCursor{
			CreatedAt: thread.CreatedAt.Add(time.Second),
			UUID:      uuid.NewV4().String(),
		},
	})
	if err != nil || len(threads) != 1 || threads[0].UUID != thread.UUID {
		t.Fatalf("list Session threads with typed UUID cursor = (%+v, %v)", threads, err)
	}

	eventExternalID := "sevt_typed_uuid_" + suffix
	events, err := app.db.AppendSessionEvents(ctx, ids.WorkspaceUUID, session.ExternalID, []db.SessionEvent{{
		UUID:        uuid.NewV4().String(),
		ExternalID:  eventExternalID,
		EventType:   "typed_uuid.runtime",
		Payload:     []byte(`{"typed_uuid":true}`),
		ProcessedAt: now,
		CreatedAt:   now,
	}}, nil)
	if err != nil || len(events) != 1 || events[0].ThreadUUID == nil || *events[0].ThreadUUID != thread.UUID {
		t.Fatalf("append Session event with inferred typed thread UUID = (%+v, %v)", events, err)
	}
	listedEvents, _, err := app.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceUUID:     ids.WorkspaceUUID,
		SessionExternalID: session.ExternalID,
		Limit:             10,
		Cursor: &db.SessionEventPageCursor{
			CreatedAt: now.Add(-time.Second),
			UUID:      uuid.NewV4().String(),
		},
	})
	if err != nil || len(listedEvents) != 1 || listedEvents[0].UUID != events[0].UUID {
		t.Fatalf("list Session events with typed UUID cursor = (%+v, %v)", listedEvents, err)
	}

	codeSession, err := app.db.CreateCodeSession(ctx, db.CreateCodeSessionInput{
		ExternalID:            "cse_typed_uuid_" + suffix,
		OrganizationUUID:      ids.OrganizationUUID,
		WorkspaceUUID:         ids.WorkspaceUUID,
		SessionUUID:           session.UUID,
		SessionExternalID:     session.ExternalID,
		EnvironmentUUID:       session.EnvironmentUUID,
		EnvironmentExternalID: session.EnvironmentExternalID,
		WorkDir:               "/workspace",
		PermissionMode:        "bypassPermissions",
		Model:                 "claude-sonnet-4-5",
		Status:                "active",
		Metadata:              []byte(`{}`),
		OAuthAccessTokenHash:  "typed-uuid-" + suffix,
		CreatedAt:             now,
	})
	if err != nil || codeSession.UUID == "" {
		t.Fatalf("create Code Session through typed UUID parameters = (%+v, %v)", codeSession, err)
	}
	providerSandboxID := "provider_typed_uuid_" + suffix
	runningSandbox, err := app.db.CreateEnvironmentSandbox(ctx, db.EnvironmentSandbox{
		UUID:                  uuid.NewV4().String(),
		ExternalID:            "envsbx_typed_uuid_" + suffix,
		OrganizationUUID:      ids.OrganizationUUID,
		WorkspaceUUID:         ids.WorkspaceUUID,
		EnvironmentUUID:       session.EnvironmentUUID,
		EnvironmentExternalID: session.EnvironmentExternalID,
		WorkUUID:              &work.UUID,
		WorkExternalID:        &work.ExternalID,
		Provider:              "e2b",
		Template:              "base",
		ProviderSandboxID:     &providerSandboxID,
		State:                 "running",
		Metadata:              []byte(`{}`),
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	if err != nil || runningSandbox.WorkUUID == nil || *runningSandbox.WorkUUID != work.UUID {
		t.Fatalf("create running sandbox with typed work UUID = (%+v, %v)", runningSandbox, err)
	}
	epoch, _, err := app.db.RegisterCodeSessionWorker(ctx, codeSession.ExternalID, db.CodeSessionWorkerBinding{
		TokenSessionID: "typed-uuid-" + suffix,
		AuthMode:       "oauth",
	}, time.Minute)
	if err != nil || epoch != 1 {
		t.Fatalf("register Code Session worker through typed UUID update = (%d, %v)", epoch, err)
	}
	runningStatus := "running"
	if _, err := app.db.UpdateCodeSessionWorkerState(ctx, codeSession.ExternalID, db.UpdateCodeSessionWorkerStateInput{
		WorkerEpoch:  epoch,
		WorkerStatus: &runningStatus,
	}); err != nil {
		t.Fatalf("mark Code Session worker running through typed UUID update: %v", err)
	}
	renewableSandbox, err := app.db.GetRenewableEnvironmentSandboxForCodeSession(ctx, codeSession.ExternalID)
	if err != nil || renewableSandbox.UUID != runningSandbox.UUID ||
		renewableSandbox.ProviderSandboxID == nil || *renewableSandbox.ProviderSandboxID != providerSandboxID {
		t.Fatalf("resolve renewable sandbox through typed UUID joins = (%+v, %v)", renewableSandbox, err)
	}
	if _, err := app.db.RecordCodeSessionWorkerHeartbeat(
		ctx,
		codeSession.ExternalID,
		epoch,
		time.Minute,
		time.Second,
	); err != nil {
		t.Fatalf("record Code Session heartbeat through typed UUID transaction: %v", err)
	}
	credential, err := app.db.GetCodeSessionCredentialContextForIssue(
		ctx,
		ids.OrganizationUUID,
		ids.WorkspaceUUID,
		codeSession.ExternalID,
	)
	if err != nil || credential.CodeSessionUUID != codeSession.UUID ||
		credential.PublicSessionUUID != session.UUID || credential.AgentUUID != session.AgentUUID {
		t.Fatalf("get Code Session credential typed UUID projection = (%+v, %v)", credential, err)
	}
	inbound, duplicate, err := app.db.AppendCodeSessionInboundEvent(ctx, codeSession.ExternalID, db.AppendCodeSessionEventInput{
		ExternalID:     "csein_typed_uuid_" + suffix,
		EventType:      "control_request",
		EventSubtype:   "typed_uuid",
		Payload:        []byte(`{}`),
		PayloadHash:    strings.Repeat("b", 64),
		IdempotencyKey: "typed-uuid-" + suffix,
		DeliveryStatus: "queued",
		Source:         "integration",
		CreatedAt:      now,
	})
	if err != nil || duplicate || inbound.CodeSessionUUID != codeSession.UUID {
		t.Fatalf("append Code Session event through typed UUID transaction = (%+v, %v, %v)", inbound, duplicate, err)
	}
	internalEvents, err := app.db.AppendCodeSessionInternalEvents(ctx, codeSession.ExternalID, epoch, []db.AppendCodeSessionInternalEventInput{{
		ExternalID:     "cseint_typed_uuid_" + suffix,
		EventType:      "typed_uuid",
		PayloadUUID:    "payload_" + suffix,
		Payload:        []byte(`{}`),
		PayloadHash:    strings.Repeat("c", 64),
		IdempotencyKey: "internal-typed-uuid-" + suffix,
		EventMetadata:  []byte(`{}`),
		CreatedAt:      now,
	}})
	if err != nil || len(internalEvents) != 1 || internalEvents[0].CodeSessionUUID != codeSession.UUID {
		t.Fatalf("append Code Session internal event through typed UUID transaction = (%+v, %v)", internalEvents, err)
	}

	userExternalID, orgUUID, err := app.db.FindBootstrapUserContext(ctx, "")
	if err != nil {
		t.Fatalf("find bootstrap platform user: %v", err)
	}
	bootstrapUser, err := app.db.GetBootstrapUser(ctx, userExternalID)
	if err != nil || bootstrapUser == nil || !isValidUUID(bootstrapUser.UUID) {
		t.Fatalf("get bootstrap user through typed UUID row = (%+v, %v)", bootstrapUser, err)
	}
	bootstrapOrganization, err := app.db.GetPlatformOrganization(ctx, orgUUID)
	if err != nil || bootstrapOrganization == nil || bootstrapOrganization.UUID != orgUUID {
		t.Fatalf("get bootstrap organization through typed UUID row = (%+v, %v)", bootstrapOrganization, err)
	}
	bootstrapOrganizations, err := app.db.ListBootstrapUserOrganizations(ctx, userExternalID, orgUUID)
	if err != nil || len(bootstrapOrganizations) == 0 || bootstrapOrganizations[0].UUID != orgUUID {
		t.Fatalf("list bootstrap organizations through typed UUID rows = (%+v, %v)", bootstrapOrganizations, err)
	}
	platformExpiresAt := now.Add(time.Hour)
	platformIdentity, err := app.db.ResolvePlatformSessionIdentity(ctx, platformsession.CreateInput{
		SessionKey: "typed-uuid-" + suffix,
		UserUUID:   userExternalID,
		OrgUUID:    orgUUID,
		ExpiresAt:  &platformExpiresAt,
	})
	if err != nil || platformIdentity.OrganizationUUID != orgUUID ||
		!isValidUUID(platformIdentity.UserUUID) || !isValidUUID(platformIdentity.WorkspaceUUID) || platformIdentity.APIKeyUUID != "" {
		t.Fatalf("resolve platform identity through typed UUID rows = (%+v, %v)", platformIdentity, err)
	}

	catalog, err := app.db.UpsertMCPToolCatalog(ctx, "url", "https://mcp.example.test/"+suffix, []db.MCPToolCatalogItem{{
		Name:        "typed_uuid",
		Description: "PostgreSQL UUID boundary",
	}})
	if err != nil || !isValidUUID(catalog.UUID) {
		t.Fatalf("upsert MCP tool catalog through typed UUID row = (%+v, %v)", catalog, err)
	}

	vault, err := app.db.CreateVault(ctx, db.Vault{
		UUID:                uuid.NewV4().String(),
		ExternalID:          "vlt_runtime_typed_uuid_" + suffix,
		OrganizationUUID:    ids.OrganizationUUID,
		WorkspaceUUID:       ids.WorkspaceUUID,
		CreatedByAPIKeyUUID: ids.APIKeyUUID,
		DisplayName:         "runtime typed UUID",
		Metadata:            []byte(`{}`),
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		t.Fatalf("create runtime Vault: %v", err)
	}
	flow, err := app.db.CreateMCPOAuthFlow(ctx, db.MCPOAuthFlow{
		UUID:                    uuid.NewV4().String(),
		ExternalID:              "mcpoauth_typed_uuid_" + suffix,
		OrganizationUUID:        ids.OrganizationUUID,
		WorkspaceUUID:           ids.WorkspaceUUID,
		VaultUUID:               vault.UUID,
		VaultExternalID:         vault.ExternalID,
		MCPServerURL:            "https://mcp.example.test/" + suffix,
		RedirectURL:             "https://app.example.test/callback",
		DisplayName:             "typed UUID",
		Source:                  "integration",
		AuthorizationEndpoint:   "https://auth.example.test/authorize",
		TokenEndpoint:           "https://auth.example.test/token",
		Resource:                "https://mcp.example.test",
		ClientID:                "typed-uuid",
		ClientCredentialSource:  "sealed",
		TokenEndpointAuthMethod: "none",
		CodeChallengeMethod:     "S256",
		SecretEnvelope: &secrets.Envelope{
			Ciphertext:    []byte("typed-uuid-flow-cipher"),
			Nonce:         []byte("nonce-12byte"),
			WrappedDEK:    []byte("typed-uuid-flow-wrap"),
			FormatVersion: 1,
			KeyProvider:   "local",
			KeyVersion:    1,
		},
		Status:    "pending",
		CreatedAt: now,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil || flow.UserUUID != "" || flow.UUID == "" {
		t.Fatalf("create MCP OAuth flow with nullable typed user UUID = (%+v, %v)", flow, err)
	}

	builtin, builtinVersion, err := app.db.UpsertBuiltinSkillWithVersion(ctx, db.BuiltinSkill{
		ExternalID:   "builtin_typed_uuid_" + suffix,
		DisplayTitle: "typed UUID",
		CreatedAt:    now,
	}, db.BuiltinSkillVersion{
		ExternalID:  "builtinver_typed_uuid_" + suffix,
		Version:     "1.0.0",
		Name:        "typed-uuid",
		Description: "PostgreSQL UUID boundary",
		Directory:   "typed-uuid",
		S3Bucket:    "test",
		S3Key:       "typed-uuid/" + suffix,
		SizeBytes:   1,
		SHA256:      strings.Repeat("d", 64),
		CreatedAt:   now,
	})
	if err != nil || !isValidUUID(builtin.UUID) || !isValidUUID(builtinVersion.UUID) {
		t.Fatalf("upsert builtin Skill through typed UUID rows = (%+v, %+v, %v)", builtin, builtinVersion, err)
	}
}

func isValidUUID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}

// TestTypedUUIDFilesAndFilestorePostgres exercises the final UUID migration
// surface against PostgreSQL: Files rows and cursors, storage accounting,
// filesystem provisioning, nullable Filestore entry references, expiry
// batching, and cleanup-job leases.
func TestTypedUUIDFilesAndFilestorePostgres(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("typed-uuid-files-filestore"))
	t.Cleanup(app.close)

	_, _, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID :=
		seedFilestoreLookupScope(t, app)
	ctx := context.Background()
	now := time.Now().UTC()
	suffix := uuid.NewV4().String()

	firstFile := db.FileRecord{
		UUID:                uuid.NewV4().String(),
		ExternalID:          "file_typed_uuid_first_" + suffix,
		WorkspaceUUID:       workspaceUUID,
		Filename:            "first.txt",
		MimeType:            "text/plain",
		SizeBytes:           3,
		SHA256:              strings.Repeat("1", 64),
		S3Bucket:            "typed-uuid",
		S3Key:               "files/" + suffix + "/first.txt",
		Downloadable:        true,
		CreatedByAPIKeyUUID: apiKeyUUID,
		CreatedAt:           now,
	}
	secondFile := firstFile
	secondFile.UUID = uuid.NewV4().String()
	secondFile.ExternalID = "file_typed_uuid_second_" + suffix
	secondFile.Filename = "second.txt"
	secondFile.SizeBytes = 4
	secondFile.SHA256 = strings.Repeat("2", 64)
	secondFile.S3Key = "files/" + suffix + "/second.txt"
	secondFile.CreatedAt = now.Add(time.Millisecond)
	for _, file := range []db.FileRecord{firstFile, secondFile} {
		if err := app.db.CreateFile(ctx, file); err != nil {
			t.Fatalf("create File through typed UUID parameters: %v", err)
		}
	}
	loadedFile, err := app.db.GetFileByUUIDInOrganization(ctx, organizationUUID, firstFile.UUID)
	if err != nil || loadedFile.WorkspaceUUID != workspaceUUID || loadedFile.CreatedByAPIKeyUUID != apiKeyUUID {
		t.Fatalf("get File through typed UUID row = (%+v, %v)", loadedFile, err)
	}
	firstPage, hasMore, err := app.db.ListFilesPage(ctx, db.ListFilesPageParams{
		WorkspaceUUID: workspaceUUID,
		Limit:         1,
	})
	if err != nil || !hasMore || len(firstPage) != 1 || firstPage[0].ExternalID != secondFile.ExternalID {
		t.Fatalf("first Files page through typed UUID rows = (%+v, %v, %v)", firstPage, hasMore, err)
	}
	secondPage, _, err := app.db.ListFilesPage(ctx, db.ListFilesPageParams{
		WorkspaceUUID: workspaceUUID,
		AfterID:       secondFile.ExternalID,
		Limit:         1,
	})
	if err != nil || len(secondPage) != 1 || secondPage[0].ExternalID != firstFile.ExternalID {
		t.Fatalf("Files page with typed UUID cursor = (%+v, %v)", secondPage, err)
	}

	filesystemUUID := uuid.NewV4().String()
	filesystem, created, err := app.db.ProvisionFilestoreFilesystem(ctx, db.ProvisionFilestoreFilesystemInput{
		UUID:                filesystemUUID,
		ExternalID:          "claude_chat_typed_uuid_" + strings.ReplaceAll(suffix, "-", ""),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         sessionUUID,
		CodeSessionUUID:     &codeSessionUUID,
		CreatedByAPIKeyUUID: &apiKeyUUID,
		Now:                 now,
	})
	if err != nil || !created || filesystem.UUID != filesystemUUID ||
		filesystem.CodeSessionUUID == nil || *filesystem.CodeSessionUUID != codeSessionUUID {
		t.Fatalf("provision Filestore filesystem through typed UUIDs = (%+v, %v, %v)", filesystem, created, err)
	}
	loadedFilesystem, err := app.db.GetFilestoreFilesystem(ctx, workspaceUUID, filesystem.UUID)
	if err != nil || loadedFilesystem.ExternalID != filesystem.ExternalID {
		t.Fatalf("get Filestore filesystem by typed UUID identifier = (%+v, %v)", loadedFilesystem, err)
	}

	activeResult, err := app.db.PutFilestoreFile(ctx, db.PutFilestoreFileInput{
		WorkspaceUUID:  workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		Path:           "/outputs/active.txt",
		Blob: db.FilestoreFileBlob{
			SizeBytes:             9,
			MediaType:             "text/plain",
			DetectedMimeType:      "text/plain",
			Metadata:              []byte(`{"typed_uuid":true}`),
			AuthorizationMetadata: []byte(`{}`),
			Tags:                  []string{"typed-uuid"},
			Downloadable:          true,
			MD5:                   strings.Repeat("a", 32),
			SHA256:                strings.Repeat("a", 64),
			S3Bucket:              "typed-uuid",
			S3Key:                 "filestore/" + suffix + "/active.txt",
			S3ETag:                "etag-active",
			S3VersionID:           "version-active",
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("put Filestore file through typed UUID parameters: %v", err)
	}
	if activeResult.Node.UUID == "" || activeResult.Node.SourceFileUUID != nil {
		t.Fatalf("Filestore nullable UUID row = %+v", activeResult.Node)
	}

	page, err := app.db.ListSessionResourceFilesPage(ctx, db.ListSessionResourceFilesPageParams{
		WorkspaceUUID:  workspaceUUID,
		FilesystemUUID: filesystem.UUID,
		DirectoryPath:  "/outputs",
		Recursive:      true,
		Limit:          1,
		Cursor: &db.SessionResourceFilePageCursor{
			Path: "/outputs",
			UUID: uuid.NewV4().String(),
		},
	})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].UUID != activeResult.Node.UUID {
		t.Fatalf("list Filestore entries with typed UUID cursor = (%+v, %v)", page, err)
	}

	cleanupJob, err := app.db.EnqueueFilestoreObjectCleanupJob(ctx, db.EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceUUID:   workspaceUUID,
		FilesystemUUID:  filesystem.UUID,
		EntryExternalID: activeResult.Node.ExternalID,
		Bucket:          "typed-uuid",
		Key:             "filestore/" + suffix + "/sentinel.txt",
		Reason:          "typed_uuid_integration",
		RunAfter:        now,
	})
	if err != nil || cleanupJob.WorkspaceUUID != workspaceUUID || cleanupJob.FilesystemUUID != filesystem.UUID {
		t.Fatalf("enqueue Filestore cleanup job through typed UUID row = (%+v, %v)", cleanupJob, err)
	}
	leaseToken := "typed-uuid-" + suffix
	leasedJobs, err := app.db.LeaseFilestoreObjectCleanupJobs(ctx, leaseToken, 100, 10)
	if err != nil {
		t.Fatalf("lease Filestore cleanup jobs through typed UUID rows: %v", err)
	}
	foundCleanupJob := false
	for _, job := range leasedJobs {
		if job.UUID == cleanupJob.UUID {
			foundCleanupJob = job.WorkspaceUUID == workspaceUUID && job.FilesystemUUID == filesystem.UUID
			if err := app.db.CompleteLeasedFilestoreObjectCleanupJob(ctx, job.UUID, leaseToken); err != nil {
				t.Fatalf("complete Filestore cleanup job with typed UUID parameter: %v", err)
			}
		}
	}
	if !foundCleanupJob {
		t.Fatalf("leased Filestore cleanup jobs do not contain %s: %+v", cleanupJob.UUID, leasedJobs)
	}

	storageBytes, err := app.db.GetWorkspaceStorageBytes(ctx, workspaceUUID)
	if err != nil || storageBytes != firstFile.SizeBytes+secondFile.SizeBytes+activeResult.Node.OwnedBytes() {
		t.Fatalf("workspace storage bytes = (%d, %v), want %d", storageBytes, err, 16)
	}
}

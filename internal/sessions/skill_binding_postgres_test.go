//go:build integration

package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"uuid"
)

func TestSessionCreateSkillBindingBoundaryPostgres(t *testing.T) {
	store := newReconnectPostgres(t)
	ctx := t.Context()
	workspace, org, key := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	now := time.Now().UTC()
	if _, err := store.ListenerPool().Exec(ctx, `INSERT INTO workspaces (uuid,external_id,organization_uuid,name) VALUES ($1,'wrkspc_binding',$2,'binding')`, workspace, org); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListenerPool().Exec(ctx, `INSERT INTO api_keys (uuid,external_id,workspace_uuid,key_hash) VALUES ($1,'key_binding',$2,'noncredential-test-hash')`, key, workspace); err != nil {
		t.Fatal(err)
	}
	createSkill := func(id, version string) db.SkillVersion {
		title := "display-" + id
		_, v, err := store.CreateSkillWithVersion(ctx, db.Skill{UUID: uuid.NewV4().String(), ExternalID: id, WorkspaceUUID: workspace, DisplayTitle: &title, CreatedAt: now}, db.SkillVersion{UUID: uuid.NewV4().String(), ExternalID: "skver_" + id, WorkspaceUUID: workspace, Version: version, Name: "name-" + id, Directory: "same-private-directory", S3Bucket: "private-bucket", S3Key: "private-key-" + id, SHA256: strings.Repeat("a", 64), SizeBytes: 12, CreatedAt: now}, false)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a, b := createSkill("skill_binding_a", "v1"), createSkill("skill_binding_b", "v2")
	ref := func(id, version string) json.RawMessage {
		raw, err := json.Marshal([]map[string]string{{"type": "custom", "skill_id": id, "version": version}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	createAgent := func(name string, skills, multi json.RawMessage) db.Agent {
		agent, err := store.CreateAgent(ctx, db.Agent{UUID: uuid.NewV4().String(), ExternalID: "agent_" + name, WorkspaceUUID: workspace, Name: name, Skills: skills, Multiagent: multi, Model: []byte(`{"id":"local-model"}`), MCPServers: []byte(`[]`), Tools: []byte(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false,"permission_policy":{"type":"always_allow"}},"configs":[{"type":"read","name":"read","enabled":true,"permission_policy":{"type":"always_allow"}}]}]`), Metadata: []byte(`{}`), CreatedAt: now}, "aver_"+name)
		if err != nil {
			t.Fatal(err)
		}
		return agent
	}
	good := createAgent("binding_good", ref(a.SkillExternalID, a.Version), nil)
	bad := createAgent("binding_bad", ref(a.SkillExternalID, b.ExternalID), nil)
	child := createAgent("binding_child", ref(a.SkillExternalID, b.ExternalID), nil)
	roster := json.RawMessage(`{"type":"multiagent_20261001","subagents":{"type":"enabled","inline_agents":{"type":"disabled"},"predefined_agents":[{"type":"agent","id":"` + child.ExternalID + `","version":1}]},"workflows":{"type":"disabled"}}`)
	root := createAgent("binding_root", ref(a.SkillExternalID, a.Version), roster)
	scope := "organization"
	env, err := store.CreateEnvironment(ctx, db.Environment{UUID: uuid.NewV4().String(), ExternalID: "env_binding", Provider: "e2b", Scope: &scope, OrganizationUUID: org, WorkspaceUUID: workspace, CreatedByAPIKeyUUID: key, Name: "binding", Config: []byte(`{"type":"cloud","networking":{"type":"unrestricted"}}`), Metadata: []byte(`{}`), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	h := NewHandler(config.Config{}, store, codesessions.NewServiceWithCredentials(store, creds, nil), nil, nil, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	counts := func() [6]int64 {
		var result [6]int64
		err := store.ListenerPool().QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions),(SELECT count(*) FROM session_threads),(SELECT count(*) FROM environment_work),(SELECT count(*) FROM session_resources),(SELECT count(*) FROM files),(SELECT count(*) FROM code_sessions)`).Scan(&result[0], &result[1], &result[2], &result[3], &result[4], &result[5])
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	request := func(agent any) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"agent": agent, "environment_id": env.ExternalID, "initial_events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": "must not enqueue invalid binding"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(string(body)))
		req = req.WithContext(auth.WithPrincipal(ctx, auth.Principal{CredentialType: auth.CredentialTypeAPIKey, OrganizationUUID: org, WorkspaceUUID: workspace, APIKeyUUID: key}))
		response := httptest.NewRecorder()
		h.errorAdapter.Wrap(h.create).ServeHTTP(response, req)
		return response
	}
	for name, agent := range map[string]any{
		"cross-owner-version": bad.ExternalID,
		"display-label":       map[string]any{"type": "agent_with_overrides", "id": good.ExternalID, "skills": json.RawMessage(ref("display-"+a.SkillExternalID, a.Version))},
		"invalid-override":    map[string]any{"type": "agent_with_overrides", "id": good.ExternalID, "skills": json.RawMessage(ref(a.SkillExternalID, b.Version))},
		"expanded-child":      root.ExternalID,
	} {
		t.Run(name, func(t *testing.T) {
			before := counts()
			response := request(agent)
			if response.Code != 400 || !strings.Contains(response.Body.String(), `"invalid_request_error"`) {
				t.Fatalf("invalid effective binding must reject before persistence: status%d %s", response.Code, response.Body)
			}
			if counts() != before {
				t.Fatal("rejected binding persisted Session/input/Work/resources")
			}
			for _, secret := range []string{"private-bucket", "private-key-", "same-private-directory", "name-" + b.SkillExternalID} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatal("foreign source metadata leaked")
				}
			}
		})
	}
	t.Run("unavailable-not-invalid", func(t *testing.T) {
		original := h.skillBindings
		h.skillBindings = skillsapi.NewRuntimeResolver(nil)
		defer func() { h.skillBindings = original }()
		before := counts()
		response := request(good.ExternalID)
		if response.Code != 500 || strings.Contains(response.Body.String(), "invalid_request_error") || counts() != before {
			t.Fatalf("operational fault mislabeled or persisted: %d %s", response.Code, response.Body)
		}
	})
	for name, agent := range map[string]any{
		"valid-owned-version":                good.ExternalID,
		"valid-latest":                       map[string]any{"type": "agent_with_overrides", "id": good.ExternalID, "skills": json.RawMessage(ref(a.SkillExternalID, "latest"))},
		"valid-external-version-alias":       map[string]any{"type": "agent_with_overrides", "id": good.ExternalID, "skills": json.RawMessage(ref(a.SkillExternalID, a.ExternalID))},
		"valid-override-invalid-saved-agent": map[string]any{"type": "agent_with_overrides", "id": bad.ExternalID, "skills": json.RawMessage(ref(a.SkillExternalID, a.Version))},
	} {
		t.Run(name, func(t *testing.T) {
			response := request(agent)
			if response.Code != 200 {
				t.Fatalf("valid final binding rejected: %d %s; %s", response.Code, response.Body, logs.String())
			}
		})
	}
	for _, v := range []db.SkillVersion{a, b} {
		after, err := store.GetSkillVersion(ctx, workspace, v.SkillExternalID, v.Version)
		if err != nil || !reflect.DeepEqual(after, v) {
			t.Fatal("validation changed immutable version", err)
		}
	}
	t.Run("query-only-no-flat-directory-rejection", func(t *testing.T) {
		snapshot := json.RawMessage(fmt.Sprintf(`{"skills":%s,"multiagent":{"agents":[{"skills":%s}]}}`, ref(a.SkillExternalID, a.Version), ref(b.SkillExternalID, b.Version)))
		if err := h.skillBindings.ValidateAgentSnapshotBindings(ctx, workspace, snapshot); err != nil {
			t.Fatal("existence validator applied unrelated install-directory guard", err)
		}
	})
	t.Run("foreign-workspace", func(t *testing.T) {
		err := h.skillBindings.ValidateAgentSnapshotBindings(ctx, uuid.NewV4().String(), json.RawMessage(`{"skills":`+string(ref(a.SkillExternalID, a.Version))+`}`))
		var binding *skillsapi.BindingReferenceError
		if !errors.As(err, &binding) {
			t.Fatal("foreign workspace binding must be invalid without ownership leak", err)
		}
	})
	t.Run("scoped-same-skill-versions", func(t *testing.T) {
		_, second, err := store.CreateSkillVersion(ctx, workspace, a.SkillExternalID, db.SkillVersion{UUID: uuid.NewV4().String(), ExternalID: "skver_binding_a_second", Version: "v3", Name: a.Name, Directory: a.Directory, S3Bucket: a.S3Bucket, S3Key: "private-second-key", SHA256: strings.Repeat("b", 64), SizeBytes: 13, CreatedAt: now.Add(time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		snapshot := json.RawMessage(fmt.Sprintf(`{"skills":%s,"multiagent":{"agents":[{"id":"agent_worker","version":1,"name":"worker","skills":%s}]}}`, ref(a.SkillExternalID, "latest"), ref(a.SkillExternalID, a.Version)))
		resolved, err := h.skillBindings.ResolveAgentSnapshot(ctx, workspace, snapshot)
		if err != nil || len(resolved) != 2 {
			t.Fatalf("different scopes require independent versions: %+v, %v", resolved, err)
		}
		if resolved[0].VersionUUID != second.UUID || resolved[1].VersionUUID != a.UUID {
			t.Fatal("resolved wrong immutable version")
		}
		catalogs, err := skillsapi.RuntimeCatalogs(snapshot, resolved)
		if err != nil || len(catalogs[""].Skills) != 1 || catalogs[""].Skills[0].VersionUUID != second.UUID || len(catalogs["worker"].Skills) != 1 || catalogs["worker"].Skills[0].VersionUUID != a.UUID {
			t.Fatal("catalog scope mismatch", err)
		}
		response := request(good.ExternalID)
		var created struct {
			ID string `json:"id"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &created) != nil {
			t.Fatal("valid archive fixture Session failed")
		}
		inputs := make([]db.SessionSkillArchiveResourceInput, 0, 2)
		for _, value := range resolved {
			inputs = append(inputs, db.SessionSkillArchiveResourceInput{Source: value.Source, SkillVersionUUID: value.VersionUUID, Directory: value.Directory, InstallDirectory: value.MountDirectory(), S3Bucket: value.S3Bucket, S3Key: value.S3Key, SizeBytes: value.SizeBytes, SHA256: value.SHA256})
		}
		if err := store.ReplaceSessionSkillArchiveResources(ctx, workspace, created.ID, inputs); err != nil {
			t.Fatal("version-aware archive projection failed", err)
		}
		filesystem, err := store.GetFilestoreFilesystemBySession(ctx, workspace, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := store.ListSessionSkillArchiveResources(ctx, workspace, filesystem.UUID)
		if err != nil || len(entries) != 2 {
			t.Fatal("archive projections absent", err)
		}
		for _, entry := range entries {
			var metadata struct {
				Directory string `json:"skill_package_directory"`
			}
			if json.Unmarshal(entry.Metadata, &metadata) != nil || metadata.Directory != a.Directory || entry.Path == "/skills/"+a.Directory {
				t.Fatal("package root confused with mount identity")
			}
		}
		before := append([]db.SessionResourceFile(nil), entries...)
		forged := append([]db.SessionSkillArchiveResourceInput(nil), inputs...)
		forged[0].Directory = "wrong-catalog-directory"
		if err := store.ReplaceSessionSkillArchiveResources(ctx, workspace, created.ID, forged); err == nil {
			t.Fatal("forged package directory accepted")
		}
		after, err := store.ListSessionSkillArchiveResources(ctx, workspace, filesystem.UUID)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("failed replacement changed immutable projections", err)
		}

	})
	t.Run("cancel-is-operational", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		err := h.skillBindings.ValidateAgentSnapshotBindings(canceled, workspace, json.RawMessage(`{"skills":`+string(ref(a.SkillExternalID, a.Version))+`}`))
		var binding *skillsapi.BindingReferenceError
		if err == nil || errors.As(err, &binding) {
			t.Fatal("canceled lookup misclassified as invalid binding", err)
		}
	})
}

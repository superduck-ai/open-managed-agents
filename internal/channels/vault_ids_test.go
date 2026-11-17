package channels

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type vaultListStore struct{ fakeLocalStore }

func (s *vaultListStore) GetVault(_ context.Context, workspace, id string) (db.Vault, error) {
	if workspace != "workspace-a" || !slices.Contains([]string{"vault-a", "vault-b", "archived"}, id) {
		return db.Vault{}, db.ErrNotFound
	}
	vault := db.Vault{ExternalID: id}
	if id == "archived" {
		vault.ArchivedAt = new(time.Now())
	}
	return vault, nil
}

func TestChannelVaultListValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, workspace string
		valid                 bool
		want                  []string
	}{
		{"非数组", `{"vault_ids":"vault-a"}`, "workspace-a", false, nil},
		{"空标识", `{"vault_ids":["vault-a",""]}`, "workspace-a", false, nil},
		{"空白标识", `{"vault_ids":[" "]}`, "workspace-a", false, nil},
		{"重复", `{"vault_ids":["vault-a","vault-a"]}`, "workspace-a", false, nil},
		{"部分不存在", `{"vault_ids":["vault-a","missing"]}`, "workspace-a", false, nil},
		{"部分归档", `{"vault_ids":["vault-a","archived"]}`, "workspace-a", false, nil},
		{"其他工作区", `{"vault_ids":["vault-a"]}`, "workspace-b", false, nil},
		{"新旧字段冲突", `{"vault_ids":["vault-a"],"vault_id":"vault-b"}`, "workspace-a", false, nil},
		{"多个", `{"vault_ids":["vault-a","vault-b"]}`, "workspace-a", true, []string{"vault-a", "vault-b"}},
		{"空数组清除", `{"vault_ids":[]}`, "workspace-a", true, []string{}},
		{"空值清除", `{"vault_ids":null}`, "workspace-a", true, []string{}},
		{"省略保留旧配置", `{}`, "workspace-a", true, []string{"vault-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input channelMutation
			if err := json.Unmarshal([]byte(tc.body), &input); err != nil {
				t.Fatal(err)
			}
			service := newLocalService(&vaultListStore{}, nil, nil, nil)
			record := db.ChannelRecord{Mounts: `{"vault_id":"vault-a"}`}
			err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: tc.workspace}, &record, input)
			if (err == nil) != tc.valid {
				t.Fatalf("Vault整批校验结果: %v", err)
			}
			if !tc.valid {
				if record.Mounts != `{"vault_id":"vault-a"}` {
					t.Fatal("无效配置改变了已保存的Vault")
				}
				return
			}
			bundle, err := channelSessionResources(t.Context(), nil, record)
			if err != nil {
				t.Fatal(err)
			}
			ids := bundle.VaultIDs
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("会话Vault=%v，期望%v", ids, tc.want)
			}
			if got, ok := channelMountView(record)["vault_ids"].([]string); !ok || !slices.Equal(got, tc.want) {
				t.Fatalf("回显Vault不完整: %v", channelMountView(record))
			}
		})
	}
}

func TestChannelVaultListCompatibility(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused.test")
	worker.record.Mounts = `{"vault_ids":["vault-a","vault-b"]}`
	session := omaSession{VaultIDs: []string{"vault-a", "vault-b"}}
	if !worker.compatibleMounts(session) {
		t.Fatal("完整相同的Vault列表不能复用")
	}
	session.VaultIDs = []string{"vault-a"}
	if worker.compatibleMounts(session) {
		t.Fatal("部分Vault会话不应被复用")
	}
}

func TestManuallyMountedOwnVaultIsNotAttachedTwice(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused.test")
	worker.supervisor.personalVault = func(context.Context, db.ChannelRecord, string) (string, error) { return "vault-a", nil }
	worker.record.Mounts = `{"vault_ids":["vault-a"]}`
	session := omaSession{VaultIDs: []string{"vault-a"}, Metadata: map[string]string{"chat_type": "p2p", "actor_id": "user-a", "personal_vault_id": "vault-a", "credential_binding_mode": "first_vault"}}
	if !worker.compatibleMounts(session) {
		t.Fatal("手动挂载本人 Vault 不应重复追加同一资源")
	}
}

func TestLegacyVaultUpdateDoesNotDiscardMultipleVaults(t *testing.T) {
	service := newLocalService(&vaultListStore{}, nil, nil, nil)
	for _, raw := range []string{`{"vault_id":null}`, `{"vault_id":"vault-a"}`} {
		record := db.ChannelRecord{Mounts: `{"vault_ids":["vault-a","vault-b"]}`}
		var input channelMutation
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
		if err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: "workspace-a"}, &record, input); err == nil {
			t.Fatal("旧单值字段不应静默覆盖多个Vault")
		}
		bundle, err := channelSessionResources(t.Context(), nil, record)
		if err != nil {
			t.Fatal(err)
		}
		ids := bundle.VaultIDs
		if !slices.Equal(ids, []string{"vault-a", "vault-b"}) {
			t.Fatalf("原Vault丢失: %v", ids)
		}
	}
	record := db.ChannelRecord{Mounts: `{"vault_ids":["vault-a","vault-b"]}`}
	if err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: "workspace-a"}, &record, channelMutation{}); err != nil {
		t.Fatal(err)
	}
	bundle, err := channelSessionResources(t.Context(), nil, record)
	if err != nil {
		t.Fatal(err)
	}
	ids := bundle.VaultIDs
	if !slices.Equal(ids, []string{"vault-a", "vault-b"}) {
		t.Fatal("省略字段未保留完整列表")
	}
}

func TestChannelCreatesSessionWithAllVaultIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			VaultIDs []string `json:"vault_ids"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil || !slices.Equal(body.VaultIDs, []string{"vault-a", "vault-b"}) {
			t.Errorf("原生创建请求丢失Vault: %+v %v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"sesn_vaults"}`))
	}))
	defer server.Close()
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	worker.record.Mounts = `{"vault_ids":["vault-a","vault-b"]}`
	if _, err := worker.sessionFor(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1"}, "chat:oc_1"); err != nil {
		t.Fatal(err)
	}
}

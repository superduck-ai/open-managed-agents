package channels

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type versionLocalStore struct{ fakeLocalStore }

func (f *versionLocalStore) GetAgentVersion(_ context.Context, workspace, agent string, version int) (db.Agent, error) {
	if workspace != "workspace-a" || agent != "agent-a" || version != 2 {
		return db.Agent{}, db.ErrNotFound
	}
	return db.Agent{ExternalID: agent, CurrentVersion: version}, nil
}

func TestChannelAgentVersionValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, workspace string
		valid                 bool
		want                  int
	}{
		{"零版本", `{"agent_version":0}`, "workspace-a", false, 0},
		{"负版本", `{"agent_version":-1}`, "workspace-a", false, 0},
		{"字符串", `{"agent_version":"2"}`, "workspace-a", false, 0},
		{"小数", `{"agent_version":2.5}`, "workspace-a", false, 0},
		{"不存在版本", `{"agent_version":99}`, "workspace-a", false, 0},
		{"其他工作区", `{"agent_version":2}`, "workspace-b", false, 0},
		{"固定版本", `{"agent_version":2}`, "workspace-a", true, 2},
		{"省略保留", `{}`, "workspace-a", true, 2},
		{"清除恢复默认", `{"agent_version":null}`, "workspace-a", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input channelMutation
			if err := json.Unmarshal([]byte(tc.body), &input); err != nil {
				t.Fatal(err)
			}
			store := &versionLocalStore{}
			service := newLocalService(store, nil, nil, nil)
			record := db.ChannelRecord{AgentExternalID: "agent-a", Mounts: `{"agent_version":2}`}
			err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: tc.workspace}, &record, input)
			if (err == nil) != tc.valid {
				t.Fatalf("版本校验结果: %v", err)
			}
			if !tc.valid {
				return
			}
			var value struct {
				AgentVersion *int `json:"agent_version"`
			}
			encoded, err := json.Marshal(channelMountView(record))
			if err != nil || json.Unmarshal(encoded, &value) != nil {
				t.Fatal("版本响应无效")
			}
			if tc.want == 0 && value.AgentVersion != nil || tc.want != 0 && (value.AgentVersion == nil || *value.AgentVersion != tc.want) {
				t.Fatalf("保存与回显版本不一致: %s", encoded)
			}
		})
	}
}

func TestPinnedChannelVersionCreatesNativeSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Agent struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"agent"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil || body.Agent.ID != "agent_1" || body.Agent.Version != 2 {
			t.Errorf("没有通过原生会话合同固定版本: %+v %v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"sesn_pinned"}`))
	}))
	defer server.Close()
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	worker.record.Mounts = `{"agent_version":2}`
	if _, err := worker.sessionFor(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1"}, "chat:oc_1"); err != nil {
		t.Fatal(err)
	}
	old := omaSession{Agent: omaSessionAgent{ID: "agent_1", Version: 1}, EnvironmentID: "env_1"}
	if worker.compatibleSessionConfiguration(old, "env_1", "") {
		t.Fatal("固定版本错误复用了其他版本的会话")
	}
	old.Agent.Version = 2
	old.Metadata = map[string]string{"agent_version": "2"}
	if !worker.compatibleSessionConfiguration(old, "env_1", "") {
		t.Fatal("相同固定版本不能复用")
	}
	worker.record.Mounts = `{}`
	if worker.compatibleSessionConfiguration(old, "env_1", "") {
		t.Fatal("改回最新版错误复用了固定版本绑定")
	}
}

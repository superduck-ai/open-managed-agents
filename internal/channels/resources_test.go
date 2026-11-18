package channels

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type resourceLocalStore struct{ fakeLocalStore }

func (f *resourceLocalStore) GetFile(_ context.Context, workspace, id string) (db.FileRecord, error) {
	if workspace != "workspace-a" || id != "file_a" && id != "file_b" {
		return db.FileRecord{}, db.ErrNotFound
	}
	return db.FileRecord{ExternalID: id, WorkspaceUUID: workspace}, nil
}

func TestChannelResourceArrayValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, workspace string
		valid                 bool
		count                 int
	}{
		{"非数组", `{"resources":{}}`, "workspace-a", false, 0},
		{"未知类型", `{"resources":[{"type":"other"}]}`, "workspace-a", false, 0},
		{"部分文件不存在", `{"resources":[{"type":"file","file_id":"file_a"},{"type":"file","file_id":"file_missing"}]}`, "workspace-a", false, 0},
		{"文件跨工作区", `{"resources":[{"type":"file","file_id":"file_a"}]}`, "workspace-b", false, 0},
		{"文件路径冲突", `{"resources":[{"type":"file","file_id":"file_a","mount_path":"/uploads/data"},{"type":"file","file_id":"file_b","mount_path":"/uploads/data/x"}]}`, "workspace-a", false, 0},
		{"仓库身份重复", `{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git","mount_path":"/workspace/a"},{"type":"github_repository","url":"https://gitlab.example/a.git","mount_path":"/workspace/b"}]}`, "workspace-a", false, 0},
		{"仓库目录重叠", `{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git","mount_path":"/workspace/a"},{"type":"github_repository","url":"https://gitlab.example/b.git","mount_path":"/workspace/a/b"}]}`, "workspace-a", false, 0},
		{"混用旧字段", `{"resources":[],"git_repository":null}`, "workspace-a", false, 0},
		{"两个文件", `{"resources":[{"type":"file","file_id":"file_a"},{"type":"file","file_id":"file_b"}]}`, "workspace-a", true, 2},
		{"文件与两个仓库", `{"resources":[{"type":"file","file_id":"file_a"},{"type":"github_repository","url":"https://gitlab.example/a.git"},{"type":"github_repository","url":"https://gitlab.example/b.git","checkout":{"type":"branch","name":"main"}}]}`, "workspace-a", true, 3},
		{"清空", `{"resources":[]}`, "workspace-a", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input channelMutation
			if err := json.Unmarshal([]byte(tc.body), &input); err != nil {
				t.Fatal(err)
			}
			service := testLocalService(&resourceLocalStore{}, testCipher(t), nil, nil)
			record := db.ChannelRecord{Mounts: `{}`}
			err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: tc.workspace}, &record, input)
			if (err == nil) != tc.valid {
				t.Fatalf("资源整批校验结果: %v", err)
			}
			if !tc.valid {
				if record.Mounts != `{}` {
					t.Fatal("失败保存改变了原配置")
				}
				return
			}
			var stored struct {
				Resources []jsontext.Value `json:"resources"`
			}
			if err := json.Unmarshal([]byte(record.Mounts), &stored); err != nil || len(stored.Resources) != tc.count {
				t.Fatalf("完整资源未保存: %s %v", record.Mounts, err)
			}
		})
	}
}

func TestChannelResourceTokensAreNotPlaintext(t *testing.T) {
	service := testLocalService(&resourceLocalStore{}, testCipher(t), nil, nil)
	record := db.ChannelRecord{ExternalID: "channel-a", OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a", Mounts: `{}`}
	var input channelMutation
	if err := json.Unmarshal([]byte(`{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git","authorization_token":"token-first"},{"type":"github_repository","url":"https://gitlab.example/b.git","authorization_token":"token-second"}]}`), &input); err != nil {
		t.Fatal(err)
	}
	if err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: "workspace-a"}, &record, input); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(record.Mounts, "token-first") || strings.Contains(record.Mounts, "token-second") {
		t.Fatal("资源配置包含明文令牌")
	}
	var stored struct {
		Resources []jsontext.Value `json:"resources"`
	}
	if err := json.Unmarshal([]byte(record.Mounts), &stored); err != nil || len(stored.Resources) != 2 {
		t.Fatalf("带独立凭据的两个仓库没有保存: %v", err)
	}
}

func applyResourceMutation(t *testing.T, service *localService, record *db.ChannelRecord, raw string) {
	t.Helper()
	var input channelMutation
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	if err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: "workspace-a"}, record, input); err != nil {
		t.Fatal(err)
	}
}

func TestChannelResourceCredentialsFollowRepository(t *testing.T) {
	cipher := testCipher(t)
	service := testLocalService(&resourceLocalStore{}, cipher, nil, nil)
	record := db.ChannelRecord{ExternalID: "channel-a", OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a", Mounts: `{}`}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git","authorization_token":"token-first"},{"type":"github_repository","url":"https://gitlab.example/b.git","authorization_token":"token-second"}]}`)
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/b.git","mount_path":"/workspace/moved-b"},{"type":"github_repository","url":"https://gitlab.example/a.git"}]}`)
	bundle, err := channelSessionResources(t.Context(), cipher, record)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Resources[0]["authorization_token"] != "token-second" || bundle.Resources[1]["authorization_token"] != "token-first" {
		t.Fatal("重排或修改路径导致仓库凭据丢失或串用")
	}
	previousFingerprint := bundle.Fingerprint
	view, err := json.Marshal(channelMountView(record))
	if err != nil || strings.Contains(string(view), "token-first") || strings.Contains(string(view), "token-second") || strings.Contains(string(view), "token_envelope") || strings.Contains(string(view), "ciphertext") {
		t.Fatal("资源响应泄漏凭据或信封")
	}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/b.git","mount_path":"/workspace/moved-b","authorization_token":"token-replaced"},{"type":"github_repository","url":"https://gitlab.example/a.git"}]}`)
	bundle, err = channelSessionResources(t.Context(), cipher, record)
	if err != nil || bundle.Fingerprint == previousFingerprint || bundle.Resources[0]["authorization_token"] != "token-replaced" || bundle.Resources[1]["authorization_token"] != "token-first" {
		t.Fatal("替换一个凭据未更新配置身份或影响了另一仓库")
	}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/b.git","authorization_token":null}]}`)
	bundle, err = channelSessionResources(t.Context(), cipher, record)
	if err != nil || len(bundle.Resources) != 1 || bundle.Resources[0]["authorization_token"] != nil {
		t.Fatal("明确清除凭据或移除仓库未生效")
	}
}

func TestLegacyResourceMigrationPreservesSessionUntilCredentialsChange(t *testing.T) {
	cipher := testCipher(t)
	service := testLocalService(&resourceLocalStore{}, cipher, nil, nil)
	record := db.ChannelRecord{ExternalID: "channel-a", OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a", Mounts: `{"git_repository":{"url":"https://gitlab.example/a.git"}}`}
	envelope, err := sealChannelSecret(t.Context(), cipher, record, "old-token")
	if err != nil {
		t.Fatal(err)
	}
	record.GitTokenEnvelope = envelope
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused.test")
	worker.record = record
	session := omaSession{Resources: []omaSessionResource{{Type: "github_repository", URL: "https://gitlab.example/a.git", MountPath: "/workspace/a"}}}
	if !worker.compatibleMounts(session, "") {
		t.Fatal("旧单仓库会话在迁移前不兼容")
	}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git"}]}`)
	worker.record = record
	if !worker.compatibleMounts(session, "") {
		t.Fatal("保存未变化资源错误打断了旧会话")
	}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git","authorization_token":"new-token"}]}`)
	worker.record = record
	if worker.compatibleMounts(session, "") {
		t.Fatal("凭据替换后仍复用持有旧凭据的会话")
	}
}

func TestConfiguredFilesRemainDistinctFromConversationAttachments(t *testing.T) {
	service := testLocalService(&resourceLocalStore{}, testCipher(t), nil, nil)
	record := db.ChannelRecord{Mounts: `{}`}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"file","file_id":"file_a"}]}`)
	bundle, err := channelSessionResources(t.Context(), nil, record)
	if err != nil {
		t.Fatal(err)
	}
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused.test")
	worker.record = record
	session := omaSession{Metadata: map[string]string{"channel_resources": bundle.Fingerprint}, Resources: []omaSessionResource{
		{Type: "file", FileID: "file_session_clone_a", MountPath: "/mnt/session/uploads/file_a"},
		{Type: "file", FileID: "file_chat_attachment", MountPath: "/mnt/session/uploads/message_attachment"},
	}}
	session.Resources[0].MountPath = "/mnt/session/uploads/other"
	if worker.compatibleMounts(session, "") {
		t.Fatal("配置文件实际路径不匹配时不应复用")
	}
	session.Resources[0].MountPath = "/mnt/session/uploads/file_a"
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"file","file_id":"file_b","mount_path":"/uploads/file_a"}]}`)
	worker.record = record
	if worker.compatibleMounts(session, "") {
		t.Fatal("相同路径改用另一源文件时不应复用")
	}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"file","file_id":"file_a"}]}`)
	worker.record = record
	if !worker.compatibleMounts(session, "") {
		t.Fatal("原生会话文件副本和聊天附件不应使未变的配置失配")
	}
	applyResourceMutation(t, service, &record, `{"resources":[]}`)
	worker.record = record
	if worker.compatibleMounts(session, "") {
		t.Fatal("移除配置文件后不应复用仍带该文件的会话")
	}
}

func TestChannelResourcesUseNativeBodyAndResourceLimits(t *testing.T) {
	items := make([]map[string]string, 500)
	for i := range items {
		items[i] = map[string]string{"type": "file", "file_id": "file_a", "mount_path": fmt.Sprintf("/uploads/%03d_%s", i, strings.Repeat("x", 100))}
	}
	data, err := json.Marshal(map[string]any{"resources": items})
	if err != nil || len(data) <= 32<<10 {
		t.Fatal("夹具没有覆盖原32KiB限制")
	}
	input, err := decodeChannelMutation(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(string(data))))
	if err != nil {
		t.Fatal(err)
	}
	service := testLocalService(&resourceLocalStore{}, nil, nil, nil)
	record := db.ChannelRecord{Mounts: `{}`}
	if err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: "workspace-a"}, &record, input); err != nil {
		t.Fatal(err)
	}
	if len(parseChannelMounts(record.Mounts).Resources) != 500 {
		t.Fatal("没有保存原生支持的完整资源数组")
	}
	before := record.Mounts
	input = channelMutation{MemoryStoreID: []byte(`"memory-a"`)}
	if err := service.applyChannelMounts(t.Context(), auth.Principal{WorkspaceUUID: "workspace-a"}, &record, input); err == nil || record.Mounts != before {
		t.Fatal("新增Memory后超过原生总资源上限，仍保存了部分配置")
	}
}

func TestConfiguredResourceSecretsRejectOtherChannelBinding(t *testing.T) {
	cipher := testCipher(t)
	service := testLocalService(&resourceLocalStore{}, cipher, nil, nil)
	record := db.ChannelRecord{ExternalID: "channel-a", OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a", Mounts: `{}`}
	applyResourceMutation(t, service, &record, `{"resources":[{"type":"github_repository","url":"https://gitlab.example/a.git","authorization_token":"private-test-token"}]}`)
	record.ExternalID = "channel-other"
	if bundle, err := channelSessionResources(t.Context(), cipher, record); err == nil || len(bundle.Resources) != 0 {
		t.Fatal("其他渠道的信封不应被解密或降级为空凭据继续创建会话")
	}
}

func TestFileResourcePreflightChecksAllRuntimePermissions(t *testing.T) {
	for _, denySecond := range []bool{false, true} {
		t.Run(fmt.Sprint(denySecond), func(t *testing.T) {
			files := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != "key" {
					t.Error("预检没有使用原运行凭据进行只读查询")
				}
				id := strings.TrimPrefix(r.URL.Path, "/v1/agents/")
				if strings.HasPrefix(r.URL.Path, "/v1/environments/") {
					id = "env"
				}
				if strings.HasPrefix(r.URL.Path, "/v1/files/") {
					id = strings.TrimPrefix(r.URL.Path, "/v1/files/")
					files = append(files, id)
					if denySecond && id == "file_b" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
			}))
			defer server.Close()
			runtime := testRuntimeSupervisor(&fakeRuntimeStore{env: "env"}, nil, newOMAClient(server.URL), nil)
			worker := &channelWorker{}
			worker.online.Store(true)
			runtime.workers["channel"] = worker
			result := runtime.check(t.Context(), db.ChannelRecord{ExternalID: "channel", AgentExternalID: "agent", Enabled: true, BindingStatus: "bound", Mounts: `{"resources":[{"type":"file","file_id":"file_a","mount_path":"/uploads/a"},{"type":"file","file_id":"file_b","mount_path":"/uploads/b"}]}`})
			if result.OK == denySecond || result.ExecutionVerified || len(files) != 2 {
				t.Fatalf("文件部分无权时错误放行，或未逐项检查: ok=%v files=%v", result.OK, files)
			}
		})
	}
}

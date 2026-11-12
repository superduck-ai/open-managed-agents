package channels

import (
	"encoding/json"
	"strings"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func scopeMessage(raw map[string]any) *channeltypes.NormalizedMessage {
	return &channeltypes.NormalizedMessage{ChatID: "oc_1", RawEvent: raw}
}

func TestChannelScopeIsolatesThreads(t *testing.T) {
	chat := scopeMessage(map[string]any{"chat_id": "oc_1"})
	thread := scopeMessage(map[string]any{"chat_id": "oc_1", "thread_id": "omt_1"})
	other := scopeMessage(map[string]any{"chat_id": "oc_1", "thread_id": "omt_2"})
	if channelScopeKey(chat) != "chat:oc_1" {
		t.Fatalf("chat scope = %q", channelScopeKey(chat))
	}
	if channelScopeKey(thread) != "thread:omt_1" {
		t.Fatalf("thread scope = %q", channelScopeKey(thread))
	}
	if channelScopeKey(thread) == channelScopeKey(other) {
		t.Fatal("不同话题必须使用不同的会话作用域")
	}
}

func TestChannelSessionResourcesFromMounts(t *testing.T) {
	record := db.ChannelRecord{Mounts: `{"memory_store_id":"mem_1","vault_id":"vlt_1","git_repository":{"url":"https://example.com/repo.git","mount_path":"/repo","checkout":{"type":"branch","name":"main"}}}`}
	resources, vaultIDs := channelSessionResources(record, "token_1")
	if len(resources) != 2 || len(vaultIDs) != 1 || vaultIDs[0] != "vlt_1" {
		t.Fatalf("resources = %v, vaults = %v", resources, vaultIDs)
	}
	if resources[0]["type"] != "memory_store" || resources[0]["memory_store_id"] != "mem_1" {
		t.Fatalf("memory resource = %v", resources[0])
	}
	git := resources[1]
	if git["type"] != "github_repository" || git["url"] != "https://example.com/repo.git" || git["authorization_token"] != "token_1" || git["mount_path"] != "/repo" {
		t.Fatalf("git resource = %v", git)
	}
	if _, ok := git["checkout"].(map[string]any); !ok {
		t.Fatalf("git checkout = %v", git["checkout"])
	}
}

func TestChannelMountViewHidesToken(t *testing.T) {
	record := db.ChannelRecord{Mounts: `{"git_repository":{"url":"https://example.com/repo.git"}}`}
	record.GitTokenEnvelope = new(string)
	view := channelMountView(record)
	git, ok := view["git_repository"].(map[string]any)
	if !ok {
		t.Fatalf("git view = %v", view["git_repository"])
	}
	if _, leaked := git["authorization_token"]; leaked {
		t.Fatal("视图不能返回 git token")
	}
	if git["tokenConfigured"] != true {
		t.Fatalf("tokenConfigured = %v", git["tokenConfigured"])
	}
}

func TestApprovalCardCarriesSessionAndTool(t *testing.T) {
	card := approvalCard("tool_1", "sesn_1", "Bash", `{"command":"ls"}`)
	for _, want := range []string{`"kind":"confirm"`, `"session_id":"sesn_1"`, `"tool_use_id":"tool_1"`, `"result":"allow"`, `"result":"deny"`, "允许", "拒绝"} {
		if !strings.Contains(card, want) {
			t.Fatalf("卡片缺少 %s: %s", want, card)
		}
	}
}

func TestQuestionCardOffersOptions(t *testing.T) {
	questions := parseQuestions(json.RawMessage(`{"questions":[{"id":"q1","question":"用哪个分支？","options":[{"label":"main"},{"label":"dev"}]}]}`))
	if len(questions) != 1 || len(questions[0].Options) != 2 {
		t.Fatalf("questions = %v", questions)
	}
	card := questionCard("tool_1", "sesn_1", questions)
	for _, want := range []string{"用哪个分支？", `"question_id":"q1"`, `"selected":["main"]`} {
		if !strings.Contains(card, want) {
			t.Fatalf("卡片缺少 %s: %s", want, card)
		}
	}
}

func TestDecodeActionValue(t *testing.T) {
	value, err := decodeActionValue(map[string]any{"kind": "answer", "session_id": "sesn_1", "tool_use_id": "tool_1", "question_id": "q1", "selected": []any{"A"}})
	if err != nil || value.Kind != "answer" || len(value.Selected) != 1 || value.Selected[0] != "A" {
		t.Fatalf("decode = %+v, %v", value, err)
	}
}

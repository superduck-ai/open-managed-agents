package channels

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestQuestionSubmissionChecksPersistedQuestion(t *testing.T) {
	for _, kind := range []string{"空答案", "无效选项", "单选多值", "错误类型", "缺少问卷", "超长文本", "单选", "多选去重", "多选自定义优先"} {
		t.Run(kind, func(t *testing.T) {
			bodies := &omaRecorder{}
			multi := kind == "多选去重" || kind == "多选自定义优先"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					bodies.record(string(body))
					_, _ = w.Write([]byte(`{}`))
					return
				}
				if kind == "缺少问卷" {
					_, _ = w.Write([]byte(`{"data":[{"id":"card_input","type":"user.message"}]}`))
					return
				}
				question := map[string]any{"id": "q", "question": "请选择颜色", "multi_select": multi, "options": []map[string]string{{"label": "红色"}, {"label": "蓝色"}}}
				body, _ := json.Marshal(map[string]any{"data": []map[string]any{{"id": "card_input", "type": "user.message"}, {"id": "question", "type": "agent.custom_tool_use", "name": "AskUserQuestion", "input": map[string]any{"questions": []any{question}}}}})
				_, _ = w.Write(body)
			}))
			t.Cleanup(server.Close)
			form := map[string]any{"q": "红色"}
			switch kind {
			case "空答案":
				form = map[string]any{}
			case "无效选项":
				form["q"] = "紫色"
			case "单选多值", "多选自定义优先":
				form["q"] = []any{"红色", "蓝色"}
			case "错误类型":
				form["q"] = []any{1}
			case "超长文本":
				form["q_custom"] = strings.Repeat("字", 10001)
			case "多选去重":
				form["q"] = []any{"红色", "蓝色", "红色"}
			}
			if kind == "多选自定义优先" {
				form["q_custom"] = " 青色 "
			}
			worker, channel := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
			seedCardActionReceipt(worker, "card_input")
			value := map[string]any{"kind": "answer", "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": "question", "open_id": "ou_1", "multi": []string{"q"}}
			event := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: value, FormValue: form}}
			err := worker.handleCardAction(t.Context(), event)
			posts := bodies.snapshot()
			valid := kind == "单选" || multi
			if !valid {
				if err != nil || len(posts) != 0 || len(channel.text()) != 1 {
					t.Fatalf("无效回答应确认回调并提示用户，不能提交: err=%v posts=%d replies=%v", err, len(posts), channel.text())
				}
				return
			}
			if err != nil || len(posts) != 1 {
				t.Fatalf("有效回答应提交一次: err=%v posts=%d", err, len(posts))
			}
			var result struct {
				Events []struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"events"`
			}
			if err := json.Unmarshal([]byte(posts[0]), &result); err != nil {
				t.Fatal(err)
			}
			var answer struct {
				Answers []struct {
					Selected []string `json:"selected"`
					Custom   string   `json:"custom"`
				} `json:"answers"`
			}
			if len(result.Events) != 1 || len(result.Events[0].Content) != 1 {
				t.Fatalf("问卷提交事件结构无效: %s", posts[0])
			}
			text := result.Events[0].Content[0].Text
			if err := json.Unmarshal([]byte(text), &answer); err != nil {
				t.Fatal(err)
			}
			got := answer.Answers[0]
			if kind == "多选自定义优先" && (len(got.Selected) != 0 || got.Custom != "青色") || kind == "多选去重" && len(got.Selected) != 2 {
				t.Fatalf("应使用原问卷的多选规则，去重且自定义优先: %s", text)
			}
		})
	}
}

func TestQuestionOptionsKeepDescription(t *testing.T) {
	questions := parseQuestions([]byte(`{"questions":[{"id":"q","question":"选择","options":[{"label":"标准","description":"适合日常使用"}]}]}`))
	body, err := json.Marshal(questionElements(channelActionValue{}, questions))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "适合日常使用") {
		t.Fatalf("问卷必须保留选项说明: %s", body)
	}
}

func TestMalformedQuestionDoesNotSilentlyDropFields(t *testing.T) {
	for _, input := range []string{
		`{"questions":[{"id":"q","question":"选择","options":[{"label":"A"}]},{"id":"q2","question":"选择","options":[{"label":""}]}]}`,
		`{"questions":[{"id":"q","question":"选择","options":null}]}`,
		`{"questions":[{"id":"q","question":"  ","options":[]}]}`,
		`{"questions":[{"id":"q","question":"选择","options":[]},{"id":"q","question":"第二题","options":[]}]}`,
	} {
		if questions := parseQuestions([]byte(input)); len(questions) != 0 {
			t.Fatalf("无效结构应完整交给控制台，不能丢掉题目或选项后继续提问: %v", questions)
		}
	}
}

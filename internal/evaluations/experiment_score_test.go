package evaluations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestSessionEndTurnAttachesLLMScore(t *testing.T) {
	store := readyExperimentStore()
	store.evaluatorVersions = []db.EvaluationEvaluatorVersion{
		{
			UUID: "00000000-0000-4000-8000-000000000031", ExternalID: "eev_b", EvaluatorUUID: "00000000-0000-4000-8000-000000000021",
			EvaluatorType: evaluatorTypeLLM, Model: "claude", PromptTemplate: "Score {{input}} / {{output}}", Variables: []string{"input", "output"},
		},
		{
			UUID: "00000000-0000-4000-8000-000000000032", ExternalID: "eev_a", EvaluatorUUID: "00000000-0000-4000-8000-000000000022",
			EvaluatorType: evaluatorTypeAgent, AgentID: "agent_score",
		},
	}
	store.sessionEvents = []db.SessionEvent{
		{EventType: "user.message", Payload: []byte(`{"content":[{"type":"text","text":"hello"}]}`)},
		{EventType: "agent.message", Payload: []byte(`{"content":[{"type":"text","text":"hi"}]}`)},
	}
	model := &recordingEvaluatorModel{score: EvaluatorScore{Score: 0.75, Explanation: "matches", Details: json.RawMessage(`{"pass":true}`)}}
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"}).WithEvaluatorModel(model)
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), experimentCreateBody(), "")
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if model.calls != 1 || model.prompt != "Score hello / hi" {
		t.Fatalf("model calls=%d prompt=%q", model.calls, model.prompt)
	}
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].ErrorMessage != "" {
		t.Fatalf("case status=%s item=%#v", store.experiment.Status, store.experiment.Items[0])
	}
	loaded := serveEvaluation(evaluationRouterWith(handler), http.MethodGet, evaluationExperimentPath("/"+store.experiment.ExternalID), "", "")
	var body experimentResponse
	if err := json.Unmarshal(loaded.Body.Bytes(), &body); err != nil || loaded.Code != http.StatusOK {
		t.Fatalf("get = %d %s", loaded.Code, loaded.Body.String())
	}
	if body.Status != db.EvaluationExperimentStatusCompleted || len(body.Items) != 1 || body.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("body status=%s items=%#v", body.Status, body.Items)
	}
	if len(body.Scores) != 2 {
		t.Fatalf("scores = %#v", body.Scores)
	}
	llm := scoreByVersion(body.Scores, "eev_b")
	agent := scoreByVersion(body.Scores, "eev_a")
	if llm.Status != db.EvaluationExperimentScoreStatusCompleted || llm.Score == nil || *llm.Score != 0.75 || llm.Explanation != "matches" || string(llm.Details) != `{"pass":true}` {
		t.Fatalf("llm score = %#v", llm)
	}
	if agent.Status != db.EvaluationExperimentScoreStatusFailed || agent.Score != nil || agent.Error != agentScoringUnavailableMessage {
		t.Fatalf("agent score = %#v", agent)
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if model.calls != 1 || len(store.experiment.Scores) != 2 {
		t.Fatalf("rescored calls=%d scores=%d", model.calls, len(store.experiment.Scores))
	}
}

func TestSessionEndTurnKeepsCaseCompletedWhenScoringFails(t *testing.T) {
	store := readyExperimentStore()
	store.evaluators = store.evaluators[:1]
	store.evaluatorVersions = []db.EvaluationEvaluatorVersion{{
		UUID: "00000000-0000-4000-8000-000000000031", ExternalID: "eev_b", EvaluatorUUID: "00000000-0000-4000-8000-000000000021",
		EvaluatorType: evaluatorTypeLLM, Model: "claude", PromptTemplate: "Score {{input}}", Variables: []string{"input"},
	}}
	model := &recordingEvaluatorModel{err: errEvaluatorScoreFailed}
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"}).WithEvaluatorModel(model)
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_b"]}`, "")
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("status=%s item=%s", store.experiment.Status, store.experiment.Items[0].Status)
	}
	if len(store.experiment.Scores) != 1 || store.experiment.Scores[0].Status != db.EvaluationExperimentScoreStatusFailed || store.experiment.Scores[0].ErrorMessage != evaluatorScoreFailedMessage {
		t.Fatalf("scores = %#v", store.experiment.Scores)
	}
}

func TestRenderEvaluatorPromptAndParseScore(t *testing.T) {
	prompt := renderEvaluatorPrompt("{{input}} {{output}} {{checkpoints}}", []string{"input", "output", "checkpoints"}, scoreValues{
		Input: "ask", Output: "answer", Checkpoints: `["done"]`,
	})
	if prompt != "ask answer [\"done\"]" {
		t.Fatalf("prompt = %q", prompt)
	}
	values := scoreValuesFrom(db.EvaluationCase{Checkpoints: json.RawMessage(`["done"]`)}, []db.SessionEvent{
		{EventType: "agent.tool_result", Payload: []byte(`{"content":[{"type":"text","text":"ran"}]}`)},
		{EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`)},
	})
	if values.InvocationResult != "ran" || values.Checkpoints != `["done"]` {
		t.Fatalf("tool result = %#v", values)
	}
	fallback := scoreValuesFrom(db.EvaluationCase{}, []db.SessionEvent{
		{EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`)},
	})
	if fallback.InvocationResult != "end_turn" {
		t.Fatalf("stop reason = %#v", fallback)
	}
	score, err := parseEvaluatorScoreText("```json\n{\"score\":0,\"explanation\":\"no\",\"details\":{\"k\":\"v\"}}\n```")
	if err != nil || score.Score != 0 || score.Explanation != "no" || string(score.Details) != `{"k":"v"}` {
		t.Fatalf("score = (%#v, %v)", score, err)
	}
	if _, err := parseEvaluatorScoreText(`{"explanation":"missing"}`); err == nil {
		t.Fatal("missing score parsed")
	}
	text, err := evaluatorScoreText(llmproviders.ChatCompletions, []byte(`{"choices":[{"message":{"content":"{\"score\":1}"}}]}`))
	if err != nil || text != `{"score":1}` {
		t.Fatalf("chat text = (%q, %v)", text, err)
	}
}

func TestLLMEvaluatorModelReadsMessagesScore(t *testing.T) {
	var sawKey bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("X-Api-Key") == "" {
			t.Errorf("path=%s key_set=%v", r.URL.Path, r.Header.Get("X-Api-Key") != "")
		}
		sawKey = r.Header.Get("X-Api-Key") == "secret"
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "secret") {
			t.Errorf("request body included the api key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"score\":1,\"explanation\":\"yes\",\"details\":{}}"}]}`))
	}))
	defer server.Close()
	model := &LLMEvaluatorModel{client: server.Client()}
	score, err := model.complete(context.Background(), llmproviders.Upstream{
		BaseURL: server.URL, APIKey: "secret", ModelID: "claude", APIBackend: llmproviders.Messages,
	}, "grade the answer")
	if err != nil || !sawKey || score.Score != 1 || score.Explanation != "yes" {
		t.Fatalf("score = (%#v, %v)", score, err)
	}
}

type recordingEvaluatorModel struct {
	calls  int
	prompt string
	score  EvaluatorScore
	err    error
}

func (m *recordingEvaluatorModel) Score(_ context.Context, request EvaluatorScoreRequest) (EvaluatorScore, error) {
	m.calls++
	m.prompt = request.Prompt
	if m.err != nil {
		return EvaluatorScore{}, m.err
	}
	return m.score, nil
}

func scoreByVersion(scores []experimentScoreResponse, versionID string) experimentScoreResponse {
	for _, score := range scores {
		if score.VersionID == versionID {
			return score
		}
	}
	return experimentScoreResponse{}
}

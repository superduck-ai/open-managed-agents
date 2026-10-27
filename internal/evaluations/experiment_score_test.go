package evaluations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestSessionEndTurnIgnoresRemovedPresetEvaluator(t *testing.T) {
	store := readyExperimentStore()
	model := &recordingEvaluatorModel{score: EvaluatorScore{Score: 0.5, Explanation: "preset", Details: json.RawMessage(`{}`)}}
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"}).WithEvaluatorModel(model)
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_oma_latency"]}`, "")
	if created.Code != http.StatusNotFound || !strings.Contains(created.Body.String(), evaluatorNotFoundMessage) || store.createExperimentCalls != 0 || model.calls != 0 {
		t.Fatalf("create = %d calls=%d model=%d body=%s", created.Code, store.createExperimentCalls, model.calls, created.Body.String())
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
	if fallback.InvocationResult != "end_turn" || !strings.Contains(fallback.AgentTrajectory, "session.status_idle: stop_reason=end_turn") {
		t.Fatalf("stop reason = %#v", fallback)
	}
	start := time.Date(2026, 10, 27, 1, 38, 26, 0, time.UTC)
	traced := scoreValuesFrom(db.EvaluationCase{}, []db.SessionEvent{
		{EventType: "session.status_running", CreatedAt: start},
		{EventType: "user.message", CreatedAt: start, Payload: []byte(`{"content":[{"type":"text","text":"hello"}]}`)},
		{EventType: "span.model_request_start", CreatedAt: start.Add(time.Second), Payload: []byte(`{"model":"claude"}`)},
		{EventType: "agent.thinking", CreatedAt: start.Add(2 * time.Second)},
		{EventType: "agent.message", CreatedAt: start.Add(3 * time.Second), Payload: []byte(`{"content":[{"type":"text","text":"hi"}]}`)},
		{EventType: "span.model_request_end", CreatedAt: start.Add(4 * time.Second), Payload: []byte(`{"model":"claude","model_usage":{"input_tokens":3,"output_tokens":4}}`)},
		{EventType: "session.usage", CreatedAt: start.Add(5 * time.Second), Payload: []byte(`{"usage":{"input_tokens":3,"output_tokens":4,"list_cost":{"amount":"1","currency":"USD"},"active_seconds":5}}`)},
		{EventType: "session.status_idle", CreatedAt: start.Add(5 * time.Second), Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`)},
	})
	if traced.Input != "hello" || traced.Output != "hi" {
		t.Fatalf("text = %#v", traced)
	}
	for _, want := range []string{
		"2026-10-27T01:38:26Z session.status_running",
		"2026-10-27T01:38:26Z user.message: hello",
		"2026-10-27T01:38:27Z span.model_request_start: model=claude",
		"2026-10-27T01:38:28Z agent.thinking",
		"2026-10-27T01:38:30Z span.model_request_end: model=claude input_tokens=3 output_tokens=4",
		"2026-10-27T01:38:31Z session.usage: input_tokens=3 output_tokens=4 list_cost=1 active_seconds=5",
		"2026-10-27T01:38:31Z session.status_idle: stop_reason=end_turn",
	} {
		if !strings.Contains(traced.AgentTrajectory, want) {
			t.Fatalf("trajectory missing %q\n%s", want, traced.AgentTrajectory)
		}
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

func TestSessionEndTurnFailsAgentScoreWhenAgentIsMissing(t *testing.T) {
	store, handler, sandbox := startAgentScoreExperiment(t, &agentScoreSandboxStub{sessionID: "sesn_score"}, nil, false)
	store.agent = db.Agent{}
	store.frozenAgents = nil
	observeEndTurn(handler, "sesn_case")
	if sandbox.starts != 0 {
		t.Fatalf("starts = %d", sandbox.starts)
	}
	assertAgentScoreFailure(t, store, agentNotFoundMessage)
}

func TestSessionEndTurnKeepsCaseCompletedWhenAgentSandboxFails(t *testing.T) {
	store, handler, sandbox := startAgentScoreExperiment(t, &agentScoreSandboxStub{sessionID: "sesn_score", startErr: errEvaluatorScoreFailed}, nil, false)
	observeEndTurn(handler, "sesn_case")
	if sandbox.starts != 1 {
		t.Fatalf("starts = %d", sandbox.starts)
	}
	assertAgentScoreFailure(t, store, evaluatorScoreFailedMessage)
	observeEndTurn(handler, "sesn_case")
	if sandbox.starts != 1 || len(store.experiment.Scores) != 1 {
		t.Fatalf("rescored starts=%d scores=%d", sandbox.starts, len(store.experiment.Scores))
	}
}

func TestMissingScoreFileIsRereadBeforeFailure(t *testing.T) {
	sandbox := &agentScoreSandboxStub{sessionID: "sesn_score", readErr: ErrAgentScoreFileMissing}
	store, handler, jobs := startScoreSessionWithJobs(t, sandbox)
	observeEndTurn(handler, "sesn_score")
	if len(store.experiment.Scores) != 0 || jobs.fileCalls != 1 || jobs.fileAttempt != 1 || jobs.fileSessionID != "sesn_score" {
		t.Fatalf("scores=%#v calls=%d attempt=%d session=%s", store.experiment.Scores, jobs.fileCalls, jobs.fileAttempt, jobs.fileSessionID)
	}
	sandbox.readErr = nil
	sandbox.scoreJSON = []byte(`{"score":1,"explanation":"late","details":{}}`)
	handler.readDeferredScoreFile(context.Background(), scoreFileArgs{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionID: "sesn_score", Attempt: 1,
	})
	if len(store.experiment.Scores) != 1 {
		t.Fatalf("scores = %#v", store.experiment.Scores)
	}
	score := store.experiment.Scores[0]
	if score.Status != db.EvaluationExperimentScoreStatusCompleted || score.Score == nil || *score.Score != 1 || score.Explanation != "late" || score.ErrorMessage != "" {
		t.Fatalf("score = %#v", score)
	}
}

func TestMissingScoreFileFailsOnLastReread(t *testing.T) {
	sandbox := &agentScoreSandboxStub{sessionID: "sesn_score", readErr: ErrAgentScoreFileMissing}
	store, handler, jobs := startScoreSessionWithJobs(t, sandbox)
	handler.readDeferredScoreFile(context.Background(), scoreFileArgs{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionID: "sesn_score", Attempt: scoreFileReadAttempts - 1,
	})
	assertAgentScoreFailure(t, store, agentScoreFileMissingMessage)
	if jobs.fileCalls != 0 {
		t.Fatalf("file calls = %d", jobs.fileCalls)
	}
}

func startScoreSessionWithJobs(t *testing.T, sandbox *agentScoreSandboxStub) (*evaluationStoreStub, *Handler, *recordingScoreEnqueuer) {
	t.Helper()
	store, handler, _ := startAgentScoreExperiment(t, sandbox, nil, false)
	jobs := &recordingScoreEnqueuer{}
	handler.WithScoreJobs(jobs)
	observeEndTurn(handler, "sesn_case")
	if err := handler.scoreQueuedItem(context.Background(), scoreItemArgs{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", ExperimentID: store.experiment.ExternalID,
		ItemID: store.experiment.Items[0].ExternalID, SessionID: "sesn_case",
	}); err != nil || sandbox.starts != 1 {
		t.Fatalf("start score err=%v starts=%d", err, sandbox.starts)
	}
	return store, handler, jobs
}

func TestSessionEndTurnKeepsCaseCompletedWhenAgentScoreFileIsMissing(t *testing.T) {
	store, handler, _ := startAgentScoreExperiment(t, &agentScoreSandboxStub{sessionID: "sesn_score", readErr: ErrAgentScoreFileMissing}, nil, false)
	observeEndTurn(handler, "sesn_case")
	if len(store.experiment.Scores) != 0 {
		t.Fatalf("scores before scorer finished = %#v", store.experiment.Scores)
	}
	observeEndTurn(handler, "sesn_score")
	assertAgentScoreFailure(t, store, agentScoreFileMissingMessage)
	observeEndTurn(handler, "sesn_score")
	if len(store.experiment.Scores) != 1 {
		t.Fatalf("rescored scores = %#v", store.experiment.Scores)
	}
}

func TestSessionEndTurnKeepsCaseCompletedWhenAgentScoreIsInvalid(t *testing.T) {
	store, handler, _ := startAgentScoreExperiment(t, &agentScoreSandboxStub{sessionID: "sesn_score", scoreJSON: []byte(`{"explanation":"missing"}`)}, nil, false)
	observeEndTurn(handler, "sesn_case")
	observeEndTurn(handler, "sesn_score")
	assertAgentScoreFailure(t, store, evaluatorScoreParseMessage)
}

func TestSessionEndTurnAttachesAgentScore(t *testing.T) {
	sandbox := &agentScoreSandboxStub{sessionID: "sesn_score", scoreJSON: []byte(`{"score":0.5,"explanation":"grounded","details":{"pass":true}}`)}
	store, handler, _ := startAgentScoreExperiment(t, sandbox, nil, false)
	store.evaluatorVersions[0].EnvironmentID = "env_score"
	store.evaluatorVersions[0].Resources = json.RawMessage(`[{"type":"file","file_id":"file_a","mount_path":"/data/rubric.md"}]`)
	observeEndTurn(handler, "sesn_case")
	if sandbox.starts != 1 {
		t.Fatalf("starts = %d", sandbox.starts)
	}
	input := sandbox.last
	if input.AgentExternalID != "agent_score" || input.AgentVersion != 7 || input.EnvironmentExternalID != "env_score" || string(input.Resources) != `[{"type":"file","file_id":"file_a","mount_path":"/data/rubric.md"}]` || input.UserUUID != "user-uuid" || input.TimeoutSeconds != 60 || len(input.Env) != 0 {
		t.Fatalf("score session = %#v", input)
	}
	if input.Metadata[agentScoreRoleKey] != agentScoreRole || input.Metadata[agentScoreItemKey] == "" {
		t.Fatalf("metadata = %#v", input.Metadata)
	}
	if len(input.Turns) != 1 || !strings.Contains(input.Turns[0], evaluatorResultPath) || !strings.Contains(input.Turns[0], evaluatorScorePath) || !strings.Contains(input.Turns[0], agentScoreSandboxResultPath) || !strings.Contains(input.Turns[0], agentScoreSandboxOutputPath) || !strings.Contains(input.Turns[0], "OMA rubric for hi") {
		t.Fatalf("turns = %#v", input.Turns)
	}
	if !strings.Contains(string(input.ResultJSON), `"session_id":"sesn_case"`) || !strings.Contains(string(input.ResultJSON), `"input":"hello"`) || !strings.Contains(string(input.ResultJSON), `"output":"hi"`) || !strings.Contains(string(input.ResultJSON), `"checkpoints":["ready"]`) || !strings.Contains(string(input.ResultJSON), `"reference_files":["guide.txt"]`) || !strings.Contains(string(input.ResultJSON), `"input_files":["in.txt"]`) {
		t.Fatalf("result = %s", input.ResultJSON)
	}
	observeEndTurn(handler, "sesn_score")
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("status=%s item=%s", store.experiment.Status, store.experiment.Items[0].Status)
	}
	if len(store.experiment.Scores) != 1 {
		t.Fatalf("scores = %#v", store.experiment.Scores)
	}
	score := store.experiment.Scores[0]
	if score.Status != db.EvaluationExperimentScoreStatusCompleted || score.Score == nil || *score.Score != 0.5 || score.Explanation != "grounded" || string(score.Details) != `{"pass":true}` || score.ErrorMessage != "" {
		t.Fatalf("score = %#v", score)
	}
	observeEndTurn(handler, "sesn_case")
	observeEndTurn(handler, "sesn_score")
	if sandbox.starts != 1 || len(store.experiment.Scores) != 1 {
		t.Fatalf("rescored starts=%d scores=%d", sandbox.starts, len(store.experiment.Scores))
	}
}

func TestSessionEndTurnScoresLLMAndAgent(t *testing.T) {
	model := &recordingEvaluatorModel{score: EvaluatorScore{Score: 0.75, Explanation: "matches", Details: json.RawMessage(`{"pass":true}`)}}
	sandbox := &agentScoreSandboxStub{sessionID: "sesn_score", scoreJSON: []byte(`{"score":0.25,"explanation":"agent","details":{}}`)}
	store, handler, _ := startAgentScoreExperiment(t, sandbox, model, true)
	observeEndTurn(handler, "sesn_case")
	if model.calls != 1 || model.prompt != "Score hello / hi" || sandbox.starts != 1 || len(store.experiment.Scores) != 1 {
		t.Fatalf("llm calls=%d prompt=%q starts=%d scores=%d", model.calls, model.prompt, sandbox.starts, len(store.experiment.Scores))
	}
	observeEndTurn(handler, "sesn_score")
	loaded := serveEvaluation(evaluationRouterWith(handler), http.MethodGet, evaluationExperimentPath("/"+store.experiment.ExternalID), "", "")
	var body experimentResponse
	if err := json.Unmarshal(loaded.Body.Bytes(), &body); err != nil || loaded.Code != http.StatusOK {
		t.Fatalf("get = %d %s", loaded.Code, loaded.Body.String())
	}
	llm := scoreByVersion(body.Scores, "eev_b")
	agent := scoreByVersion(body.Scores, "eev_a")
	if llm.Status != db.EvaluationExperimentScoreStatusCompleted || llm.Score == nil || *llm.Score != 0.75 {
		t.Fatalf("llm score = %#v", llm)
	}
	if agent.Status != db.EvaluationExperimentScoreStatusCompleted || agent.Score == nil || *agent.Score != 0.25 || agent.Explanation != "agent" || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("agent score = %#v item=%s", agent, store.experiment.Items[0].Status)
	}
	if sandbox.last.TimeoutSeconds != 60 || string(sandbox.last.Env) != `{"TRACE":"1"}` || sandbox.last.AgentVersion != 7 {
		t.Fatalf("score session = %#v", sandbox.last)
	}
}

func TestAgentScoreSessionUsesFrozenAgentVersion(t *testing.T) {
	sandbox := &agentScoreSandboxStub{sessionID: "sesn_score", scoreJSON: []byte(`{"score":1,"explanation":"frozen","details":{}}`)}
	store, handler, _ := startAgentScoreExperiment(t, sandbox, nil, false)
	var frozen int
	for _, binding := range store.createdExperiment.Bindings {
		if binding.EvaluatorExternalID == "evalev_a" {
			frozen = binding.AgentVersion
		}
	}
	if frozen != 7 {
		t.Fatalf("frozen version = %d", frozen)
	}
	store.agent.CurrentVersion = 9
	observeEndTurn(handler, "sesn_case")
	if sandbox.starts != 1 || sandbox.last.AgentVersion != 7 || sandbox.last.Title != "评分 · 账单摘要" || sandbox.last.TimeoutSeconds != 60 {
		t.Fatalf("score session = %#v starts=%d", sandbox.last, sandbox.starts)
	}
}

func startAgentScoreExperiment(t *testing.T, sandbox *agentScoreSandboxStub, model EvaluatorModel, withLLM bool) (*evaluationStoreStub, *Handler, *agentScoreSandboxStub) {
	t.Helper()
	store := readyExperimentStore()
	store.agent = db.Agent{ExternalID: "agent_score", CurrentVersion: 7}
	store.frozenAgents = []db.Agent{{ExternalID: "agent_score", CurrentVersion: 7}}
	for index := range store.evaluators {
		if store.evaluators[index].ExternalID != "evalev_a" {
			continue
		}
		store.evaluators[index].Version.EvaluatorType = evaluatorTypeAgent
		store.evaluators[index].Version.AgentID = "agent_score"
	}
	store.versionCases[0].Checkpoints = json.RawMessage(`["ready"]`)
	store.versionCases[0].InputFiles = json.RawMessage(`["in.txt"]`)
	store.versionCases[0].ReferenceFiles = json.RawMessage(`["guide.txt"]`)
	store.sessionEvents = []db.SessionEvent{
		{EventType: "user.message", Payload: []byte(`{"content":[{"type":"text","text":"hello"}]}`)},
		{EventType: "agent.message", Payload: []byte(`{"content":[{"type":"text","text":"hi"}]}`)},
	}
	agentVersion := db.EvaluationEvaluatorVersion{
		UUID: "00000000-0000-4000-8000-000000000032", ExternalID: "eev_a", EvaluatorUUID: "00000000-0000-4000-8000-000000000022",
		EvaluatorType: evaluatorTypeAgent, AgentID: "agent_score", PromptTemplate: "OMA rubric for {{output}}", Variables: []string{"output"},
	}
	body := `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_a"]}`
	store.evaluatorVersions = []db.EvaluationEvaluatorVersion{agentVersion}
	if withLLM {
		store.evaluatorVersions = []db.EvaluationEvaluatorVersion{
			{
				UUID: "00000000-0000-4000-8000-000000000031", ExternalID: "eev_b", EvaluatorUUID: "00000000-0000-4000-8000-000000000021",
				EvaluatorType: evaluatorTypeLLM, Model: "claude", PromptTemplate: "Score {{input}} / {{output}}", Variables: []string{"input", "output"},
			},
			agentVersion,
		}
		body = experimentCreateBody()
	}
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"}).WithAgentScoreSandbox(sandbox)
	if model != nil {
		handler = handler.WithEvaluatorModel(model)
	}
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), body, "")
	if created.Code != http.StatusOK {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	return store, handler, sandbox
}

func observeEndTurn(handler *Handler, sessionID string) {
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: sessionID,
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
}

func assertAgentScoreFailure(t *testing.T, store *evaluationStoreStub, message string) {
	t.Helper()
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].ErrorMessage != "" {
		t.Fatalf("status=%s item=%#v", store.experiment.Status, store.experiment.Items[0])
	}
	if len(store.experiment.Scores) != 1 || store.experiment.Scores[0].Status != db.EvaluationExperimentScoreStatusFailed || store.experiment.Scores[0].Score != nil || store.experiment.Scores[0].ErrorMessage != message {
		t.Fatalf("scores = %#v", store.experiment.Scores)
	}
}

type agentScoreSandboxStub struct {
	sessionID string
	startErr  error
	readErr   error
	scoreJSON []byte
	starts    int
	last      AgentScoreInput
	claims    map[string]AgentScoreClaim
}

func (s *agentScoreSandboxStub) Start(_ context.Context, input AgentScoreInput) (string, error) {
	s.starts++
	s.last = input
	if s.startErr != nil {
		return "", s.startErr
	}
	raw, err := json.Marshal(input.Metadata)
	if err != nil {
		return "", err
	}
	claim, ok := AgentScoreClaimFromMetadata(raw)
	if !ok {
		return "", errEvaluatorScoreFailed
	}
	claim.OrganizationUUID = input.OrganizationUUID
	claim.WorkspaceUUID = input.WorkspaceUUID
	if s.claims == nil {
		s.claims = map[string]AgentScoreClaim{}
	}
	s.claims[s.sessionID] = claim
	return s.sessionID, nil
}

func (s *agentScoreSandboxStub) Claim(_ context.Context, _, _, sessionID string) (AgentScoreClaim, bool, error) {
	claim, ok := s.claims[sessionID]
	return claim, ok, nil
}

func (s *agentScoreSandboxStub) ReadScore(context.Context, string, string, string) ([]byte, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	if len(s.scoreJSON) == 0 {
		return nil, ErrAgentScoreFileMissing
	}
	return append([]byte(nil), s.scoreJSON...), nil
}

func TestSessionEndTurnQueuesScoreAndKeepsExistingRows(t *testing.T) {
	store := readyExperimentStore()
	store.evaluatorVersions = []db.EvaluationEvaluatorVersion{{
		UUID: "00000000-0000-4000-8000-000000000031", ExternalID: "eev_b", EvaluatorUUID: "00000000-0000-4000-8000-000000000021",
		EvaluatorType: evaluatorTypeLLM, Model: "claude", PromptTemplate: "Score {{input}}", Variables: []string{"input"},
	}}
	model := &recordingEvaluatorModel{score: EvaluatorScore{Score: 1, Explanation: "ok"}}
	jobs := &recordingScoreEnqueuer{}
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"}).WithEvaluatorModel(model).WithScoreJobs(jobs)
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_b"]}`, "")
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_case",
		EventType: "session.status_idle", Payload: []byte(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if model.calls != 0 || jobs.calls != 1 || jobs.itemID != store.experiment.Items[0].ExternalID {
		t.Fatalf("calls=%d enqueue=%d item=%s", model.calls, jobs.calls, jobs.itemID)
	}
	if store.experiment.Status != db.EvaluationExperimentStatusCompleted || store.experiment.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("status=%s item=%s", store.experiment.Status, store.experiment.Items[0].Status)
	}
	if err := handler.scoreQueuedItem(context.Background(), scoreItemArgs{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", ExperimentID: store.experiment.ExternalID,
		ItemID: store.experiment.Items[0].ExternalID, SessionID: "sesn_case",
	}); err != nil || model.calls != 1 || len(store.experiment.Scores) != 1 {
		t.Fatalf("score err=%v calls=%d scores=%#v", err, model.calls, store.experiment.Scores)
	}
	if err := handler.scoreQueuedItem(context.Background(), scoreItemArgs{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", ExperimentID: store.experiment.ExternalID,
		ItemID: store.experiment.Items[0].ExternalID, SessionID: "sesn_case",
	}); err != nil || model.calls != 1 {
		t.Fatalf("rescored err=%v calls=%d", err, model.calls)
	}
	jobs.calls = 0
	store.missingScores = []db.EvaluationScoreWork{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", ExperimentExternalID: store.experiment.ExternalID,
		ItemExternalID: store.experiment.Items[0].ExternalID, SessionExternalID: "sesn_case",
	}}
	if err := handler.rescueUnscoredItems(context.Background()); err != nil || jobs.calls != 1 || jobs.itemID != store.experiment.Items[0].ExternalID || store.reconcileCalls != 1 {
		t.Fatalf("rescue err=%v calls=%d item=%s reconcile=%d", err, jobs.calls, jobs.itemID, store.reconcileCalls)
	}
}

func TestScoringWaitsUntilEveryCaseFinishes(t *testing.T) {
	store := readyExperimentStore()
	store.evaluatorVersions = []db.EvaluationEvaluatorVersion{{
		UUID: "00000000-0000-4000-8000-000000000031", ExternalID: "eev_b", EvaluatorUUID: "00000000-0000-4000-8000-000000000021",
		EvaluatorType: evaluatorTypeLLM, Model: "claude", PromptTemplate: "Score {{input}}", Variables: []string{"input"},
	}}
	model := &recordingEvaluatorModel{score: EvaluatorScore{Score: 1, Explanation: "ok"}}
	jobs := &recordingScoreEnqueuer{}
	handler := NewHandler(store, nil).WithSessionStarter(&evaluationSessionStarterStub{sessionID: "sesn_case"}).WithEvaluatorModel(model).WithScoreJobs(jobs)
	created := serveEvaluation(evaluationRouterWith(handler), http.MethodPost, evaluationExperimentPath(""), `{"name":"nightly","dataset_version_id":"edv_test","agent_id":"agent_pinned","agent_version":4,"environment_id":"env_test","timeout_seconds":60,"evaluator_ids":["evalev_b"]}`, "")
	if created.Code != http.StatusOK {
		t.Fatal(created.Body.String())
	}
	finishedID := store.experiment.Items[0].ExternalID
	store.experiment.Items = append(store.experiment.Items, db.EvaluationExperimentItem{
		ExternalID: "evalitem_later", SessionExternalID: "sesn_later", Status: db.EvaluationExperimentStatusRunning,
	})
	observeEndTurn(handler, "sesn_case")
	if jobs.calls != 0 || model.calls != 0 || store.experiment.Status != db.EvaluationExperimentStatusRunning {
		t.Fatalf("early calls=%d model=%d status=%s", jobs.calls, model.calls, store.experiment.Status)
	}
	early := scoreItemArgs{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", ExperimentID: store.experiment.ExternalID,
		ItemID: finishedID, SessionID: "sesn_case",
	}
	if err := handler.scoreQueuedItem(context.Background(), early); err != nil || model.calls != 0 {
		t.Fatalf("queued early err=%v calls=%d", err, model.calls)
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", SessionExternalID: "sesn_later",
		EventType: "session.status_terminated",
	}})
	if jobs.calls != 1 || jobs.itemID != finishedID || store.experiment.Status != db.EvaluationExperimentStatusFailed || store.experiment.Items[1].Status != db.EvaluationExperimentStatusFailed {
		t.Fatalf("enqueue=%d item=%s status=%s later=%s", jobs.calls, jobs.itemID, store.experiment.Status, store.experiment.Items[1].Status)
	}
	if err := handler.scoreQueuedItem(context.Background(), early); err != nil || model.calls != 1 || len(store.experiment.Scores) != 1 {
		t.Fatalf("score err=%v calls=%d scores=%#v", err, model.calls, store.experiment.Scores)
	}
}

type recordingScoreEnqueuer struct {
	calls         int
	itemID        string
	fileCalls     int
	fileAttempt   int
	fileSessionID string
}

func (e *recordingScoreEnqueuer) EnqueueItem(_ context.Context, _, _, _, itemID, _ string) error {
	e.calls++
	e.itemID = itemID
	return nil
}

func (e *recordingScoreEnqueuer) EnqueueScoreFile(_ context.Context, _, _, sessionID string, attempt int) error {
	e.fileCalls++
	e.fileAttempt = attempt
	e.fileSessionID = sessionID
	return nil
}

func scoreByVersion(scores []experimentScoreResponse, versionID string) experimentScoreResponse {
	for _, score := range scores {
		if score.VersionID == versionID {
			return score
		}
	}
	return experimentScoreResponse{}
}

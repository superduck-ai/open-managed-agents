//go:build e2e

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const agentScoreSandboxBody = `{"score":0.91,"explanation":"contract path","details":{"source":"e2e"}}`

func TestAgentScoreSandboxContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real E2B agent score sandbox test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	requireFullE2BBridgeConfig(t, cfg)
	if cfg.EnvironmentRunner.AgentMode == "brain" {
		t.Log("clearing environment_runner.agent_mode=brain so this test creates the scoring sandbox in process")
		cfg.EnvironmentRunner.AgentMode = ""
	}
	if cfg.E2B.RequestTimeout < 2*time.Minute {
		cfg.E2B.RequestTimeout = 2 * time.Minute
	}
	if cfg.E2B.SandboxTimeout < 2*time.Minute {
		cfg.E2B.SandboxTimeout = 2 * time.Minute
	}

	app := newTestApp(t, &cfg)
	defer app.close()
	if quickstartLooksLikeLoopbackURL(cfg.E2B.APIURL) {
		app.cfg.CodeSession.SandboxAPIBaseURL = ""
	}
	quickstartEnsureSandboxIngress(t, app)
	cfg = app.cfg

	suffix := uniqueMemoryStoreSuffix(t)
	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Agent Score Sandbox `+suffix+`"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{
		"name":"agent-score-sandbox-`+suffix+`",
		"config":{"type":"cloud","networking":{"type":"unrestricted"}}
	}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)

	cookies := app.platformLoginCookies(t, "agent-score-sandbox-"+suffix+"@example.com")
	orgID := responseCookie(cookies, "lastActiveOrg").Value
	prefix := "/api/console/organizations/" + orgID + "/workspaces/default"

	var dataset struct {
		ID      string `json:"id"`
		Version struct {
			ID string `json:"id"`
		} `json:"version"`
	}
	postEvaluationConsole(t, app, cookies, prefix+"/evaluation-datasets", `{"name":"score sandbox `+suffix+`","desc":"e2e"}`, &dataset)
	postEvaluationConsole(t, app, cookies, prefix+"/evaluation-datasets/"+dataset.ID+"/cases", `{"name":"ready","turns":["reply ready"]}`, nil)

	var evaluator struct {
		ID      string `json:"id"`
		Version struct {
			ID string `json:"id"`
		} `json:"version"`
	}
	postEvaluationConsole(t, app, cookies, prefix+"/evaluation-evaluators", `{
		"name":"score sandbox judge `+suffix+`",
		"evaluator_type":"agent",
		"agent_id":`+quoteJSON(agent.ID)+`,
		"variables":["output"]
	}`, &evaluator)

	var created evaluationExperimentPayload
	postEvaluationConsole(t, app, cookies, prefix+"/evaluation-experiments", `{
		"name":"score sandbox `+suffix+`",
		"dataset_version_id":`+quoteJSON(dataset.Version.ID)+`,
		"agent_id":`+quoteJSON(agent.ID)+`,
		"environment_id":`+quoteJSON(environment.ID)+`,
		"timeout_seconds":600,
		"env":{"OMA_EVAL_SCORE":"1"},
		"evaluator_ids":[`+quoteJSON(evaluator.ID)+`]
	}`, &created)
	if len(created.Items) != 1 || created.Items[0].SessionID == "" {
		t.Fatalf("experiment items = %+v", created.Items)
	}
	caseSessionID := created.Items[0].SessionID

	var caseSandbox, scoreSandbox *memorySandboxSession
	defer func() {
		caseSandbox.stop(t, app, environment.ID)
		scoreSandbox.stop(t, app, environment.ID)
	}()

	caseSandbox = startEvaluationSandbox(t, ctx, app, cfg, environment.ID, caseSessionID)
	postEvaluationSessionEndTurn(t, app, caseSessionID)
	scoreSessionID := waitForAgentScoreSession(t, app, created.ID, caseSessionID)
	scoreSandbox = startEvaluationSandbox(t, ctx, app, cfg, environment.ID, scoreSessionID)
	stopSandboxAgent(t, ctx, scoreSandbox.sandbox)

	dataLink := sandboxReadlink(t, ctx, scoreSandbox.sandbox, "/data")
	outputLink := sandboxReadlink(t, ctx, scoreSandbox.sandbox, "/output")
	if dataLink != "/mnt/session/uploads/data" || outputLink != "/mnt/user-data/outputs" {
		t.Fatalf("score links data=%q output=%q", dataLink, outputLink)
	}
	resultBody := waitE2BFileContent(t, ctx, scoreSandbox.sandbox, "/data/session/result.json")
	if !strings.Contains(resultBody, caseSessionID) {
		t.Fatalf("result.json = %s", resultBody)
	}
	envValue := sandboxCommandOutput(t, ctx, scoreSandbox.sandbox, "printenv OMA_EVAL_SCORE")
	if envValue != "1" {
		t.Fatalf("OMA_EVAL_SCORE = %q", envValue)
	}

	writeSandboxFile(t, ctx, scoreSandbox.sandbox, "/output/score.json", agentScoreSandboxBody)
	workspaceUUID := sessionWorkspaceUUID(t, app, scoreSessionID)
	waitForFilestoreScore(t, app, workspaceUUID, scoreSessionID)
	postEvaluationSessionEndTurn(t, app, scoreSessionID)

	var settled evaluationExperimentPayload
	getEvaluationConsole(t, app, cookies, prefix+"/evaluation-experiments/"+created.ID, &settled)
	if len(settled.Items) != 1 || settled.Items[0].Status != db.EvaluationExperimentStatusCompleted {
		t.Fatalf("items = %+v", settled.Items)
	}
	if len(settled.Scores) != 1 || settled.Scores[0].Status != db.EvaluationExperimentScoreStatusCompleted || settled.Scores[0].Error != "" || settled.Scores[0].Explanation != "contract path" || settled.Scores[0].Score == nil || math.Abs(*settled.Scores[0].Score-0.91) > 0.000001 {
		t.Fatalf("scores = %+v", settled.Scores)
	}
}

type evaluationExperimentPayload struct {
	ID    string `json:"id"`
	Items []struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
		Error     string `json:"error"`
	} `json:"items"`
	Scores []struct {
		Status      string   `json:"status"`
		Score       *float64 `json:"score"`
		Explanation string   `json:"explanation"`
		Error       string   `json:"error"`
	} `json:"scores"`
}

func postEvaluationConsole(t *testing.T, app *testApp, cookies []*http.Cookie, path, body string, target any) {
	t.Helper()
	evaluationConsole(t, app, cookies, "POST", path, body, target)
}

func getEvaluationConsole(t *testing.T, app *testApp, cookies []*http.Cookie, path string, target any) {
	t.Helper()
	evaluationConsole(t, app, cookies, "GET", path, "", target)
}

func evaluationConsole(t *testing.T, app *testApp, cookies []*http.Cookie, method, path, body string, target any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	response := app.platformRequest(t, method, path, reader, cookies)
	defer response.Body.Close()
	raw := readAll(t, response.Body)
	if response.StatusCode != 200 {
		t.Fatalf("%s %s status = %d: %s", method, path, response.StatusCode, raw)
	}
	if target == nil {
		return
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, raw)
	}
}

func startEvaluationSandbox(t *testing.T, ctx context.Context, app *testApp, cfg config.Config, environmentID, sessionID string) *memorySandboxSession {
	t.Helper()
	workID := quickstartFindSessionEnvironmentWorkID(t, app, environmentID, sessionID)
	live := &memorySandboxSession{session: sessionAPIResponse{ID: sessionID}, workID: workID}
	runner := newManagedAgentRunner(t, app, e2bruntime.NewProvider(cfg.E2B), cfg)
	deadline := time.Now().Add(4 * time.Minute)
	var last environmentWorkAPIResponse
	for {
		last = quickstartRetrieveEnvironmentWork(t, app, environmentID, workID)
		if last.State == "active" || last.State == "running" {
			break
		}
		if last.State == "failed" || last.State == "stopped" {
			t.Fatalf("environment work %s state = %s", workID, last.State)
		}
		if time.Now().After(deadline) {
			t.Fatalf("environment work %s stayed %s", workID, last.State)
		}
		processed, err := runner.RunOnce(ctx, "agent-score-e2e")
		if err != nil {
			t.Fatalf("run environment runner once: %v", err)
		}
		if !processed {
			t.Fatal("environment runner did not process queued session work")
		}
	}
	if _, metadata := quickstartWaitForCodeSessionMetadata(t, ctx, app, sessionID); metadata["runtime"] != "claude_code_local" {
		t.Fatalf("session metadata was not patched with local code session ids: %#v", metadata)
	}
	providerSandboxID, workState := quickstartWaitForProviderSandboxMetadata(t, ctx, app, environmentID, workID)
	if workState != "active" && workState != "running" {
		t.Fatalf("environment work state = %s, want active", workState)
	}
	sandbox, err := e2b.Connect(ctx, providerSandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bruntime.ConnectionOptsFromConfig(cfg.E2B),
	})
	if err != nil {
		t.Fatalf("connect to sandbox %s: %v", providerSandboxID, err)
	}
	live.sandbox = sandbox
	return live
}

func postEvaluationSessionEndTurn(t *testing.T, app *testApp, sessionID string) {
	t.Helper()
	workspaceUUID := sessionWorkspaceUUID(t, app, sessionID)
	code, err := app.db.GetCodeSessionBySessionExternalID(context.Background(), workspaceUUID, sessionID)
	if err != nil {
		t.Fatalf("load code session for %s: %v", sessionID, err)
	}
	epoch := code.CurrentWorkerEpoch
	if epoch < 1 {
		t.Fatalf("code session %s worker epoch = %d", code.ExternalID, epoch)
	}
	body := `{"worker_epoch":` + quoteJSON(strconv.FormatInt(epoch, 10)) + `,"events":[{"payload":{"type":"result","is_error":false,"subtype":"success"}}]}`
	postCodeSessionWorkerEvents(t, app, code.ExternalID, body)
}

func waitForAgentScoreSession(t *testing.T, app *testApp, experimentID, caseSessionID string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last error
	for {
		var sessionID string
		err := app.pool.QueryRow(context.Background(), `
			select external_id
			from sessions
			where deleted_at is null
			  and metadata->>'evaluation_role' = 'agent_score'
			  and metadata->>'experiment_id' = $1
			  and external_id <> $2
			order by created_at desc
			limit 1
		`, experimentID, caseSessionID).Scan(&sessionID)
		if err == nil && sessionID != "" {
			return sessionID
		}
		last = err
		if time.Now().After(deadline) {
			t.Fatalf("agent score session for %s did not appear: %v", experimentID, last)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForFilestoreScore(t *testing.T, app *testApp, workspaceUUID, sessionID string) {
	t.Helper()
	files := filestore.NewService(app.cfg, app.db, nil, app.store)
	deadline := time.Now().Add(45 * time.Second)
	var last error
	for {
		body, err := files.ReadSessionFile(context.Background(), workspaceUUID, sessionID, "/outputs/score.json", 1<<20)
		if err == nil && strings.TrimSpace(string(body)) == agentScoreSandboxBody {
			return
		}
		last = err
		if err == nil {
			last = errors.New("unexpected score body " + string(body))
		}
		if time.Now().After(deadline) {
			t.Fatalf("filestore /outputs/score.json for %s: %v", sessionID, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func sessionWorkspaceUUID(t *testing.T, app *testApp, sessionID string) string {
	t.Helper()
	var workspaceUUID string
	if err := app.pool.QueryRow(context.Background(), `select workspace_uuid::text from sessions where external_id = $1`, sessionID).Scan(&workspaceUUID); err != nil {
		t.Fatalf("load workspace for %s: %v", sessionID, err)
	}
	return workspaceUUID
}

func stopSandboxAgent(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox) {
	t.Helper()
	_, _, err := runE2BCommand(ctx, sandbox, "pkill -f environment-manager || true", 20*time.Second)
	if err != nil {
		t.Fatalf("stop environment manager: %v", err)
	}
}

func sandboxReadlink(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, path string) string {
	t.Helper()
	return sandboxCommandOutput(t, ctx, sandbox, "readlink "+shellPath(path))
}

func sandboxCommandOutput(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, command string) string {
	t.Helper()
	stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second)
	if err != nil {
		t.Fatalf("%s: %v\nstdout=%s\nstderr=%s", command, err, stdout, stderr)
	}
	return strings.TrimSpace(stdout)
}

package environments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func (r *Runner) startExternalWorker(ctx context.Context, run *runnerRun, env db.Environment, work *db.EnvironmentWork) error {
	preparation, err := r.prepareManagedAgentLaunch(ctx, env, work)
	if err != nil {
		return err
	}
	run.markRecovered(isRecoveryLaunch(preparation))
	ctx = run.begin(ctx, "code_session")
	launch, err := r.createManagedAgentRuntimeLaunch(ctx, env, *work, *preparation)
	if err != nil {
		return err
	}
	ctx = run.begin(ctx, "mark_running")
	if _, err := r.db.HeartbeatEnvironmentWork(ctx, work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID, "", 60, formatTime); err != nil {
		return err
	}
	if r.cfg.EnvironmentRunner.BrainSandboxPrewarm {
		ctx = run.begin(ctx, "sandbox_prewarm")
		code, found, loadErr := r.db.GetCodeSession(ctx, launch.CodeSessionID)
		if loadErr != nil {
			return loadErr
		}
		if !found {
			return db.ErrNotFound
		}
		if _, _, err := r.ensureSessionSandbox(ctx, code, runnerTriggerPrewarm); err != nil {
			return err
		}
	}
	return r.finishManagedAgentRuntime(ctx, run, preparation.Session, *work, launch)
}

func (r *Runner) EnsureSessionSandbox(ctx context.Context, code db.CodeSession) (string, db.EnvironmentSandbox, error) {
	return r.ensureSessionSandbox(ctx, code, runnerTriggerLazyMCP)
}

func (r *Runner) ensureSessionSandbox(ctx context.Context, code db.CodeSession, trigger string) (endpoint string, _ db.EnvironmentSandbox, err error) {
	startedAt := time.Now()
	ensureOutcome := "reused"
	defer func() {
		if err != nil {
			ensureOutcome = telemetry.Outcome(err)
		}
		recordSandboxEnsure(ctx, trigger, ensureOutcome, startedAt)
	}()
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	work, err := r.db.GetLatestEnvironmentWorkForSession(ctx, code.WorkspaceUUID, code.EnvironmentExternalID, code.SessionUUID)
	if err != nil {
		return "", db.EnvironmentSandbox{}, err
	}
	if work.OrganizationUUID != code.OrganizationUUID || work.State != "active" {
		return "", db.EnvironmentSandbox{}, db.ErrInvalidState
	}
	if sandbox, err := r.db.GetResumableEnvironmentSandboxForCodeSession(ctx, code.ExternalID); err == nil && sandbox.ProviderSandboxID != nil {
		return r.sessionSandboxEndpoint(ctx, sandbox)
	} else if err != nil && !errors.Is(err, db.ErrNotFound) {
		return "", db.EnvironmentSandbox{}, err
	}
	env, err := r.db.GetEnvironmentByUUID(ctx, work.WorkspaceUUID, work.EnvironmentUUID)
	if err != nil {
		return "", db.EnvironmentSandbox{}, err
	}
	manager, err := brainSandboxManagerCommand(code.ExternalID, code.WorkDir, r.cfg)
	if err != nil {
		return "", db.EnvironmentSandbox{}, err
	}
	launch := managedAgentRuntimeLaunch{CodeSessionID: code.ExternalID, Recovered: true, Manager: manager}
	runCtx, run := startSandboxRun(ctx, work, trigger)
	created, err := r.createEnvironmentSandbox(runCtx, run, env, &work, &launch)
	run.end(runCtx, err)
	if err != nil {
		return "", db.EnvironmentSandbox{}, err
	}
	ensureOutcome = "joined"
	if created {
		ensureOutcome = "created"
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		sandbox, found, err := r.db.GetPendingEnvironmentSandboxForWork(ctx, work)
		if err != nil {
			return "", db.EnvironmentSandbox{}, err
		}
		if !found || sandbox.State == "stopping" {
			return "", db.EnvironmentSandbox{}, db.ErrInvalidState
		}
		if sandbox.State == "running" && sandbox.ProviderSandboxID != nil {
			return r.sessionSandboxEndpoint(ctx, sandbox)
		}
		if sandbox.CreatedAt.Add(3 * time.Minute).Before(time.Now()) {
			return "", db.EnvironmentSandbox{}, db.ErrInvalidState
		}
		select {
		case <-ctx.Done():
			return "", db.EnvironmentSandbox{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) sessionSandboxEndpoint(ctx context.Context, sandbox db.EnvironmentSandbox) (string, db.EnvironmentSandbox, error) {
	endpoint, err := r.sandboxMCPEndpoint(ctx, *sandbox.ProviderSandboxID)
	if err != nil {
		return "", db.EnvironmentSandbox{}, err
	}
	return endpoint, sandbox, nil
}

type brainSandboxManagerPayload struct {
	StartupContext struct {
		APIBaseURL string `json:"api_base_url"`
	} `json:"startup_context"`
	Environment struct {
		Type string `json:"environment_type"`
		CWD  string `json:"cwd"`
	} `json:"environment"`
}

func brainSandboxManagerCommand(codeSessionID, workDir string, cfg config.Config) (environmentManagerCommand, error) {
	request := brainSandboxManagerPayload{}
	request.StartupContext.APIBaseURL = codeSessionSandboxAPIBaseURL(cfg)
	request.Environment.Type = "anthropic"
	request.Environment.CWD = workDir
	payload, err := json.Marshal(request)
	if err != nil {
		return environmentManagerCommand{}, err
	}
	return buildEnvironmentManagerCommand(codeSessionID, cfg, payload), nil
}

func waitSandboxMCPReady(ctx context.Context, endpoint string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		probeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		err := probeSandboxMCPReady(probeCtx, endpoint)
		stop()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func probeSandboxMCPReady(ctx context.Context, endpoint string) error {
	const requestBody = `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(requestBody))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "server/discover")
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("sandbox MCP discovery returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 1<<20 {
		return errors.New("sandbox MCP discovery response is too large")
	}
	contentType := response.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		for _, line := range bytes.Split(body, []byte{'\n'}) {
			if data, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
				if err := validateSandboxMCPDiscovery(data); err == nil {
					return nil
				}
			}
		}
		return errors.New("sandbox MCP discovery did not advertise 2026-07-28")
	}
	if strings.HasPrefix(contentType, "application/json") {
		return validateSandboxMCPDiscovery(body)
	}
	return fmt.Errorf("sandbox MCP discovery returned content type %q", contentType)
}

func validateSandboxMCPDiscovery(body []byte) error {
	var response struct {
		ID     int `json:"id"`
		Result struct {
			ResultType        string   `json:"resultType"`
			SupportedVersions []string `json:"supportedVersions"`
			Capabilities      struct {
				Tools map[string]json.RawMessage `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	if response.ID != 1 {
		return errors.New("sandbox MCP discovery response ID mismatch")
	}
	if response.Result.ResultType != "complete" || response.Result.Capabilities.Tools == nil {
		return errors.New("sandbox MCP discovery has no tool capability")
	}
	for _, version := range response.Result.SupportedVersions {
		if version == "2026-07-28" {
			return nil
		}
	}
	return errors.New("sandbox MCP discovery did not advertise 2026-07-28")
}

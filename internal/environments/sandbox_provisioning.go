package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
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
	if cloudEnvironment(env) {
		ctx = run.begin(ctx, "mark_running")
		if _, err := r.db.HeartbeatEnvironmentWork(ctx, work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID, "", 60, formatTime); err != nil {
			return err
		}
	}
	if cloudEnvironment(env) && r.cfg.EnvironmentRunner.BrainSandboxPrewarm {
		ctx = run.begin(ctx, "sandbox_prewarm")
		code, found, loadErr := r.db.GetCodeSession(ctx, launch.CodeSessionID)
		if loadErr != nil {
			return loadErr
		}
		if !found {
			return db.ErrNotFound
		}
		if _, err := r.ensureSessionSandbox(ctx, code, runnerTriggerPrewarm); err != nil {
			return err
		}
	}
	return r.finishManagedAgentRuntime(ctx, run, preparation.Session, *work, launch)
}

func (r *Runner) EnsureSessionSandbox(ctx context.Context, code db.CodeSession) (db.EnvironmentSandbox, error) {
	return r.ensureSessionSandbox(ctx, code, runnerTriggerLazyMCP)
}

func (r *Runner) ensureSessionSandbox(ctx context.Context, code db.CodeSession, trigger string) (_ db.EnvironmentSandbox, err error) {
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
		return db.EnvironmentSandbox{}, err
	}
	if work.OrganizationUUID != code.OrganizationUUID || work.State != "active" {
		return db.EnvironmentSandbox{}, db.ErrInvalidState
	}
	if sandbox, err := r.db.GetResumableEnvironmentSandboxForCodeSession(ctx, code.ExternalID); err == nil && sandbox.ProviderSandboxID != nil {
		err := r.renewSessionSandbox(ctx, sandbox)
		if !errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
			return sandbox, err
		}
		now := time.Now().UTC()
		message := "provider sandbox not found"
		if err := r.db.UpdateEnvironmentSandboxState(ctx, sandbox.WorkspaceUUID, sandbox.ExternalID, "failed", sandbox.ProviderSandboxID, &message, &now); err != nil {
			return db.EnvironmentSandbox{}, err
		}
	} else if err != nil && !errors.Is(err, db.ErrNotFound) {
		return db.EnvironmentSandbox{}, err
	}
	env, err := r.db.GetEnvironmentByUUID(ctx, work.WorkspaceUUID, work.EnvironmentUUID)
	if err != nil {
		return db.EnvironmentSandbox{}, err
	}
	resources, err := r.db.ListSessionResources(ctx, work.WorkspaceUUID, work.SessionExternalID)
	if err != nil {
		return db.EnvironmentSandbox{}, err
	}
	resolved, err := resolveManagedAgentRuntimeResources(resources)
	if err != nil {
		return db.EnvironmentSandbox{}, err
	}
	manager, err := brainSandboxManagerCommand(code.ExternalID, code.WorkDir, r.cfg, nil, "", resolved.sources)
	if err != nil {
		return db.EnvironmentSandbox{}, err
	}
	launch := managedAgentRuntimeLaunch{CodeSessionID: code.ExternalID, Recovered: true, Manager: manager}
	runCtx, run := startSandboxRun(ctx, work, trigger)
	created, err := r.createEnvironmentSandbox(runCtx, run, env, &work, &launch)
	run.end(runCtx, err)
	if err != nil {
		return db.EnvironmentSandbox{}, err
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
			return db.EnvironmentSandbox{}, err
		}
		if !found || sandbox.State == "stopping" {
			return db.EnvironmentSandbox{}, db.ErrInvalidState
		}
		if sandbox.State == "running" && sandbox.ProviderSandboxID != nil {
			return sandbox, r.renewSessionSandbox(ctx, sandbox)
		}
		if sandbox.CreatedAt.Add(sandboxPreparationMaxAge).Before(time.Now()) {
			providerID := ""
			if sandbox.ProviderSandboxID != nil {
				providerID = *sandbox.ProviderSandboxID
			}
			r.failCreatedSandbox(ctx, sandbox, &work, providerID, fmt.Errorf("sandbox preparation expired: %w", context.DeadlineExceeded))
			return db.EnvironmentSandbox{}, db.ErrInvalidState
		}
		select {
		case <-ctx.Done():
			return db.EnvironmentSandbox{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) renewSessionSandbox(ctx context.Context, sandbox db.EnvironmentSandbox) error {
	timeout := r.cfg.E2B.SandboxTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return r.provider.SetTimeout(ctx, *sandbox.ProviderSandboxID, timeout)
}

type brainSandboxManagerPayload struct {
	Auth           []AuthConfig `json:"auth"`
	StartupContext struct {
		APIBaseURL           string            `json:"api_base_url"`
		EnvironmentVariables map[string]string `json:"environment_variables,omitempty"`
		UseCodeSessions      bool              `json:"use_code_sessions"`
		SessionID            string            `json:"session_id"`
		Sources              []json.RawMessage `json:"sources"`
	} `json:"startup_context"`
	Environment struct {
		Type string `json:"environment_type"`
		CWD  string `json:"cwd"`
	} `json:"environment"`
}

func brainSandboxManagerCommand(codeSessionID, workDir string, cfg config.Config, placeholders map[string]string, ingressToken string, sources []json.RawMessage) (environmentManagerCommand, error) {
	request := brainSandboxManagerPayload{}
	request.StartupContext.Sources = sources
	if request.StartupContext.Sources == nil {
		request.StartupContext.Sources = []json.RawMessage{}
	}
	request.StartupContext.APIBaseURL = codeSessionSandboxAPIBaseURL(cfg)
	request.StartupContext.EnvironmentVariables = make(map[string]string, len(placeholders)+2)
	for key, value := range placeholders {
		request.StartupContext.EnvironmentVariables[key] = value
	}
	request.StartupContext.EnvironmentVariables["CLAUDE_CODE_REMOTE"] = "true"
	request.StartupContext.EnvironmentVariables["CCR_UPSTREAM_PROXY_ENABLED"] = "1"
	request.StartupContext.UseCodeSessions = true
	request.StartupContext.SessionID = codeSessionID
	request.Auth = []AuthConfig{{Type: "session_ingress", Token: ingressToken}}
	request.Environment.Type = "anthropic"
	request.Environment.CWD = workDir
	payload, err := json.Marshal(request)
	if err != nil {
		return environmentManagerCommand{}, err
	}
	return buildEnvironmentManagerCommand(codeSessionID, cfg, payload), nil
}

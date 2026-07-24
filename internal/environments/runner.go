package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
)

var (
	errRcloneConfigWrite       = errors.New("rclone-filestore config write failed")
	errRcloneConfigPermissions = errors.New("rclone-filestore config permission update failed")
	errRcloneProcessStart      = errors.New("rclone-filestore process start failed")
	errRcloneReadiness         = errors.New("rclone-filestore readiness check failed")
	errEnvironmentManagerStart = errors.New("environment manager process start failed")
)

type Runner struct {
	db           *db.DB
	provider     e2bruntime.Provider
	cfg          config.Config
	codeSessions *codesessions.Service
	skills       *skillsapi.RuntimeResolver

	filestoreCredentials *filestore.TokenCredentials
}

type managedAgentLaunchPreparation struct {
	Session       db.Session
	InitialEvents []json.RawMessage
	SessionConfig json.RawMessage
	WorkDir       string
	Title         string
}

type managedAgentRuntimeLaunch struct {
	CodeSessionID   string
	PublicSessionID string
	SDKURLPath      string
	Manager         environmentManagerCommand
}

func NewRunner(database *db.DB, provider e2bruntime.Provider) *Runner {
	return &Runner{db: database, provider: provider}
}

func NewRunnerWithConfigStoreAndCredentials(
	database *db.DB,
	provider e2bruntime.Provider,
	cfg config.Config,
	store storage.ObjectStore,
	credentials *codesessions.SessionCredentials,
	filestoreCredentials *filestore.TokenCredentials,
) *Runner {
	// 显式注入用于 main 和测试，确保不会在同一进程中意外创建第二套签名身份。
	return &Runner{
		db:           database,
		provider:     provider,
		cfg:          cfg,
		codeSessions: codesessions.NewServiceWithCredentials(database, credentials),
		skills:       skillsapi.NewRuntimeResolver(cfg, database, store),

		filestoreCredentials: filestoreCredentials,
	}
}

func StartRunnerWithStoreAndCredentials(
	ctx context.Context,
	database *db.DB,
	store storage.ObjectStore,
	cfg config.Config,
	credentials *codesessions.SessionCredentials,
	filestoreCredentials *filestore.TokenCredentials,
) {
	if !cfg.EnvironmentRunner.Enabled {
		return
	}
	concurrency := cfg.EnvironmentRunner.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	runner := NewRunnerWithConfigStoreAndCredentials(
		database,
		e2bruntime.NewProvider(cfg.E2B),
		cfg,
		store,
		credentials,
		filestoreCredentials,
	)
	for i := 0; i < concurrency; i++ {
		workerID := fmt.Sprintf("environment-runner-%d", i+1)
		go runner.loop(ctx, workerID)
	}
}

func (r *Runner) loop(ctx context.Context, workerID string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		processed, err := r.RunOnce(ctx, workerID)
		if err != nil {
			log.Printf("environment runner worker=%s: %v", workerID, err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) RunOnce(ctx context.Context, workerID string) (bool, error) {
	work, err := r.db.PollNextEnvironmentWorkForRunner(ctx, workerID, 5*time.Second, true)
	if err != nil || work == nil {
		return false, err
	}
	if _, err := r.db.AckEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID); err != nil {
		return true, err
	}
	env, err := r.db.GetEnvironmentByInternalID(ctx, work.WorkspaceID, work.EnvironmentID)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	sandboxID, err := ids.New("envsbx_")
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	if err := r.prepareManagedAgentNetworkMetadata(ctx, env, work); err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	resolution, err := r.provider.Resolve(env, work)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	preparation, err := r.prepareManagedAgentLaunch(ctx, env, work)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	record, err := r.db.CreateEnvironmentSandbox(ctx, db.EnvironmentSandbox{
		UUID:                  uuid.NewString(),
		ExternalID:            sandboxID,
		OrganizationID:        work.OrganizationID,
		WorkspaceID:           work.WorkspaceID,
		EnvironmentID:         work.EnvironmentID,
		EnvironmentExternalID: work.EnvironmentExternalID,
		WorkID:                &work.ID,
		WorkExternalID:        &work.ExternalID,
		Provider:              "e2b",
		Template:              resolution.Template,
		State:                 "creating",
		Metadata:              work.Metadata,
		CreatedAt:             time.Now().UTC(),
	})
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	sandbox, err := r.provider.Create(ctx, env, work, resolution)
	if err != nil {
		now := time.Now().UTC()
		message := err.Error()
		_ = r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "failed", nil, &message, &now)
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	providerSandboxID := sandbox.ID
	if strings.TrimSpace(providerSandboxID) != "" || preparation != nil {
		nextWorkMetadata := work.Metadata
		if strings.TrimSpace(providerSandboxID) != "" {
			nextWorkMetadata, err = patchJSONMetadata(work.Metadata, map[string]any{
				"provider_sandbox_id": providerSandboxID,
			})
			if err != nil {
				r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
				return true, err
			}
		}
		updatedWork, err := r.db.UpdateEnvironmentWorkMetadata(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, nextWorkMetadata)
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
		*work = updatedWork
	}
	if preparation != nil {
		rcloneLaunch, err := r.prepareRcloneFilestoreLaunch(ctx, preparation.Session)
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, fmt.Errorf("prepare rclone-filestore launch: %w", err)
		}
		if err := r.startRcloneFilestore(ctx, providerSandboxID, rcloneLaunch); err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
	}
	if err := r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "running", &providerSandboxID, nil, nil); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if _, err := r.db.HeartbeatEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, "", 60, formatTime); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if preparation != nil {
		launch, err := r.createManagedAgentRuntimeLaunch(ctx, env, *work, *preparation)
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, fmt.Errorf("create managed-agent runtime launch: %w", err)
		}
		// rclone 和固定挂载已就绪；manager 随后通过 stdin 取得双凭证，
		// 并在启动 Claude 前 register worker，建立首个 CCR lease。
		if err := r.provider.StartBackgroundCommand(ctx, providerSandboxID, launch.Manager.ShellCommand, launch.Manager.Payload); err != nil {
			publicError := logManagedAgentRuntimeStageFailure(
				"environment_manager_start",
				errEnvironmentManagerStart,
				err,
			)
			r.failManagedAgentRuntime(ctx, record, work, providerSandboxID, preparation.Session, launch.CodeSessionID, publicError)
			return true, publicError
		}
		if err := r.publishManagedAgentRuntime(ctx, preparation.Session, *work, launch); err != nil {
			r.failManagedAgentRuntime(ctx, record, work, providerSandboxID, preparation.Session, launch.CodeSessionID, err)
			return true, fmt.Errorf("publish managed-agent runtime metadata: %w", err)
		}
	}
	return true, nil
}

func (r *Runner) failManagedAgentRuntime(
	ctx context.Context,
	record db.EnvironmentSandbox,
	work *db.EnvironmentWork,
	providerSandboxID string,
	session db.Session,
	codeSessionID string,
	cause error,
) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.codeSessions.TerminateManagedAgentCodeSession(cleanupCtx, session, codeSessionID); err != nil {
		log.Printf(
			"terminate failed managed-agent runtime code_session_id=%s stage_error_type=%T cleanup_error_type=%T",
			codeSessionID,
			cause,
			err,
		)
	}
	r.failCreatedSandbox(ctx, record, work, providerSandboxID, cause)
}

func (r *Runner) failCreatedSandbox(ctx context.Context, record db.EnvironmentSandbox, work *db.EnvironmentWork, providerSandboxID string, cause error) {
	now := time.Now().UTC()
	message := cause.Error()
	_ = r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "failed", &providerSandboxID, &message, &now)
	_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
	if strings.TrimSpace(providerSandboxID) == "" {
		return
	}
	killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = r.provider.Kill(killCtx, providerSandboxID)
}

func (r *Runner) prepareManagedAgentLaunch(
	ctx context.Context,
	env db.Environment,
	work *db.EnvironmentWork,
) (*managedAgentLaunchPreparation, error) {
	if r == nil || work == nil || r.codeSessions == nil {
		return nil, nil
	}
	sessionID, ok := sessionIDFromEnvironmentWork(*work)
	if !ok || !cloudEnvironment(env) {
		return nil, nil
	}
	session, err := r.db.GetSession(ctx, work.WorkspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	resources, err := r.db.ListSessionResources(ctx, session.WorkspaceID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	events, err := r.sessionEventPayloads(ctx, session)
	if err != nil {
		return nil, err
	}
	runtimeSkills, err := r.resolveRuntimeSkills(ctx, session)
	if err != nil {
		return nil, err
	}
	skillMount, err := r.prepareRuntimeSkillMount(ctx, runtimeSkills)
	if err != nil {
		return nil, err
	}
	runtimeResources := resolveManagedAgentRuntimeResources(resources)
	sessionConfig := managedAgentSessionConfig(session, runtimeResources)
	workDir := runtimeResources.workDir
	title := ""
	if session.Title != nil {
		title = *session.Title
	}
	if skillMount != nil {
		nextWorkMetadata, err := patchJSONMetadata(work.Metadata, map[string]any{
			e2bruntime.SkillMountMetadataKey: skillMount,
		})
		if err != nil {
			return nil, err
		}
		work.Metadata = nextWorkMetadata
	}
	return &managedAgentLaunchPreparation{
		Session:       session,
		InitialEvents: events,
		SessionConfig: sessionConfig,
		WorkDir:       workDir,
		Title:         title,
	}, nil
}

func (r *Runner) createManagedAgentRuntimeLaunch(
	ctx context.Context,
	env db.Environment,
	work db.EnvironmentWork,
	preparation managedAgentLaunchPreparation,
) (managedAgentRuntimeLaunch, error) {
	local, err := r.codeSessions.CreateManagedAgentCodeSession(ctx, codesessions.ManagedAgentCreateInput{
		Session:                    preparation.Session,
		Environment:                env,
		EnvironmentWork:            work,
		Model:                      modelIDFromAgentSnapshot(preparation.Session.AgentSnapshot),
		Title:                      preparation.Title,
		WorkDir:                    preparation.WorkDir,
		PermissionMode:             "bypassPermissions",
		DangerouslySkipPermissions: true,
		Config:                     preparation.SessionConfig,
		InitialEvents:              preparation.InitialEvents,
	})
	if err != nil {
		return managedAgentRuntimeLaunch{}, err
	}
	payload, err := buildEnvironmentManagerV0Payload(
		local.CodeSessionID,
		local.SessionIngressToken,
		local.OAuthAccessToken,
		preparation.WorkDir,
		preparation.SessionConfig,
		r.cfg,
	)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.codeSessions.TerminateManagedAgentCodeSession(
			cleanupCtx,
			preparation.Session,
			local.CodeSessionID,
		)
		return managedAgentRuntimeLaunch{}, err
	}
	return managedAgentRuntimeLaunch{
		CodeSessionID:   local.CodeSessionID,
		PublicSessionID: local.PublicSessionID,
		SDKURLPath:      local.SDKURLPath,
		Manager:         buildEnvironmentManagerCommand(local.CodeSessionID, r.cfg, payload),
	}, nil
}

func (r *Runner) publishManagedAgentRuntime(
	ctx context.Context,
	session db.Session,
	work db.EnvironmentWork,
	launch managedAgentRuntimeLaunch,
) error {
	metadataPatch, err := json.Marshal(map[string]any{
		"claude_code_session_id":        launch.CodeSessionID,
		"claude_code_public_session_id": launch.PublicSessionID,
		"claude_code_sdk_url_path":      launch.SDKURLPath,
		"runtime":                       "claude_code_local",
	})
	if err != nil {
		return err
	}
	return r.db.BindManagedAgentRuntimeMetadata(
		ctx,
		session,
		work,
		metadataPatch,
		metadataPatch,
	)
}

func (r *Runner) startRcloneFilestore(ctx context.Context, sandboxID string, launch rcloneFilestoreLaunch) error {
	if err := r.provider.WriteFile(ctx, sandboxID, rcloneConfigPath, launch.ConfigPayload); err != nil {
		_ = r.provider.RunCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("config_write", errRcloneConfigWrite, err)
	}
	if err := r.provider.RunCommand(ctx, sandboxID, rcloneConfigPermissionsCommand(), rcloneCommandGraceTimeout); err != nil {
		_ = r.provider.RunCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("config_permissions", errRcloneConfigPermissions, err)
	}
	if err := r.provider.StartBackgroundCommand(ctx, sandboxID, rcloneStartCommand(), nil); err != nil {
		_ = r.provider.RunCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("process_start", errRcloneProcessStart, err)
	}
	if err := r.waitForRcloneReady(ctx, sandboxID, rcloneReadyPollInterval, rcloneReadyTimeout); err != nil {
		_ = r.provider.RunCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("readiness", errRcloneReadiness, err)
	}
	r.removeRcloneConfig(ctx, sandboxID)
	return nil
}

func (r *Runner) removeRcloneConfig(ctx context.Context, sandboxID string) {
	for attempt := 1; attempt <= rcloneConfigCleanupTries; attempt++ {
		cleanupErr := r.provider.RunCommand(
			ctx,
			sandboxID,
			rcloneConfigCleanupCommand(),
			rcloneCommandGraceTimeout,
		)
		if cleanupErr == nil {
			return
		}
		log.Printf(
			"rclone-filestore stage=config_cleanup attempt=%d error_type=%T",
			attempt,
			cleanupErr,
		)
		exists, probeErr := r.provider.FileExists(ctx, sandboxID, rcloneConfigPath)
		if probeErr == nil && !exists {
			return
		}
		if probeErr != nil {
			log.Printf(
				"rclone-filestore stage=config_cleanup_probe attempt=%d error_type=%T",
				attempt,
				probeErr,
			)
		}
	}
	log.Printf(
		"rclone-filestore stage=config_cleanup exhausted_attempts=%d config_may_remain=true",
		rcloneConfigCleanupTries,
	)
}

func logRcloneStageFailure(stage string, publicError, cause error) error {
	log.Printf("rclone-filestore stage=%s error_type=%T", stage, cause)
	return publicError
}

func logManagedAgentRuntimeStageFailure(stage string, publicError, cause error) error {
	log.Printf("managed-agent runtime stage=%s error_type=%T", stage, cause)
	return publicError
}

func (r *Runner) waitForRcloneReady(
	ctx context.Context,
	sandboxID string,
	pollInterval time.Duration,
	timeout time.Duration,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		ready, err := r.provider.FileExists(waitCtx, sandboxID, rcloneReadyPath)
		if err != nil {
			return fmt.Errorf("probe ready marker: %w", err)
		}
		if ready {
			return nil
		}

		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("timed out after %s", timeout)
		case <-ticker.C:
		}
	}
}

// prepareManagedAgentNetworkMetadata 在 Provider Resolve 之前解析受开关约束的
// Session MCP hosts，使 E2B 的创建时网络快照与 proxy 的策略语义一致。
func (r *Runner) prepareManagedAgentNetworkMetadata(ctx context.Context, env db.Environment, work *db.EnvironmentWork) error {
	if r == nil || work == nil || r.codeSessions == nil || !cloudEnvironment(env) {
		return nil
	}
	policyConfig, err := networkpolicy.ParseConfig(env.Config)
	if err != nil {
		return err
	}
	hosts := []string{}
	if policyConfig.Type == networkpolicy.TypeLimited && policyConfig.AllowMCPServers {
		sessionID, ok := sessionIDFromEnvironmentWork(*work)
		if !ok {
			return fmt.Errorf("limited managed-agent MCP policy requires session work identity")
		}
		session, err := r.db.GetSession(ctx, work.WorkspaceID, sessionID)
		if err != nil {
			return err
		}
		hosts, err = networkpolicy.MCPAllowedHosts(session.AgentSnapshot)
		if err != nil {
			return err
		}
	}
	if hosts == nil {
		hosts = []string{}
	}
	nextMetadata, err := networkpolicy.PatchWorkMetadataMCPAllowedHosts(work.Metadata, hosts)
	if err != nil {
		return err
	}
	updatedWork, err := r.db.UpdateEnvironmentWorkMetadata(
		ctx,
		work.WorkspaceID,
		work.EnvironmentExternalID,
		work.ExternalID,
		nextMetadata,
	)
	if err != nil {
		return err
	}
	*work = updatedWork
	return nil
}

func (r *Runner) prepareRuntimeSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*e2bruntime.SkillMount, error) {
	if len(runtimeSkills) == 0 {
		return nil, nil
	}
	preparer, ok := r.provider.(e2bruntime.SkillMountPreparer)
	if !ok {
		return nil, fmt.Errorf("runtime provider cannot prepare managed agent skill mount")
	}
	return preparer.PrepareSkillMount(ctx, runtimeSkills)
}

func (r *Runner) resolveRuntimeSkills(ctx context.Context, session db.Session) ([]skillsapi.RuntimeSkill, error) {
	if r == nil || r.skills == nil {
		if agentsnapshot.SnapshotHasSkills(session.AgentSnapshot) {
			return nil, fmt.Errorf("managed agent session %s has skills but runtime skill resolver is unavailable", session.ExternalID)
		}
		return nil, nil
	}
	return r.skills.ResolveAgentSnapshot(ctx, session.WorkspaceID, session.AgentSnapshot)
}

func (r *Runner) sessionEventPayloads(ctx context.Context, session db.Session) ([]json.RawMessage, error) {
	var out []json.RawMessage
	var cursor *db.SessionEventPageCursor
	for {
		events, hasMore, err := r.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceID:       session.WorkspaceID,
			SessionExternalID: session.ExternalID,
			Limit:             100,
			Cursor:            cursor,
			Order:             "asc",
		})
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			out = append(out, append(json.RawMessage(nil), event.Payload...))
		}
		if !hasMore || len(events) == 0 {
			return out, nil
		}
		last := events[len(events)-1]
		cursor = &db.SessionEventPageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func sessionIDFromEnvironmentWork(work db.EnvironmentWork) (string, bool) {
	var data struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(work.Data, &data); err != nil {
		return "", false
	}
	if strings.TrimSpace(data.Type) != "session" || strings.TrimSpace(data.ID) == "" {
		return "", false
	}
	return strings.TrimSpace(data.ID), true
}

func cloudEnvironment(env db.Environment) bool {
	var config struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Config, &config); err != nil {
		return false
	}
	return strings.TrimSpace(config.Type) == "cloud"
}

func patchJSONMetadata(raw json.RawMessage, patch map[string]any) (json.RawMessage, error) {
	metadata := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, err
		}
	}
	for key, value := range patch {
		metadata[key] = value
	}
	return json.Marshal(metadata)
}

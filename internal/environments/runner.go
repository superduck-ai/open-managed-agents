package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"

	"github.com/google/uuid"
)

var (
	errRcloneConfigWrite       = errors.New("rclone-filestore config write failed")
	errRcloneConfigPermissions = errors.New("rclone-filestore config permission update failed")
	errRcloneProcessStart      = errors.New("rclone-filestore process start failed")
	errRcloneReadiness         = errors.New("rclone-filestore readiness check failed")
	errEnvironmentManagerStart = errors.New("environment manager process start failed")
)

// CodeSessionRuntime exposes the managed-agent Code Session operations needed
// by Runner without coupling it to the concrete service implementation.
type CodeSessionRuntime interface {
	CreateManagedAgentCodeSession(context.Context, codesessions.ManagedAgentCreateInput) (codesessions.ManagedAgentCreateResult, error)
	TerminateManagedAgentCodeSession(context.Context, db.Session, string) error
}

// RuntimeSkillResolver resolves the immutable agent snapshot into skills that
// the sandbox provider can mount for this launch.
type RuntimeSkillResolver interface {
	ResolveAgentSnapshot(context.Context, int64, json.RawMessage) ([]skillsapi.RuntimeSkill, error)
}

// FilestoreTokenIssuer signs the read-write and read-only credentials used by
// the sandbox's fixed rclone mounts.
type FilestoreTokenIssuer interface {
	Issue(filestore.TokenIdentity) (string, error)
	IssueReadonly(filestore.TokenIdentity) (string, error)
}

// RunnerDependencies contains the complete set of collaborators required by a
// Runner. NewRunner validates these once so runtime branches do not silently
// disable Code Session, skill, or filestore behavior when a dependency is nil.
type RunnerDependencies struct {
	DB              *db.DB
	Provider        e2bruntime.Provider
	Config          config.Config
	CodeSessions    CodeSessionRuntime
	Skills          RuntimeSkillResolver
	FilestoreTokens FilestoreTokenIssuer
	Logger          *slog.Logger
}

type Runner struct {
	db              *db.DB
	provider        e2bruntime.Provider
	cfg             config.Config
	codeSessions    CodeSessionRuntime
	skills          RuntimeSkillResolver
	filestoreTokens FilestoreTokenIssuer
	logger          *slog.Logger
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

// NewRunner constructs a fully usable environment Runner from final runtime
// collaborators. It rejects incomplete dependency sets before workers start.
func NewRunner(deps RunnerDependencies) (*Runner, error) {
	switch {
	case deps.DB == nil:
		return nil, errors.New("environment runner database is required")
	case deps.Provider == nil:
		return nil, errors.New("environment runner sandbox provider is required")
	case deps.CodeSessions == nil:
		return nil, errors.New("environment runner code session runtime is required")
	case deps.Skills == nil:
		return nil, errors.New("environment runner skill resolver is required")
	case deps.FilestoreTokens == nil:
		return nil, errors.New("environment runner filestore token issuer is required")
	}

	return &Runner{
		db:              deps.DB,
		provider:        deps.Provider,
		cfg:             deps.Config,
		codeSessions:    deps.CodeSessions,
		skills:          deps.Skills,
		filestoreTokens: deps.FilestoreTokens,
		logger:          logging.LoggerOrDefault(deps.Logger),
	}, nil
}

// Start launches the configured number of background workers. It is a no-op
// when the environment runner is disabled.
func (r *Runner) Start(ctx context.Context) {
	if !r.cfg.EnvironmentRunner.Enabled {
		return
	}
	concurrency := r.cfg.EnvironmentRunner.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	for i := 0; i < concurrency; i++ {
		workerID := fmt.Sprintf("environment-runner-%d", i+1)
		go r.loop(ctx, workerID)
	}
}

// loop 持续领取并处理排队中的 Environment Work，直到服务通过 ctx 通知它退出。
//
// Runner.Start 会按配置的并发数为每个 worker 启动一个 loop。
// 每轮调用 RunOnce，最多处理一个 Work。RunOnce 返回 processed=true 只表示领到过
// Work，不代表 Sandbox 一定启动成功；此时 loop 会立即检查下一项。没有可领取的 Work
// 时，它等待最多 500ms 再检查，避免空闲时持续查询数据库。单次错误只写日志，不会让
// 后台 worker 退出。
//
// loop 本身没有事务或内存锁。RunOnce 使用数据库的 FOR UPDATE SKIP LOCKED 原子领取
// 最早的可用 Work，并写入 workerID 和 5 秒领取期限，因此并发 worker 不会同时处理
// 同一条记录；worker 在确认领取前退出时，过期记录可以再次被领取。这里也不执行 API
// 鉴权，而是信任已经进入内部队列的 Work，workerID 仅用于领取记录和日志定位。
//
// 例如：
//   - 队列中连续有两个 Work；处理第一项后无需等待，loop 会立即领取第二项。
//   - 队列为空；RunOnce 返回 processed=false，loop 等待下一次 500ms tick。
//   - 已领取 Work，但创建 Sandbox 失败；RunOnce 负责标记失败和清理，loop 记录错误后
//     继续服务其他 Work。
//
// 函数没有返回值。正常情况下只在 ctx 被取消时返回，并停止内部 ticker。数据库状态、
// E2B Sandbox、rclone 和 Environment Manager 等副作用都由 RunOnce 及其下游调用产生。
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
			r.logger.ErrorContext(ctx, "environment runner", "worker_id", workerID, "error", err)
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
	// 每次最多领取一条 queued Work。数据库使用 FOR UPDATE SKIP LOCKED
	// 避免并发 worker 领取同一条记录，并以 5 秒 claim 为 Ack 前的短暂保护。
	work, err := r.db.PollNextEnvironmentWorkForRunner(ctx, workerID, 5*time.Second, true)
	if err != nil || work == nil {
		// false 表示本轮没有取得 Work：可能是队列为空，也可能是领取 SQL 失败。
		return false, err
	}

	// 领取成功后先把 Work 从 queued 推进到 starting，并清除短期 claim。
	// 从这里开始，即使后续步骤失败，processed 也返回 true，表示本轮消费过一条 Work。
	if _, err := r.db.AckEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID); err != nil {
		return true, err
	}

	// 加载 Work 所属的 Environment，并生成本服务对外使用的 envsbx_ ID。
	// 此时实际的 E2B Sandbox 尚未创建；失败只需停止 Work，不存在远端资源需要清理。
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

	// 在 Provider 解析之前固化 Managed Agent 的网络 metadata。这样 Resolve 和
	// 后续 Create 使用的是同一份 MCP allowlist，避免创建时网络策略发生漂移。
	if err := r.prepareManagedAgentNetworkMetadata(ctx, env, work); err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	resolution, err := r.provider.Resolve(env, work)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}

	// Cloud Session Work 还要读取 Session、resources、events 和 skills，准备
	// rclone 与 Environment Manager 的启动数据。普通 Work 返回 nil preparation，
	// 后续只创建 Sandbox，不进入 Managed Agent 专属分支。
	preparation, err := r.prepareManagedAgentLaunch(ctx, env, work)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}

	// 先落一条 creating 状态的本地 Sandbox 记录，再请求 E2B 创建远端 Sandbox。
	// 这样即使远端创建失败，数据库中仍有可查询的启动尝试和失败状态。
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

	// provider.Create 返回的 ID 是 E2B 的真实 Sandbox ID，与上面的 envsbx_ ID
	// 分属远端 Provider 和本服务两个命名空间。
	sandbox, err := r.provider.Create(ctx, env, work, resolution)
	if err != nil {
		now := time.Now().UTC()
		message := err.Error()
		_ = r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "failed", nil, &message, &now)
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	providerSandboxID := sandbox.ID

	// 将 E2B ID 写入 Work metadata；Managed Agent preparation 可能还加入了 skill
	// mount，所以即使 Provider ID 为空，也要为该分支持久化更新后的 metadata。
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

	manifest, provision, err := buildPackageManifest(env.Config)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if provision {
		proceed, err := r.provisionCreatedSandboxPackages(ctx, record, work, providerSandboxID, manifest)
		if err != nil {
			return true, err
		}
		if !proceed {
			return true, nil
		}
	}

	// 只有 Cloud Session Managed Agent 使用固定的四组 Filestore 挂载。
	// 必须等 rclone ready 后才能继续，确保 Claude 启动时 uploads、outputs、
	// transcripts 和 tool_results 已经可用。
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

	// rclone 就绪后才公开 Sandbox 为 running。此后的失败统一由
	// failCreatedSandbox 标记 Sandbox/Work 失败，并 Kill 已创建的 E2B Sandbox。
	if err := r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "running", &providerSandboxID, nil, nil); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}

	// 首次 heartbeat 把 Work 推进为 active，并建立 60 秒运行租约。
	if _, err := r.db.HeartbeatEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, "", 60, formatTime); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}

	// 普通 Work 到这里已经完成。Cloud Session 还需创建 Code Session，并在
	// Sandbox 内启动 Environment Manager。
	if preparation != nil {
		launch, err := r.createManagedAgentRuntimeLaunch(ctx, env, *work, *preparation)
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, fmt.Errorf("create managed-agent runtime launch: %w", err)
		}
		// rclone 和固定挂载已就绪；manager 随后通过 stdin 取得双凭证，
		// 并在启动 Claude 前 register worker，建立首个 CCR lease。
		if err := r.provider.StartBackgroundCommand(ctx, providerSandboxID, launch.Manager.ShellCommand, launch.Manager.Payload); err != nil {
			publicError := r.logManagedAgentRuntimeStageFailure(
				ctx,
				"environment_manager_start",
				errEnvironmentManagerStart,
				err,
			)
			r.failManagedAgentRuntime(ctx, record, work, providerSandboxID, preparation.Session, launch.CodeSessionID, publicError)
			return true, publicError
		}

		// 只有 Manager 后台命令成功提交后才发布 runtime metadata，避免把启动失败的
		// Code Session 暴露为可用。发布失败时同样终止 Code Session 并清理 Sandbox。
		if err := r.publishManagedAgentRuntime(ctx, preparation.Session, *work, launch); err != nil {
			r.failManagedAgentRuntime(ctx, record, work, providerSandboxID, preparation.Session, launch.CodeSessionID, err)
			return true, fmt.Errorf("publish managed-agent runtime metadata: %w", err)
		}
	}

	// true 表示本轮确实消费了一条 Work；nil 表示所需启动阶段全部完成。
	return true, nil
}

func (r *Runner) provisionCreatedSandboxPackages(
	ctx context.Context,
	record db.EnvironmentSandbox,
	work *db.EnvironmentWork,
	providerSandboxID string,
	manifest []byte,
) (bool, error) {
	if strings.TrimSpace(providerSandboxID) == "" {
		err := errors.New("provider returned an empty sandbox id for package provisioning")
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return false, err
	}
	// Persist the provider ID before provisioning so a concurrent stop can
	// find and terminate the sandbox while package installation is running.
	if err := r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "creating", &providerSandboxID, nil, nil); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return false, err
	}
	if err := r.provisionPackages(ctx, providerSandboxID, manifest); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return false, err
	}
	heartbeat, err := r.db.HeartbeatEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, "", 60, formatTime)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return false, err
	}
	if heartbeat.LeaseExtended {
		return true, nil
	}
	r.failCreatedSandbox(ctx, record, work, providerSandboxID, errors.New("environment work stopped during package provisioning"))
	return false, nil
}

func (r *Runner) provisionPackages(ctx context.Context, sandboxID string, manifest []byte) error {
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: buildPackageProvisionCommand(r.cfg),
		Stdin:   manifest,
		Timeout: r.cfg.E2B.SandboxTimeout,
	})
	if err != nil {
		return fmt.Errorf("provision environment packages: %w", err)
	}
	return validatePackageProvisioningResult(result)
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
		r.logger.ErrorContext(
			cleanupCtx,
			"terminate failed managed agent runtime",
			"code_session_id", codeSessionID,
			"stage_error_type", fmt.Sprintf("%T", cause),
			"error", err,
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
	if r == nil || work == nil {
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
	if err := r.replaceRuntimeSkillArchives(ctx, session, runtimeSkills); err != nil {
		return nil, err
	}
	runtimeResources := resolveManagedAgentRuntimeResources(resources)
	sessionConfig := managedAgentSessionConfig(session, runtimeResources)
	workDir := runtimeResources.workDir
	title := ""
	if session.Title != nil {
		title = *session.Title
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
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return r.logRcloneStageFailure(ctx, "config_write", errRcloneConfigWrite, err)
	}
	if err := r.runSandboxCommand(ctx, sandboxID, rcloneConfigPermissionsCommand(), rcloneCommandGraceTimeout); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return r.logRcloneStageFailure(ctx, "config_permissions", errRcloneConfigPermissions, err)
	}
	if err := r.provider.StartBackgroundCommand(ctx, sandboxID, rcloneStartCommand(), nil); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return r.logRcloneStageFailure(ctx, "process_start", errRcloneProcessStart, err)
	}
	if err := r.waitForRcloneReady(ctx, sandboxID, rcloneReadyPollInterval, rcloneReadyTimeout); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return r.logRcloneStageFailure(ctx, "readiness", errRcloneReadiness, err)
	}
	r.removeRcloneConfig(ctx, sandboxID)
	return nil
}

func (r *Runner) removeRcloneConfig(ctx context.Context, sandboxID string) {
	for attempt := 1; attempt <= rcloneConfigCleanupTries; attempt++ {
		cleanupErr := r.runSandboxCommand(
			ctx,
			sandboxID,
			rcloneConfigCleanupCommand(),
			rcloneCommandGraceTimeout,
		)
		if cleanupErr == nil {
			return
		}
		r.logger.WarnContext(
			ctx,
			"rclone filestore config cleanup",
			"attempt", attempt,
			"error", cleanupErr,
		)
		exists, probeErr := r.provider.FileExists(ctx, sandboxID, rcloneConfigPath)
		if probeErr == nil && !exists {
			return
		}
		if probeErr != nil {
			r.logger.WarnContext(
				ctx,
				"rclone filestore config cleanup probe",
				"attempt", attempt,
				"error", probeErr,
			)
		}
	}
	r.logger.ErrorContext(
		ctx,
		"rclone filestore config cleanup exhausted",
		"attempts", rcloneConfigCleanupTries,
		"config_may_remain", true,
	)
}

func (r *Runner) runSandboxCommand(ctx context.Context, sandboxID, command string, timeout time.Duration) error {
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{Command: command, Timeout: timeout})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("sandbox command exited with code %d", result.ExitCode)
	}
	return nil
}

func (r *Runner) logRcloneStageFailure(ctx context.Context, stage string, publicError, cause error) error {
	r.logger.ErrorContext(ctx, "rclone filestore stage failed", "stage", stage, "error", cause)
	return publicError
}

func (r *Runner) logManagedAgentRuntimeStageFailure(ctx context.Context, stage string, publicError, cause error) error {
	r.logger.ErrorContext(ctx, "managed agent runtime stage failed", "stage", stage, "error", cause)
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
	if r == nil || work == nil || !cloudEnvironment(env) {
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

func (r *Runner) resolveRuntimeSkills(ctx context.Context, session db.Session) ([]skillsapi.RuntimeSkill, error) {
	return r.skills.ResolveAgentSnapshot(ctx, session.WorkspaceID, session.AgentSnapshot)
}

// replaceRuntimeSkillArchives 使用已解析的不可变 skill archive，完整替换 Managed Agent
// Session 的 /skills archive entries。
//
// runtimeSkills 必须已经包含具体的版本 UUID 和可信对象元数据；"latest" 会在调用本函数
// 前解析为确定版本。本函数只保留将 zip 映射为 /skills/<directory> 所需的字段，不下载、
// 复制或解压 archive。每个 zip 作为 kind=archive 的受管 entry 写入
// /skills/<directory>，来源保存在通用 metadata 中。
//
// DB 操作会校验来源、目录、版本 UUID、对象大小和 SHA-256。它在同一个事务中锁定 Session
// filesystem 记录及其命名空间，确保固定根目录存在，然后软删除旧 entries 并插入新集合。
// 采用全量替换，是为了让已从 Agent snapshot 移除的 skill 同步消失，并避免读取方或并发的
// 命名空间写入方看到只更新了一部分的视图；软删除则保留历史投影供审计。
//
// 成功时返回 nil，确保固定根目录存在并替换 archive entries；catalog 对象仍归 skill
// catalog 所有。runtimeSkills 为空时会清空 archive 子目录，但保留 /skills 根目录。元数据无效、
// Session filesystem 不存在、目录重复，或事务、加锁、写入失败时，函数返回包装后的
// 错误，并回滚整个替换操作。
//
// 示例：
//   - [pdf@v2, sheets@v1] 成功映射为 /skills/pdf 和 /skills/sheets。
//   - [] 会删除所有 skill 投影目录，但保留 /skills。
//   - 两个 skill 同时使用目录 "pdf" 时返回错误，旧视图保持不变。
func (r *Runner) replaceRuntimeSkillArchives(
	ctx context.Context,
	session db.Session,
	runtimeSkills []skillsapi.RuntimeSkill,
) error {
	archives := make([]db.FilestoreSkillArchiveEntryInput, 0, len(runtimeSkills))
	for _, skill := range runtimeSkills {
		archives = append(archives, db.FilestoreSkillArchiveEntryInput{
			Source:           skill.Source,
			SkillVersionUUID: skill.VersionUUID,
			Directory:        skill.Directory,
			S3Bucket:         skill.S3Bucket,
			S3Key:            skill.S3Key,
			SizeBytes:        skill.SizeBytes,
			SHA256:           skill.SHA256,
		})
	}
	if err := r.db.ReplaceFilestoreSkillArchiveEntries(ctx, session.WorkspaceID, session.ExternalID, archives); err != nil {
		return fmt.Errorf("replace managed agent skill archive entries: %w", err)
	}
	return nil
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

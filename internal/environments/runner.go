package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
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
}

type Runner struct {
	db           *db.DB
	provider     e2bruntime.Provider
	cfg          config.Config
	codeSessions CodeSessionRuntime
	skills       RuntimeSkillResolver

	filestoreTokens FilestoreTokenIssuer
}

type managedAgentLaunchPreparation struct {
	session               db.Session
	sessionConfig         json.RawMessage
	persistedWorkMetadata json.RawMessage
	skillMount            *e2bruntime.SkillMount
	workDir               string
	title                 string
	model                 string
}

type managedAgentRuntimeLaunch struct {
	CodeSessionID string
	Manager       environmentManagerCommand
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
	preparation, err := r.prepareManagedAgentLaunch(ctx, env, work)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	resolution, err := r.provider.Resolve(env, work)
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
	if strings.TrimSpace(providerSandboxID) != "" {
		persistedMetadata := work.Metadata
		if preparation != nil {
			persistedMetadata = preparation.persistedWorkMetadata
		}
		nextPersistedMetadata, err := patchJSONMetadata(persistedMetadata, map[string]any{
			"provider_sandbox_id": providerSandboxID,
		})
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
		updatedWork, err := r.db.UpdateEnvironmentWorkMetadata(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, nextPersistedMetadata)
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
		nextRuntimeMetadata, err := patchJSONMetadata(work.Metadata, map[string]any{
			"provider_sandbox_id": providerSandboxID,
		})
		if err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
		updatedWork.Metadata = nextRuntimeMetadata
		*work = updatedWork
	}

	manifest, provision, err := buildPackageManifest(env.Config)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if provision {
		if err := r.provisionPackages(ctx, work.ExternalID, providerSandboxID, manifest); err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
	}

	heartbeat, err := r.db.HeartbeatEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, "", 60, formatTime)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if !heartbeat.LeaseExtended {
		if err := r.stopCreatedSandbox(record, work, providerSandboxID); err != nil {
			return true, err
		}
		return true, nil
	}
	if err := r.ensureManagedAgentSessionLaunchable(ctx, preparation); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}

	// 只有 Cloud Session Managed Agent 使用固定的四组 Filestore 挂载。
	// 必须等 rclone ready 后才能继续，确保 Claude 启动时 uploads、outputs、
	// transcripts 和 tool_results 已经可用。
	if preparation != nil {
		rcloneLaunch, err := r.prepareRcloneFilestoreLaunch(ctx, preparation.session)
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

	launch, err := r.commitManagedAgentLaunch(ctx, env, work, preparation)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}

	if launch != nil {
		// rclone 和固定挂载已就绪；manager 随后通过 stdin 取得双凭证，
		// 并在启动 Claude 前 register worker，建立首个 CCR lease。
		if err := r.provider.StartBackgroundCommand(ctx, providerSandboxID, launch.Manager.ShellCommand, launch.Manager.Payload); err != nil {
			publicError := logManagedAgentRuntimeStageFailure(
				"environment_manager_start",
				errEnvironmentManagerStart,
				err,
			)
			r.failManagedAgentRuntime(ctx, record, work, providerSandboxID, preparation.session, launch.CodeSessionID, publicError)
			return true, publicError
		}
	}

	// true 表示本轮确实消费了一条 Work；nil 表示所需启动阶段全部完成。
	return true, nil
}

func (r *Runner) provisionPackages(ctx context.Context, workExternalID, sandboxID string, manifest []byte) (err error) {
	// Provisioning 独占一个 runner worker slot 直到返回，因此起始行是排队诊断的唯一
	// 依据：安装挂住时只有它会出现。两条日志都不含 spec、manifest 或 Sandbox 输出。
	startedAt := time.Now()
	log.Printf("environment packages provisioning start work_id=%s", workExternalID)
	defer func() {
		log.Printf(
			"environment packages provisioning done work_id=%s duration_ms=%d ok=%t",
			workExternalID,
			time.Since(startedAt).Milliseconds(),
			err == nil,
		)
	}()
	result, runErr := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: packageProvisionCommand,
		Stdin:   manifest,
		Timeout: r.cfg.E2B.SandboxTimeout,
	})
	if runErr != nil {
		return fmt.Errorf("provision environment packages: %w", runErr)
	}
	return validatePackageProvisioningResult(result)
}

func (r *Runner) stopCreatedSandbox(record db.EnvironmentSandbox, work *db.EnvironmentWork, providerSandboxID string) error {
	return runSandboxStopPhases(
		2*time.Minute,
		2*time.Minute,
		func(killCtx context.Context) (error, error) {
			var phaseErr error
			if strings.TrimSpace(providerSandboxID) == "" {
				return nil, nil
			}
			if err := r.db.UpdateEnvironmentSandboxState(killCtx, record.WorkspaceID, record.ExternalID, "stopping", &providerSandboxID, nil, nil); err != nil {
				phaseErr = errors.Join(phaseErr, err)
			}
			killErr := r.provider.Kill(killCtx, providerSandboxID)
			return killErr, errors.Join(phaseErr, killErr)
		},
		func(cleanupCtx context.Context, killErr error) error {
			var cleanupErr error
			if killErr != nil {
				message := killErr.Error()
				cleanupErr = errors.Join(cleanupErr, r.db.UpdateEnvironmentSandboxState(cleanupCtx, record.WorkspaceID, record.ExternalID, "failed", &providerSandboxID, &message, nil))
			} else {
				stoppedAt := time.Now().UTC()
				cleanupErr = errors.Join(cleanupErr, r.db.UpdateEnvironmentSandboxState(cleanupCtx, record.WorkspaceID, record.ExternalID, "stopped", &providerSandboxID, nil, &stoppedAt))
			}
			_, stopWorkErr := r.db.StopEnvironmentWork(cleanupCtx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
			return errors.Join(cleanupErr, stopWorkErr)
		},
	)
}

func runSandboxStopPhases(
	killTimeout time.Duration,
	cleanupTimeout time.Duration,
	killPhase func(context.Context) (killErr error, phaseErr error),
	cleanupPhase func(context.Context, error) error,
) error {
	killCtx, cancelKill := context.WithTimeout(context.Background(), killTimeout)
	killErr, phaseErr := killPhase(killCtx)
	cancelKill()

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), cleanupTimeout)
	cleanupErr := cleanupPhase(cleanupCtx, killErr)
	cancelCleanup()
	return errors.Join(phaseErr, cleanupErr)
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

func (r *Runner) prepareManagedAgentLaunch(ctx context.Context, env db.Environment, work *db.EnvironmentWork) (*managedAgentLaunchPreparation, error) {
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
	persistedWorkMetadata := append(json.RawMessage(nil), work.Metadata...)
	workMetadataPatch := map[string]any{}
	if skillMount != nil {
		workMetadataPatch[e2bruntime.SkillMountMetadataKey] = skillMount
	}
	if len(workMetadataPatch) > 0 {
		nextWorkMetadata, err := patchJSONMetadata(work.Metadata, workMetadataPatch)
		if err != nil {
			return nil, err
		}
		work.Metadata = nextWorkMetadata
	}
	return &managedAgentLaunchPreparation{
		session:               session,
		sessionConfig:         sessionConfig,
		persistedWorkMetadata: persistedWorkMetadata,
		skillMount:            skillMount,
		workDir:               workDir,
		title:                 title,
		model:                 modelIDFromAgentSnapshot(session.AgentSnapshot),
	}, nil
}

func (r *Runner) commitManagedAgentLaunch(
	ctx context.Context,
	env db.Environment,
	work *db.EnvironmentWork,
	preparation *managedAgentLaunchPreparation,
) (*managedAgentRuntimeLaunch, error) {
	if preparation == nil {
		return nil, nil
	}
	workPreparationMetadata := codesessions.ManagedAgentWorkPreparationMetadata{}
	if preparation.skillMount != nil {
		workPreparationMetadata.SkillMount = &codesessions.ManagedAgentSkillMountMetadata{
			MountPath:      preparation.skillMount.MountPath,
			VolumeName:     preparation.skillMount.VolumeName,
			ManifestSHA256: preparation.skillMount.ManifestSHA256,
			Skills:         preparation.skillMount.Skills,
		}
	}
	local, err := r.codeSessions.CreateManagedAgentCodeSession(ctx, codesessions.ManagedAgentCreateInput{
		Session:                    preparation.session,
		Environment:                env,
		EnvironmentWork:            *work,
		Model:                      preparation.model,
		Title:                      preparation.title,
		WorkDir:                    preparation.workDir,
		PermissionMode:             "bypassPermissions",
		DangerouslySkipPermissions: true,
		Config:                     preparation.sessionConfig,
		WorkPreparationMetadata:    workPreparationMetadata,
	})
	if err != nil {
		return nil, err
	}
	*work = local.EnvironmentWork

	payload, err := buildEnvironmentManagerV0Payload(
		local.CodeSessionID,
		local.SessionIngressToken,
		local.OAuthAccessToken,
		preparation.workDir,
		preparation.sessionConfig,
		r.cfg,
	)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.codeSessions.TerminateManagedAgentCodeSession(
			cleanupCtx,
			preparation.session,
			local.CodeSessionID,
		)
		return nil, err
	}
	return &managedAgentRuntimeLaunch{
		CodeSessionID: local.CodeSessionID,
		Manager:       buildEnvironmentManagerCommand(local.CodeSessionID, r.cfg, payload),
	}, nil
}

func (r *Runner) ensureManagedAgentSessionLaunchable(
	ctx context.Context,
	preparation *managedAgentLaunchPreparation,
) error {
	if preparation == nil {
		return nil
	}
	session, err := r.db.GetSession(
		ctx,
		preparation.session.WorkspaceID,
		preparation.session.ExternalID,
	)
	if err != nil {
		return err
	}
	if session.ArchivedAt != nil || session.Status != "idle" {
		return db.ErrInvalidState
	}
	preparation.session = session
	return nil
}

func (r *Runner) startRcloneFilestore(ctx context.Context, sandboxID string, launch rcloneFilestoreLaunch) error {
	if err := r.provider.WriteFile(ctx, sandboxID, rcloneConfigPath, launch.ConfigPayload); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("config_write", errRcloneConfigWrite, err)
	}
	if err := r.runSandboxCommand(ctx, sandboxID, rcloneConfigPermissionsCommand(), rcloneCommandGraceTimeout); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("config_permissions", errRcloneConfigPermissions, err)
	}
	if err := r.provider.StartBackgroundCommand(ctx, sandboxID, rcloneStartCommand(), nil); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("process_start", errRcloneProcessStart, err)
	}
	if err := r.waitForRcloneReady(ctx, sandboxID, rcloneReadyPollInterval, rcloneReadyTimeout); err != nil {
		_ = r.runSandboxCommand(ctx, sandboxID, rcloneConfigCleanupCommand(), rcloneCommandGraceTimeout)
		return logRcloneStageFailure("readiness", errRcloneReadiness, err)
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

func (r *Runner) runSandboxCommand(
	ctx context.Context,
	sandboxID string,
	command string,
	timeout time.Duration,
) error {
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: command,
		Timeout: timeout,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("sandbox command exited with code %d", result.ExitCode)
	}
	return nil
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
	return r.skills.ResolveAgentSnapshot(ctx, session.WorkspaceID, session.AgentSnapshot)
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

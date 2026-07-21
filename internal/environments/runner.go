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
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
)

type Runner struct {
	db           *db.DB
	provider     e2bruntime.Provider
	cfg          config.Config
	codeSessions *codesessions.Service
	skills       *skillsapi.RuntimeResolver
}

type managedAgentLaunchPreparation struct {
	session               db.Session
	sessionConfig         json.RawMessage
	events                []json.RawMessage
	persistedWorkMetadata json.RawMessage
	skillMount            *e2bruntime.SkillMount
	workDir               string
	title                 string
	model                 string
}

func NewRunner(database *db.DB, provider e2bruntime.Provider) *Runner {
	return &Runner{db: database, provider: provider}
}

func NewRunnerWithConfigStoreAndCredentials(database *db.DB, provider e2bruntime.Provider, cfg config.Config, store storage.ObjectStore, credentials *codesessions.SessionCredentials) *Runner {
	// 显式注入用于 main 和测试，确保不会在同一进程中意外创建第二套签名身份。
	return &Runner{
		db:           database,
		provider:     provider,
		cfg:          cfg,
		codeSessions: codesessions.NewServiceWithCredentials(database, credentials),
		skills:       skillsapi.NewRuntimeResolver(cfg, database, store),
	}
}

func StartRunnerWithStoreAndCredentials(ctx context.Context, database *db.DB, store storage.ObjectStore, cfg config.Config, credentials *codesessions.SessionCredentials) {
	if !cfg.EnvironmentRunner.Enabled {
		return
	}
	concurrency := cfg.EnvironmentRunner.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	runner := NewRunnerWithConfigStoreAndCredentials(database, e2bruntime.NewProvider(cfg.E2B), cfg, store, credentials)
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
	launchPreparation, err := r.prepareManagedAgentLaunch(ctx, env, work)
	if err != nil {
		_, _ = r.db.StopEnvironmentWork(ctx, work.WorkspaceID, work.EnvironmentExternalID, work.ExternalID, true)
		return true, err
	}
	resolution, err := r.provider.Resolve(env, work)
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
	if strings.TrimSpace(providerSandboxID) != "" {
		persistedMetadata := work.Metadata
		if launchPreparation != nil {
			persistedMetadata = launchPreparation.persistedWorkMetadata
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
		if err := r.provisionPackages(ctx, providerSandboxID, manifest); err != nil {
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
	launch, err := r.commitManagedAgentLaunch(ctx, env, work, launchPreparation)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if err := r.db.UpdateEnvironmentSandboxState(ctx, record.WorkspaceID, record.ExternalID, "running", &providerSandboxID, nil, nil); err != nil {
		r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
		return true, err
	}
	if launch != nil {
		// 先建立 environment runtime 状态，再把双凭证直接写入后台进程 stdin。
		// environment-manager 会在启动 Claude 前 register worker，建立首个 CCR lease。
		if err := r.provider.StartBackgroundCommand(ctx, providerSandboxID, launch.ShellCommand, launch.Payload); err != nil {
			r.failCreatedSandbox(ctx, record, work, providerSandboxID, err)
			return true, err
		}
	}
	return true, nil
}

func (r *Runner) provisionPackages(ctx context.Context, sandboxID string, manifest []byte) error {
	if err := r.provider.WriteFile(ctx, sandboxID, packageManifestPath, manifest); err != nil {
		return fmt.Errorf("write packages manifest: %w", err)
	}
	if err := r.provider.WriteFile(ctx, sandboxID, packageProvisionerPath, packageProvisionerV1); err != nil {
		return fmt.Errorf("write package provisioner: %w", err)
	}
	if err := r.provider.RunCommand(ctx, sandboxID, packageProvisionCommand, r.cfg.E2B.SandboxTimeout); err != nil {
		return fmt.Errorf("provision environment packages: %w", err)
	}
	return nil
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
	sessionConfig := managedAgentSessionConfig(session, resources)
	workDir := managedAgentWorkDir(resources)
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
		events:                events,
		persistedWorkMetadata: persistedWorkMetadata,
		skillMount:            skillMount,
		workDir:               workDir,
		title:                 title,
		model:                 modelIDFromAgentSnapshot(session.AgentSnapshot),
	}, nil
}

func (r *Runner) commitManagedAgentLaunch(ctx context.Context, env db.Environment, work *db.EnvironmentWork, preparation *managedAgentLaunchPreparation) (*environmentManagerCommand, error) {
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
		InitialEvents:              preparation.events,
		WorkPreparationMetadata:    workPreparationMetadata,
	})
	if err != nil {
		return nil, err
	}
	*work = local.EnvironmentWork

	payload, err := buildEnvironmentManagerV0Payload(local.CodeSessionID, local.SessionIngressToken, local.OAuthAccessToken, preparation.workDir, preparation.sessionConfig, r.cfg)
	if err != nil {
		return nil, err
	}
	command := buildEnvironmentManagerCommand(local.CodeSessionID, r.cfg, payload)
	return &command, nil
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

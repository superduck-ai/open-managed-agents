package environments

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/yourbatis"
)

const PrebuildQueue = "environment_prebuild"

// River owns execution and retention; Environment owns the resulting template.
type Prebuilds struct {
	db          *db.DB
	cfg         config.EnvironmentPrebuildConfig
	providerKey string
	images      imageBuilder
	templates   templateBuilder
	client      *river.Client[*sql.Tx]
}

func NewPrebuilds(database *db.DB, cfg config.Config) *Prebuilds {
	settings := cfg.EnvironmentPrebuilds
	return &Prebuilds{db: database, cfg: settings, providerKey: prebuildProviderKey(cfg),
		images:    &aliyunFlowImageBuilder{http: newBuildHTTP("x-yunxiao-token", settings.Image.Flow.Token), pipelineURL: settings.Image.Flow.PipelineURL},
		templates: &cubeSandboxTemplateBuilder{http: newBuildHTTP("Authorization", "Bearer "+cfg.E2B.APIKey), apiURL: strings.TrimRight(cfg.E2B.APIURL, "/"), cfg: settings.Template}}
}
func (svc *Prebuilds) Register(workers *river.Workers) {
	river.AddWorker(workers, &prebuildWorker{service: svc})
}
func (svc *Prebuilds) Configure(client *river.Client[*sql.Tx]) { svc.client = client }

type prebuildJobArgs struct {
	WorkspaceUUID   string `json:"workspace_uuid"`
	EnvironmentUUID string `json:"environment_uuid"`
	ProviderKey     string `json:"provider_key"`
	Dockerfile      string `json:"dockerfile"`
}

func (prebuildJobArgs) Kind() string { return PrebuildQueue }
func (prebuildJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: PrebuildQueue, MaxAttempts: 1}
}

// Only remote checkpoints are stored in River's output, not a second job state machine.
type prebuildJobOutput struct {
	ImageJobRef      buildJobRef `json:"image_job_ref,omitempty"`
	TemplateJobRef   buildJobRef `json:"template_job_ref,omitempty"`
	ImageRef         string      `json:"image_ref,omitempty"`
	TemplateID       string      `json:"template_id,omitempty"`
	Submitting       bool        `json:"submitting,omitempty"`
	OutcomeUncertain bool        `json:"unknown,omitempty"`
	CancelRequested  bool        `json:"cancel_requested,omitempty"`
	CancelSent       bool        `json:"cancel_sent,omitempty"`
	Message          string      `json:"message,omitempty"`
}
type prebuildTask struct {
	job    *river.Job[prebuildJobArgs]
	output prebuildJobOutput
}

func (task prebuildTask) stage() string {
	if task.output.ImageRef != "" {
		return "template"
	}
	return "image"
}
func (task prebuildTask) ref() buildJobRef {
	if task.stage() == "image" {
		return task.output.ImageJobRef
	}
	return task.output.TemplateJobRef
}
func (task prebuildTask) state() string {
	if task.output.TemplateID != "" {
		return "ready"
	}
	if task.job == nil {
		return "failed"
	}
	switch task.job.State {
	case rivertype.JobStateCancelled:
		return "cancelled"
	case rivertype.JobStateDiscarded:
		if task.output.OutcomeUncertain || task.output.Submitting {
			return "unknown"
		}
		return "failed"
	}
	if task.output.Submitting {
		return "submitting"
	}
	if task.output.CancelRequested {
		return "canceling"
	}
	if task.ref() != "" {
		return "running"
	}
	return "queued"
}
func (task prebuildTask) active() bool {
	return task.job != nil && task.job.State != rivertype.JobStateCompleted && task.job.State != rivertype.JobStateCancelled && task.job.State != rivertype.JobStateDiscarded
}
func (svc *Prebuilds) capabilities(stage string) buildCapabilities {
	if stage == "image" {
		return svc.images.Capabilities()
	}
	return svc.templates.Capabilities()
}

func (svc *Prebuilds) loadCurrentPrebuild(ctx context.Context, tx *yourbatis.Tx, env db.Environment) (prebuildTask, error) {
	if env.BuildJobID == nil {
		return prebuildTask{}, nil
	}
	if svc == nil || svc.client == nil {
		return prebuildTask{}, errPrebuildUnavailable
	}
	var row *rivertype.JobRow
	var err error
	if tx == nil {
		row, err = svc.client.JobGet(ctx, *env.BuildJobID)
	} else {
		row, err = svc.client.JobGetTx(ctx, tx.SQLTx(), *env.BuildJobID)
	}
	if errors.Is(err, river.ErrNotFound) {
		return prebuildTask{}, nil
	}
	if err != nil {
		return prebuildTask{}, err
	}
	if row.Kind != PrebuildQueue {
		return prebuildTask{}, errPrebuildConflict
	}
	task, err := decodePrebuildTask(row)
	if err != nil {
		return prebuildTask{}, err
	}
	if task.job.Args.WorkspaceUUID != env.WorkspaceUUID || task.job.Args.EnvironmentUUID != env.UUID {
		return prebuildTask{}, errPrebuildConflict
	}
	return task, nil
}
func decodePrebuildTask(row *rivertype.JobRow) (prebuildTask, error) {
	task := prebuildTask{job: &river.Job[prebuildJobArgs]{JobRow: row}}
	if err := json.Unmarshal(row.EncodedArgs, &task.job.Args); err != nil {
		return task, err
	}
	var metadata struct {
		Output prebuildJobOutput `json:"output"`
	}
	err := json.Unmarshal(row.Metadata, &metadata)
	task.output = metadata.Output
	return task, err
}

// The environment update and job insertion commit together. Each job owns
// an immutable Dockerfile and a unique image tag, even for identical packages.
func (svc *Prebuilds) enqueue(ctx context.Context, tx *yourbatis.Tx, env *db.Environment, packages *environmentPackages) error {
	if svc.client == nil {
		return errPrebuildUnavailable
	}
	args := prebuildJobArgs{
		WorkspaceUUID:   env.WorkspaceUUID,
		EnvironmentUUID: env.UUID,
		ProviderKey:     svc.providerKey,
		Dockerfile:      packageDockerfile(svc.cfg.Image.BaseImage, packages),
	}
	result, err := svc.client.InsertTx(ctx, tx.SQLTx(), args, nil)
	if err != nil {
		return err
	}
	env.BuildJobID = &result.Job.ID
	env.ResolvedTemplate = ""
	return nil
}

func (svc *Prebuilds) reconcilePrebuild(ctx context.Context, tx *yourbatis.Tx, current *db.Environment, next *db.Environment) error {
	packages, err := decodeEnvironmentPackages(next.Config)
	if err != nil {
		return err
	}
	if current != nil {
		previous, err := decodeEnvironmentPackages(current.Config)
		if err != nil {
			return err
		}
		if samePackages(previous, packages) {
			next.BuildJobID = current.BuildJobID
			next.ResolvedTemplate = current.ResolvedTemplate
			return nil
		}
	}
	next.BuildJobID = nil
	if packages == nil || next.ArchivedAt != nil || svc == nil || !svc.cfg.Enabled {
		return nil
	}
	return svc.enqueue(ctx, tx, next, packages)
}

// The environment row serializes API mutations and worker checkpoints. A stale
// job cannot overwrite a new configuration, retry, or cancellation.
func (svc *Prebuilds) saveCheckpoint(ctx context.Context, task *prebuildTask) error {
	superseded := false
	err := svc.db.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		env, err := svc.db.LockEnvironmentByUUIDTx(ctx, tx, task.job.Args.WorkspaceUUID, task.job.Args.EnvironmentUUID)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
		superseded = errors.Is(err, db.ErrNotFound) || env.BuildJobID == nil || *env.BuildJobID != task.job.ID
		row, err := svc.client.JobGetTx(ctx, tx.SQLTx(), task.job.ID)
		if err != nil {
			return err
		}
		current, err := decodePrebuildTask(row)
		if err != nil {
			return err
		}
		task.output.CancelRequested = task.output.CancelRequested || current.output.CancelRequested || superseded
		if task.output.CancelRequested {
			task.output.TemplateID = ""
		}
		// Retain even a superseded job's remote reference so failed cancellation
		// can be retried after a submission races with an environment update.
		if _, err = svc.client.JobUpdateTx(ctx, tx.SQLTx(), task.job.ID, &river.JobUpdateParams{Output: task.output}); err != nil {
			return err
		}
		if task.output.TemplateID != "" {
			return svc.db.ResolveEnvironmentPrebuildTx(ctx, tx, env.WorkspaceUUID, env.UUID, task.job.ID, task.output.TemplateID)
		}
		return nil
	})
	if err != nil {
		return river.JobSnooze(prebuildPollInterval)
	}
	if superseded {
		return svc.cancelSuperseded(ctx, *task)
	}
	return nil
}

func (svc *Prebuilds) StartOrRetryPrebuild(ctx context.Context, env db.Environment) error {
	return svc.withLockedEnvironment(ctx, env, func(tx *yourbatis.Tx, current db.Environment) error {
		if hasPrebuiltTemplate(current) {
			return nil
		}
		task, err := svc.loadCurrentPrebuild(ctx, tx, current)
		if err != nil {
			return err
		}
		if task.active() {
			return nil
		}
		packages, err := decodeEnvironmentPackages(current.Config)
		if err != nil {
			return err
		}
		if packages == nil {
			return errPrebuildConflict
		}
		if err := svc.enqueue(ctx, tx, &current, packages); err != nil {
			return err
		}
		current.UpdatedAt = time.Now().UTC()
		_, err = svc.db.UpdateEnvironmentTx(ctx, tx, current)
		return err
	})
}

func (svc *Prebuilds) CancelPrebuild(ctx context.Context, env db.Environment, jobID int64) error {
	return svc.withLockedEnvironment(ctx, env, func(tx *yourbatis.Tx, current db.Environment) error {
		task, err := svc.loadCurrentPrebuild(ctx, tx, current)
		if err != nil {
			return err
		}
		if task.job == nil || task.job.ID != jobID || !task.active() || task.output.Submitting || task.output.TemplateID != "" {
			return errPrebuildConflict
		}
		if task.job.Args.ProviderKey != svc.providerKey {
			return errPrebuildUnavailable
		}
		if task.ref() != "" && !svc.capabilities(task.stage()).Cancel {
			return errPrebuildUnsupported
		}
		task.output.CancelRequested = true
		_, err = svc.client.JobUpdateTx(ctx, tx.SQLTx(), task.job.ID, &river.JobUpdateParams{Output: task.output})
		return err
	})
}

func (svc *Prebuilds) withLockedEnvironment(ctx context.Context, env db.Environment, apply func(*yourbatis.Tx, db.Environment) error) error {
	if svc == nil || !svc.cfg.Enabled {
		return errPrebuildUnavailable
	}
	return svc.db.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		current, err := svc.db.LockEnvironmentByUUIDTx(ctx, tx, env.WorkspaceUUID, env.UUID)
		if err != nil {
			return err
		}
		if current.ArchivedAt != nil {
			return errPrebuildConflict
		}
		return apply(tx, current)
	})
}

func (svc *Prebuilds) readLogs(ctx context.Context, task prebuildTask, stage, cursor string) (buildLogChunk, error) {
	if task.job == nil || !svc.cfg.Enabled || task.job.Args.ProviderKey != svc.providerKey {
		return buildLogChunk{}, errPrebuildUnavailable
	}
	if stage == "image" {
		return svc.images.ReadLogs(ctx, task.output.ImageJobRef, cursor)
	}
	return svc.templates.ReadLogs(ctx, task.output.TemplateJobRef, cursor)
}

// Keep argv and ordering aligned with environment-manager provision-packages v1.
// Exec-form RUN preserves specs literally and exposes package manager output to CI.
func packageDockerfile(base string, packages *environmentPackages) string {
	var output strings.Builder
	fmt.Fprintf(&output, "FROM --platform=linux/amd64 %s\nUSER root\nWORKDIR /home/user\n", base)
	run := func(command []string) {
		encoded, _ := json.Marshal(command)
		output.WriteString("RUN ")
		output.Write(encoded)
		output.WriteByte('\n')
	}
	batch := func(prefix, specs []string) {
		if len(specs) > 0 {
			run(append(prefix, specs...))
		}
	}
	if len(packages.APT) > 0 {
		run([]string{"apt-get", "update"})
	}
	batch([]string{"apt-get", "install", "-y", "--"}, packages.APT)
	batch([]string{"cargo", "install"}, packages.Cargo)
	batch([]string{"gem", "install"}, packages.Gem)
	for _, spec := range packages.Go {
		run([]string{"go", "install", spec})
	}
	batch([]string{"npm", "install", "--global", "--"}, packages.NPM)
	batch([]string{"pip", "install"}, packages.PIP)
	return output.String()
}

// Decoding normalizes absent/empty packages; installation order remains significant.
func samePackages(previous, next *environmentPackages) bool {
	if previous == nil || next == nil {
		return previous == next
	}
	other := next.specsByManager()
	for i, manager := range previous.specsByManager() {
		if !slices.Equal(*manager.specs, *other[i].specs) {
			return false
		}
	}
	return true
}

func prebuildProviderKey(cfg config.Config) string {
	settings := cfg.EnvironmentPrebuilds
	// Credentials may rotate; endpoints and pipeline identities may not silently move jobs.
	// omitempty keeps absent and empty network lists equivalent, as in provider requests.
	data, _ := json.Marshal(struct {
		PipelineURL        string   `json:"pipeline_url"`
		Repository         string   `json:"repository"`
		APIURL             string   `json:"api_url"`
		DiskSize           string   `json:"disk_size"`
		CPU                uint32   `json:"cpu"`
		Memory             uint32   `json:"memory"`
		DNSServers         []string `json:"dns_servers,omitempty"`
		AllowOutboundCIDRs []string `json:"allow_outbound_cidrs,omitempty"`
		DenyOutboundCIDRs  []string `json:"deny_outbound_cidrs,omitempty"`
		AllowInternet      bool     `json:"allow_internet"`
		InjectEgressCA     bool     `json:"inject_egress_ca"`
	}{
		PipelineURL: settings.Image.Flow.PipelineURL, Repository: settings.Image.Repository(),
		APIURL:   cfg.E2B.APIURL,
		DiskSize: settings.Template.DiskSize, CPU: settings.Template.CPU, Memory: settings.Template.Memory,
		DNSServers: settings.Template.Network.DNSServers, AllowOutboundCIDRs: settings.Template.Network.AllowOutboundCIDRs,
		DenyOutboundCIDRs: settings.Template.Network.DenyOutboundCIDRs,
		AllowInternet:     settings.Template.Network.AllowInternet, InjectEgressCA: settings.Template.Network.InjectEgressCA,
	})

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// The environment owns its completed template, independently of current build settings.
// Environments without a build retain runtime package installation.
func hasPrebuiltTemplate(env db.Environment) bool {
	return env.BuildJobID != nil && env.ResolvedTemplate != ""
}

package environments

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/riverqueue/river"
)

const prebuildPollInterval = 5 * time.Second

type prebuildWorker struct {
	river.WorkerDefaults[prebuildJobArgs]
	service *Prebuilds
}

func (worker *prebuildWorker) Work(ctx context.Context, job *river.Job[prebuildJobArgs]) error {
	svc := worker.service
	task, err := decodePrebuildTask(job.JobRow)
	if err != nil {
		return err
	}
	// Detect replacement before polling the provider, including when status reads fail.
	if err := svc.saveCheckpoint(ctx, &task); err != nil {
		return err
	}
	if task.output.TemplateID != "" {
		return nil
	}
	if task.output.Submitting {
		svc.logger.WarnContext(ctx, "environment prebuild submission interrupted",
			"stage", task.stage(), "job_id", task.job.ID,
			"environment_id", task.job.Args.EnvironmentUUID, "workspace_id", task.job.Args.WorkspaceUUID)
		return errPrebuildSubmissionUnknown
	}
	if !svc.cfg.Enabled || job.Args.ProviderKey != svc.providerKey || time.Since(job.CreatedAt) > svc.cfg.Timeout {
		task.output.OutcomeUncertain = true
		task.output.Message = "Observation stopped: timeout or provider configuration changed. The remote job may still be running."
		if err := svc.saveCheckpoint(ctx, &task); err != nil {
			return err
		}
		return errors.New(task.output.Message)
	}
	if task.ref() == "" {
		if task.output.CancelRequested {
			return river.JobCancel(errors.New("Build cancelled before submission."))
		}
		return svc.submit(ctx, &task)
	}
	status, err := svc.readStatus(ctx, &task)
	if err != nil {
		return river.JobSnooze(prebuildPollInterval)
	}
	task.output.Message = status.Message
	if err := svc.saveCheckpoint(ctx, &task); err != nil {
		return err
	}
	switch status.State {
	case "failed":
		return errors.New("Remote build failed; inspect build logs.")
	case "cancelled":
		return river.JobCancel(errors.New("Remote build cancelled."))
	case "succeeded":
		if task.output.CancelRequested {
			return river.JobCancel(errors.New("Build stopped after the current stage completed."))
		}
		if task.output.TemplateID != "" {
			return nil
		}
	default:
		if task.output.CancelRequested && !task.output.CancelSent {
			if err := svc.cancelRemote(ctx, task); err != nil {
				return river.JobSnooze(prebuildPollInterval)
			}
			task.output.CancelSent = true
			if err := svc.saveCheckpoint(ctx, &task); err != nil {
				return err
			}
		}
	}
	return river.JobSnooze(prebuildPollInterval)
}
func (svc *Prebuilds) submit(ctx context.Context, task *prebuildTask) error {
	// Commit the ambiguity guard before sending a non-idempotent remote request.
	task.output.Submitting = true
	if err := svc.saveCheckpoint(ctx, task); err != nil {
		return err
	}
	if task.output.CancelRequested {
		task.output.Submitting = false
		if err := svc.saveCheckpoint(ctx, task); err != nil {
			return err
		}
		return river.JobCancel(errors.New("Build cancelled before submission."))
	}
	var ref buildJobRef
	var err error
	if task.stage() == "image" {
		tag := task.job.Args.EnvironmentUUID + "-" + strconv.FormatInt(task.job.ID, 10)
		ref, err = svc.images.Start(ctx, imageBuildInput{Dockerfile: task.job.Args.Dockerfile, Repository: svc.cfg.Image.Repository(), Tag: tag})
	} else {
		ref, err = svc.templates.Start(ctx, task.output.ImageRef)
	}
	if err == nil && ref == "" {
		err = errPrebuildMissingBuildRef
	}
	if err != nil {
		return svc.recordSubmitFailure(ctx, task, err)
	}
	if task.stage() == "image" {
		task.output.ImageJobRef = ref
	} else {
		task.output.TemplateJobRef = ref
	}
	task.output.Submitting = false
	// Keep the returned remote reference in this worker until it is durable.
	// Snoozing after a failed write would reload only the older Submitting flag.
	superseded, err := retryCheckpointWrite(ctx, prebuildPollInterval, func() (bool, error) {
		return svc.persistCheckpoint(ctx, task)
	})
	if err != nil {
		return river.JobSnooze(0)
	}
	if superseded {
		return svc.cancelSuperseded(ctx, *task)
	}
	return river.JobSnooze(prebuildPollInterval)
}

// submitFailure is how a failed submission attempt must be reported. Definitive
// failures prove that no remote work started; every other failure leaves the
// remote outcome unknown.
type submitFailure struct {
	definitive bool
	status     int
	message    string
}

// classifySubmitFailure separates a provider rejection from a failure whose
// remote outcome cannot be proven. 4xx responses reject the request before the
// provider acts on it; transport loss, redirects, 5xx responses and missing
// references may follow an accepted submission.
func classifySubmitFailure(cause error) submitFailure {
	status, rejected := providerRejectionStatus(cause)
	if !rejected {
		return submitFailure{message: errPrebuildSubmissionUnknown.Error()}
	}
	return submitFailure{definitive: true, status: status, message: prebuildSubmissionRejectedMessage(status)}
}

// The ambiguity guard and the uncertainty flag must agree with the
// classification before the checkpoint is persisted: a rejection is a
// diagnosable failure, not an outcome the operator has to disambiguate against
// the provider.
func (failure submitFailure) apply(output *prebuildJobOutput) {
	output.Submitting = !failure.definitive
	output.OutcomeUncertain = !failure.definitive
	output.Message = failure.message
}

// recordSubmitFailure reports and persists the outcome of a failed submission
// attempt. A checkpoint write failure keeps the guard and snoozes the job, so
// the remote outcome is never discarded before it is recorded.
func (svc *Prebuilds) recordSubmitFailure(ctx context.Context, task *prebuildTask, cause error) error {
	failure := classifySubmitFailure(cause)
	failure.apply(&task.output)
	svc.logSubmitFailure(ctx, task, failure, cause)
	if err := svc.saveCheckpoint(ctx, task); err != nil {
		return err
	}
	if failure.definitive {
		return errors.Join(errPrebuildSubmissionRejected, cause)
	}
	return errors.Join(errPrebuildSubmissionUnknown, cause)
}

func (svc *Prebuilds) logSubmitFailure(ctx context.Context, task *prebuildTask, failure submitFailure, cause error) {
	attrs := []any{
		"stage", task.stage(), "job_id", task.job.ID,
		"environment_id", task.job.Args.EnvironmentUUID, "workspace_id", task.job.Args.WorkspaceUUID,
		"error", cause,
	}
	if failure.definitive {
		svc.logger.ErrorContext(ctx, "environment prebuild submission rejected", append(attrs, "status_code", failure.status)...)
		return
	}
	svc.logger.ErrorContext(ctx, "environment prebuild submission outcome unknown", attrs...)
}

func retryCheckpointWrite(ctx context.Context, delay time.Duration, write func() (bool, error)) (bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		superseded, err := write()
		if err == nil {
			return superseded, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (svc *Prebuilds) readStatus(ctx context.Context, task *prebuildTask) (buildJobStatus, error) {
	if task.stage() == "image" {
		status, err := svc.images.GetStatus(ctx, task.output.ImageJobRef)
		if err == nil && status.State == "succeeded" {
			if status.ImageRef == "" {
				return buildJobStatus{}, errPrebuildMissingOutput
			}
			task.output.ImageRef = status.ImageRef
		}
		return status.buildJobStatus, err
	}
	status, err := svc.templates.GetStatus(ctx, task.output.TemplateJobRef)
	if err == nil && status.State == "succeeded" {
		if status.TemplateID == "" {
			return buildJobStatus{}, errPrebuildMissingOutput
		}
		task.output.TemplateID = status.TemplateID
	}
	return status.buildJobStatus, err
}
func (svc *Prebuilds) cancelRemote(ctx context.Context, task prebuildTask) error {
	if task.stage() == "image" {
		return svc.images.Cancel(ctx, task.ref())
	}
	return svc.templates.Cancel(ctx, task.ref())
}

func (svc *Prebuilds) cancelSuperseded(ctx context.Context, task prebuildTask) error {
	if task.ref() != "" && task.job.Args.ProviderKey == svc.providerKey && svc.capabilities(task.stage()).Cancel {
		status, err := svc.readStatus(ctx, &task)
		if err != nil || (status.State != "succeeded" && status.State != "failed" && status.State != "cancelled") {
			if err := svc.cancelRemote(ctx, task); err != nil {
				if time.Since(task.job.CreatedAt) > svc.cfg.Timeout {
					return river.JobCancel(errors.Join(errPrebuildSuperseded, err))
				}
				return river.JobSnooze(prebuildPollInterval)
			}
		}
	}
	return river.JobCancel(errPrebuildSuperseded)
}

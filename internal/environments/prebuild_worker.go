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
		return errors.New("Submission outcome unknown; inspect the provider before retrying.")
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
	if err != nil || ref == "" {
		return errors.New("Submission outcome unknown; inspect the provider before retrying.")
	}
	if task.stage() == "image" {
		task.output.ImageJobRef = ref
	} else {
		task.output.TemplateJobRef = ref
	}
	task.output.Submitting = false
	if err := svc.saveCheckpoint(ctx, task); err != nil {
		return err
	}
	return river.JobSnooze(prebuildPollInterval)
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
				return river.JobSnooze(prebuildPollInterval)
			}
		}
	}
	return river.JobCancel(errPrebuildSuperseded)
}

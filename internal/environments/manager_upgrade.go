package environments

import (
	"context"
	"time"

	"golang.org/x/mod/semver"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

type managerUpgradeSteps struct {
	probe   func() (managerProcess, error)
	install func() error
	idle    func() (bool, error)
	stop    func(managerProcess) error
	start   func() error
	ready   func() (bool, error)
	phase   func(string) error
}

func (r *Runner) recordInstalledManagerVersion(ctx context.Context, sandboxID string, record db.EnvironmentSandbox) error {
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: shellQuote(environmentManagerPath) + " --version", Timeout: managerVersionProbeLimit,
	})
	if err != nil {
		return err
	}
	version, valid := parseEnvironmentManagerVersion(result.Stdout)
	if result.ExitCode != 0 || !valid {
		return errEnvironmentManagerInstall
	}
	return r.db.RecordManagerVersion(ctx, db.ManagerResumeTarget{
		OrganizationUUID: record.OrganizationUUID, WorkspaceUUID: record.WorkspaceUUID, SandboxUUID: record.UUID,
	}, version)
}

func upgradeManager(ctx context.Context, required, phase string, steps managerUpgradeSteps) error {
	process, err := steps.probe()
	if err != nil {
		return err
	}
	if process.Version != "" && semver.Compare("v"+process.Version, "v"+required) >= 0 {
		if phase == "starting" {
			if err := waitManagerCondition(ctx, steps.ready); err != nil {
				return err
			}
		}
		return steps.phase("")
	}
	if err := steps.install(); err != nil {
		return err
	}
	if err := steps.phase("draining"); err != nil {
		return err
	}
	if err := waitManagerCondition(ctx, steps.idle); err != nil {
		return err
	}
	if process.PID != 0 {
		if err := steps.stop(process); err != nil {
			return err
		}
	}
	if err := steps.phase("starting"); err != nil {
		return err
	}
	if err := steps.start(); err != nil {
		return err
	}
	if err := waitManagerCondition(ctx, func() (bool, error) {
		current, err := steps.probe()
		if err != nil {
			return false, err
		}
		if current.PID == 0 {
			return false, nil
		}
		if semver.Compare("v"+current.Version, "v"+required) < 0 {
			return false, errManagerVersionBelowRequired
		}
		return steps.ready()
	}); err != nil {
		return err
	}
	return steps.phase("")
}

func waitManagerCondition(ctx context.Context, condition func() (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ready, err := condition()
		if err != nil || ready {
			return err
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *Runner) upgradeResumedManager(ctx context.Context, sandboxID string, target db.ManagerResumeTarget) error {
	return upgradeManager(ctx, r.managerArtifacts.cfg.Version, target.Phase, managerUpgradeSteps{
		probe:   func() (managerProcess, error) { return r.inspectManagerProcess(ctx, sandboxID, target.CodeSessionID) },
		install: func() error { return r.installEnvironmentManager(ctx, sandboxID) },
		idle:    func() (bool, error) { return r.db.IsManagerResumeIdle(ctx, target) },
		stop: func(process managerProcess) error {
			return r.runSandboxCommand(ctx, sandboxID, managerProcessStopCommand(process), 20*time.Second)
		},
		start: func() error { return r.startResumedManager(ctx, sandboxID, target) },
		ready: func() (bool, error) {
			return r.db.IsSandboxToolCallReady(ctx, target.OrganizationUUID, target.WorkspaceUUID, target.SandboxUUID)
		},
		phase: func(phase string) error {
			if phase == "" {
				process, err := r.inspectManagerProcess(ctx, sandboxID, target.CodeSessionID)
				if err != nil {
					return err
				}
				if process.PID == 0 {
					return errEnvironmentManagerStart
				}
				if err := r.db.RecordManagerVersion(ctx, target, process.Version); err != nil {
					return err
				}
			}
			return r.db.SetManagerResumePhase(ctx, target, phase)
		},
	})
}

func (r *Runner) startResumedManager(ctx context.Context, sandboxID string, target db.ManagerResumeTarget) error {
	session, found, err := r.db.GetSession(ctx, target.WorkspaceUUID, target.SessionID)
	if err != nil {
		return err
	}
	if !found || session.OrganizationUUID != target.OrganizationUUID || session.ArchivedAt != nil || session.DeletedAt != nil || session.Status == "terminated" {
		return db.ErrInvalidState
	}
	token, err := r.codeSessions.SandboxIngressToken(ctx, session, target.CodeSessionID, target.SandboxUUID)
	if err != nil {
		return err
	}
	placeholders, err := r.prepareEnvCredentialPlaceholders(ctx, session)
	if err != nil {
		return err
	}
	cfg := r.cfg
	cfg.EnvironmentRunner.AgentMode = "brain"
	manager, err := brainSandboxManagerCommand(target.CodeSessionID, target.WorkDir, cfg, placeholders, token, nil)
	if err != nil {
		return err
	}
	return r.provider.StartBackgroundCommand(ctx, sandboxID, environmentManagerShellCommand(target.CodeSessionID, cfg), manager.Payload)
}

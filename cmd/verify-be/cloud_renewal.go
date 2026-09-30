package main

import (
	"context"
	"errors"
	"time"

	e2b "github.com/superduck-ai/e2b-go-sdk"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func verifyCloudRenewal(ctx context.Context, cfg cloudConfig, directory string, r *report, stage func(string)) (result error) {
	provider := e2bruntime.NewProvider(cfg.E2B)
	env := db.Environment{ExternalID: r.RunID, WorkspaceUUID: r.RunID, ResolvedTemplate: cfg.E2B.Template}
	resolution := e2bruntime.Resolution{Template: cfg.E2B.Template, Timeout: time.Minute, Metadata: map[string]string{"verify_be_run": r.RunID}, AllowInternetAccess: false}
	r.CleanupComplete = false
	sandbox, err := provider.Create(ctx, env, nil, resolution)
	if err != nil {
		cloudCleanup(r, err)
		return errors.New("cloud sandbox creation failed; check provider resources tagged with this run ID if the request outcome is unknown")
	}
	r.CleanupComplete = false
	opts := &e2b.SandboxApiOpts{ApiKey: cfg.E2B.APIKey, Domain: cfg.E2B.Domain, ApiUrl: cfg.E2B.APIURL}
	if cfg.E2B.AccessToken != "" {
		opts.Headers = map[string]string{"Authorization": "Bearer " + cfg.E2B.AccessToken}
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cleanupErr := provider.Kill(cleanupCtx, sandbox.ID)
		if cleanupErr == nil {
			_, infoErr := e2b.GetInfo(cleanupCtx, sandbox.ID, opts)
			var missing *e2b.SandboxNotFoundError
			if !errors.As(infoErr, &missing) {
				cleanupErr = errors.New("cannot confirm sandbox absence")
			}
		}
		cloudCleanup(r, cleanupErr)
		if cleanupErr != nil {
			result = errors.Join(result, errors.New("cloud sandbox cleanup failed"))
			return
		}
		r.CleanupComplete = true
		if result == nil {
			stage("cloud_sandbox_deleted")
		}
	}()
	if err := saveCloudResources(directory, cloudResources{SandboxID: sandbox.ID}); err != nil {
		return err
	}
	before, err := e2b.GetInfo(ctx, sandbox.ID, opts)
	if err != nil {
		return errors.New("cannot observe initial cloud sandbox deadline")
	}
	stage("cloud_sandbox_created")
	started := time.Now()
	if err := provider.SetTimeout(ctx, sandbox.ID, 3*time.Minute); err != nil {
		return errors.New("production E2B provider renewal failed")
	}
	after, err := e2b.GetInfo(ctx, sandbox.ID, opts)
	if err != nil || !after.EndAt.After(before.EndAt) || after.EndAt.Before(started.Add(150*time.Second)) || after.EndAt.After(time.Now().Add(210*time.Second)) {
		return errors.New("cloud sandbox deadline did not reflect the requested renewal")
	}
	stage("cloud_timeout_extended")
	return nil
}

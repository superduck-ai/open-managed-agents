package agentruntime

import (
	"context"
	"log/slog"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type ProfileStore interface {
	GetRuntimeProfileByUUID(ctx context.Context, profileUUID string) (db.RuntimeProfile, error)
	GetWorkspaceRuntimeProfile(ctx context.Context, organizationUUID, workspaceUUID string) (db.RuntimeProfile, error)
	UpdateRuntimeProfileHealth(ctx context.Context, profileUUID, healthStatus, healthError string, checkedAt time.Time) error
}

type SecretOpener interface {
	OpenRuntimeProfile(ctx context.Context, binding secrets.RuntimeProfileBinding, envelope secrets.Envelope) ([]byte, error)
}

type Resolver struct {
	store     ProfileStore
	registry  *Registry
	secretSvc SecretOpener
	logger    *slog.Logger
}

func NewResolver(store ProfileStore, registry *Registry, secretSvc SecretOpener, logger *slog.Logger) *Resolver {
	return &Resolver{
		store:     store,
		registry:  registry,
		secretSvc: secretSvc,
		logger:    logging.LoggerOrDefault(logger),
	}
}

func (r *Resolver) ProfileCapabilities(profile db.RuntimeProfile) (CapabilitySet, error) {
	base := DefaultCapabilities(ProviderKind(profile.ProviderKind))
	return MergeCapabilityOverrides(base, profile.CapabilityOverrides)
}

func (r *Resolver) WorkspaceCapabilities(ctx context.Context, organizationUUID, workspaceUUID string) (CapabilitySet, db.RuntimeProfile, error) {
	if r == nil || r.store == nil {
		return CapabilitySet{}, db.RuntimeProfile{}, UnavailableError("runtime resolver unavailable", nil)
	}
	profile, err := r.store.GetWorkspaceRuntimeProfile(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return CapabilitySet{}, db.RuntimeProfile{}, MapDBError(err)
	}
	caps, err := r.ProfileCapabilities(profile)
	if err != nil {
		return CapabilitySet{}, db.RuntimeProfile{}, err
	}
	return caps, profile, nil
}

func (r *Resolver) ResolveProfile(ctx context.Context, profileUUID string) (Runtime, db.RuntimeProfile, error) {
	if r == nil || r.store == nil {
		return nil, db.RuntimeProfile{}, UnavailableError("runtime resolver unavailable", nil)
	}
	profile, err := r.store.GetRuntimeProfileByUUID(ctx, profileUUID)
	if err != nil {
		return nil, db.RuntimeProfile{}, MapDBError(err)
	}
	rt, err := r.buildRuntime(ctx, profile)
	if err != nil {
		return nil, profile, err
	}
	return rt, profile, nil
}

func (r *Resolver) ResolveWorkspace(ctx context.Context, organizationUUID, workspaceUUID string) (Runtime, db.RuntimeProfile, error) {
	if r == nil || r.store == nil {
		return nil, db.RuntimeProfile{}, UnavailableError("runtime resolver unavailable", nil)
	}
	profile, err := r.store.GetWorkspaceRuntimeProfile(ctx, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, db.RuntimeProfile{}, MapDBError(err)
	}
	rt, err := r.buildRuntime(ctx, profile)
	if err != nil {
		return nil, profile, err
	}
	return rt, profile, nil
}

func (r *Resolver) HealthCheckProfile(ctx context.Context, profileUUID string) (HealthCheckResult, error) {
	if r == nil || r.store == nil {
		return HealthCheckResult{}, UnavailableError("runtime resolver unavailable", nil)
	}
	profile, err := r.store.GetRuntimeProfileByUUID(ctx, profileUUID)
	if err != nil {
		return HealthCheckResult{}, MapDBError(err)
	}
	started := time.Now().UTC()
	if ProviderKind(profile.ProviderKind).IsNative() {
		result := HealthCheckResult{
			Status:    "healthy",
			Latency:   time.Since(started),
			Message:   "oma_native ready",
			CheckedAt: started,
		}
		_ = r.store.UpdateRuntimeProfileHealth(ctx, profile.UUID, result.Status, "", result.CheckedAt)
		return result, nil
	}
	rt, err := r.buildRuntime(ctx, profile)
	if err != nil {
		result := HealthCheckResult{
			Status:    "unhealthy",
			Latency:   time.Since(started),
			Message:   err.Error(),
			CheckedAt: time.Now().UTC(),
		}
		_ = r.store.UpdateRuntimeProfileHealth(ctx, profile.UUID, result.Status, result.Message, result.CheckedAt)
		return result, err
	}
	defer func() {
		if closeErr := rt.Close(); closeErr != nil {
			r.logger.WarnContext(ctx, "close runtime after health check failed", "profile_uuid", profile.UUID, "error", closeErr)
		}
	}()

	checker, ok := rt.(HealthChecker)
	if !ok {
		result := HealthCheckResult{
			Status:    "healthy",
			Latency:   time.Since(started),
			Message:   "provider initialized",
			CheckedAt: time.Now().UTC(),
		}
		_ = r.store.UpdateRuntimeProfileHealth(ctx, profile.UUID, result.Status, "", result.CheckedAt)
		return result, nil
	}
	result, checkErr := checker.HealthCheck(ctx)
	if result.CheckedAt.IsZero() {
		result.CheckedAt = time.Now().UTC()
	}
	if result.Latency == 0 {
		result.Latency = time.Since(started)
	}
	if checkErr != nil {
		if result.Status == "" {
			result.Status = "unhealthy"
		}
		if result.Message == "" {
			result.Message = checkErr.Error()
		}
		_ = r.store.UpdateRuntimeProfileHealth(ctx, profile.UUID, result.Status, result.Message, result.CheckedAt)
		return result, checkErr
	}
	if result.Status == "" {
		result.Status = "healthy"
	}
	_ = r.store.UpdateRuntimeProfileHealth(ctx, profile.UUID, result.Status, "", result.CheckedAt)
	return result, nil
}

func (r *Resolver) buildRuntime(ctx context.Context, profile db.RuntimeProfile) (Runtime, error) {
	if profile.Status != "active" {
		return nil, UnavailableError("runtime profile is not active", ErrProfileInactive)
	}
	caps, err := r.ProfileCapabilities(profile)
	if err != nil {
		return nil, err
	}
	var credentials []byte
	if profile.SecretEnvelope != nil {
		if r.secretSvc == nil {
			return nil, UnavailableError("runtime credential decryption unavailable", ErrCredentialsUnavailable)
		}
		binding := secrets.RuntimeProfileBinding{
			Scope:              profile.Scope,
			OrganizationUUID:   profile.OrganizationUUID,
			RuntimeProfileUUID: profile.UUID,
			CredentialRevision: profile.CredentialRevision,
		}
		credentials, err = r.secretSvc.OpenRuntimeProfile(ctx, binding, *profile.SecretEnvelope)
		if err != nil {
			return nil, UnavailableError("runtime credential decryption failed", err)
		}
		defer clear(credentials)
	}
	return r.registry.Build(ctx, ProfileRuntimeConfig{
		Profile:      profile,
		Credentials:  credentials,
		Capabilities: caps,
	})
}

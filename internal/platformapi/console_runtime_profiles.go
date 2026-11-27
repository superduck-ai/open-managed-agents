package platformapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	defaultOMANativeProfileExternalID = "rprof_oma_native_default"
	defaultOMANativeProfileName       = "OMA Native"
)

type consoleRuntimeProfileStore interface {
	GetOrganizationRuntimePolicy(ctx context.Context, organizationUUID string) (db.OrganizationRuntimePolicy, error)
	ListPlatformRuntimeProfiles(ctx context.Context) ([]db.RuntimeProfile, error)
	GetRuntimeProfileByUUID(ctx context.Context, profileUUID string) (db.RuntimeProfile, error)
	GetRuntimeProfileByExternalID(ctx context.Context, externalID string) (db.RuntimeProfile, error)
}

type consoleWorkspaceRuntimeDTO struct {
	ProfileUUID  string                     `json:"profile_uuid"`
	ProfileID    string                     `json:"profile_id"`
	ProfileName  string                     `json:"profile_name"`
	ProviderKind string                     `json:"provider_kind"`
	Capabilities agentruntime.CapabilitySet `json:"capabilities"`
}

type consoleRuntimeProfileSummaryDTO struct {
	UUID                  string                     `json:"uuid"`
	ID                    string                     `json:"id"`
	Name                  string                     `json:"name"`
	ProviderKind          string                     `json:"provider_kind"`
	EffectiveCapabilities agentruntime.CapabilitySet `json:"effective_capabilities"`
	LastHealthStatus      string                     `json:"last_health_status"`
}

type consoleRuntimeProfilesResponse struct {
	DefaultRuntimeProfileUUID string                            `json:"default_runtime_profile_uuid"`
	Profiles                  []consoleRuntimeProfileSummaryDTO `json:"profiles"`
}

func RegisterConsoleOrganizationRuntimeProfileRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/runtime-profiles", handleListConsoleRuntimeProfiles(store))
}

func asConsoleRuntimeProfileStore(store OrganizationStore) consoleRuntimeProfileStore {
	if database, ok := store.(*db.DB); ok && database == nil {
		return nil
	}
	runtimeStore, _ := store.(consoleRuntimeProfileStore)
	return runtimeStore
}

func handleListConsoleRuntimeProfiles(store OrganizationStore) http.HandlerFunc {
	runtimeStore := asConsoleRuntimeProfileStore(store)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if runtimeStore == nil {
			internalError(w, "failed to load runtime profiles")
			return
		}
		policy, err := runtimeStore.GetOrganizationRuntimePolicy(r.Context(), orgUUID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to load organization runtime policy")
			return
		}
		allProfiles, err := runtimeStore.ListPlatformRuntimeProfiles(r.Context())
		if err != nil {
			internalError(w, "failed to list runtime profiles")
			return
		}
		allowed := make(map[string]struct{}, len(policy.AllowedRuntimeProfileUUIDs))
		for _, id := range policy.AllowedRuntimeProfileUUIDs {
			allowed[id] = struct{}{}
		}
		summaries := make([]consoleRuntimeProfileSummaryDTO, 0, len(allProfiles))
		for _, profile := range allProfiles {
			if profile.Scope != "platform" || profile.Status != "active" {
				continue
			}
			if _, ok := allowed[profile.UUID]; !ok {
				continue
			}
			caps, capErr := effectiveProfileCapabilities(profile)
			if capErr != nil {
				caps = agentruntime.DefaultCapabilities(agentruntime.ProviderKind(profile.ProviderKind))
			}
			summaries = append(summaries, consoleRuntimeProfileSummaryDTO{
				UUID:                  profile.UUID,
				ID:                    profile.ExternalID,
				Name:                  profile.Name,
				ProviderKind:          profile.ProviderKind,
				EffectiveCapabilities: caps,
				LastHealthStatus:      profile.LastHealthStatus,
			})
		}
		writeJSON(w, http.StatusOK, consoleRuntimeProfilesResponse{
			DefaultRuntimeProfileUUID: policy.DefaultRuntimeProfileUUID,
			Profiles:                  summaries,
		})
	}
}

func effectiveProfileCapabilities(profile db.RuntimeProfile) (agentruntime.CapabilitySet, error) {
	base := agentruntime.DefaultCapabilities(agentruntime.ProviderKind(profile.ProviderKind))
	return agentruntime.MergeCapabilityOverrides(base, profile.CapabilityOverrides)
}

func loadRuntimeProfilesByUUID(ctx context.Context, store OrganizationStore) map[string]db.RuntimeProfile {
	runtimeStore := asConsoleRuntimeProfileStore(store)
	if runtimeStore == nil {
		return nil
	}
	profiles, err := runtimeStore.ListPlatformRuntimeProfiles(ctx)
	if err != nil {
		return nil
	}
	byUUID := make(map[string]db.RuntimeProfile, len(profiles))
	for _, profile := range profiles {
		byUUID[profile.UUID] = profile
	}
	return byUUID
}

func loadSingleRuntimeProfile(ctx context.Context, store OrganizationStore, profileUUID string) *db.RuntimeProfile {
	if profileUUID == "" {
		return nil
	}
	runtimeStore := asConsoleRuntimeProfileStore(store)
	if runtimeStore == nil {
		return nil
	}
	profile, err := runtimeStore.GetRuntimeProfileByUUID(ctx, profileUUID)
	if err != nil {
		return nil
	}
	return &profile
}

func buildConsoleWorkspaceRuntime(workspace ConsoleWorkspace, profile *db.RuntimeProfile) consoleWorkspaceRuntimeDTO {
	profileUUID := workspace.RuntimeProfileUUID
	profileID := workspace.RuntimeProfileExternalID
	profileName := workspace.RuntimeProfileName
	providerKind := workspace.RuntimeProviderKind
	if profile != nil {
		if profileUUID == "" {
			profileUUID = profile.UUID
		}
		if profileID == "" {
			profileID = profile.ExternalID
		}
		if profileName == "" {
			profileName = profile.Name
		}
		if providerKind == "" {
			providerKind = profile.ProviderKind
		}
	}
	if providerKind == "" {
		providerKind = agentruntime.ProviderOMANative.String()
	}
	if profileUUID == "" {
		profileUUID = db.DefaultOMANativeRuntimeProfileUUID
	}
	if providerKind == agentruntime.ProviderOMANative.String() {
		if profileID == "" {
			profileID = defaultOMANativeProfileExternalID
		}
		if profileName == "" {
			profileName = defaultOMANativeProfileName
		}
	}
	caps := agentruntime.DefaultCapabilities(agentruntime.ProviderKind(providerKind))
	if profile != nil {
		if merged, err := effectiveProfileCapabilities(*profile); err == nil {
			caps = merged
		}
	}
	return consoleWorkspaceRuntimeDTO{
		ProfileUUID:  profileUUID,
		ProfileID:    profileID,
		ProfileName:  profileName,
		ProviderKind: providerKind,
		Capabilities: caps,
	}
}

func resolveConsoleWorkspaceRuntimeProfile(
	ctx context.Context,
	store OrganizationStore,
	orgUUID string,
	requestedID string,
) (string, error) {
	trimmed := strings.TrimSpace(requestedID)
	runtimeStore := asConsoleRuntimeProfileStore(store)
	if runtimeStore == nil {
		if trimmed != "" {
			return "", errRuntimeProfileStoreUnavailable
		}
		return "", nil
	}
	policy, err := runtimeStore.GetOrganizationRuntimePolicy(ctx, orgUUID)
	if err != nil {
		return "", err
	}
	if trimmed == "" {
		profileUUID := policy.DefaultRuntimeProfileUUID
		if profileUUID == "" {
			profileUUID = db.DefaultOMANativeRuntimeProfileUUID
		}
		if len(policy.AllowedRuntimeProfileUUIDs) > 0 && !slices.Contains(policy.AllowedRuntimeProfileUUIDs, profileUUID) {
			return "", errRuntimeProfileNotAllowed
		}
		profile, err := runtimeStore.GetRuntimeProfileByUUID(ctx, profileUUID)
		if err != nil {
			return "", err
		}
		if profile.Status != "active" {
			return "", errRuntimeProfileInactive
		}
		return profile.UUID, nil
	}
	profile, err := lookupRuntimeProfileByIdentifier(ctx, runtimeStore, trimmed)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return "", errRuntimeProfileNotFound
		}
		return "", err
	}
	if profile.Status != "active" {
		return "", errRuntimeProfileInactive
	}
	if !slices.Contains(policy.AllowedRuntimeProfileUUIDs, profile.UUID) {
		return "", errRuntimeProfileNotAllowed
	}
	return profile.UUID, nil
}

func lookupRuntimeProfileByIdentifier(
	ctx context.Context,
	store consoleRuntimeProfileStore,
	identifier string,
) (db.RuntimeProfile, error) {
	if _, err := uuid.Parse(identifier); err == nil {
		return store.GetRuntimeProfileByUUID(ctx, identifier)
	}
	return store.GetRuntimeProfileByExternalID(ctx, identifier)
}

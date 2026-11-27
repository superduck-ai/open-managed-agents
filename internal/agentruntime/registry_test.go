package agentruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type stubRuntime struct {
	kind      agentruntime.ProviderKind
	caps      agentruntime.CapabilitySet
	creds     []byte
	closed    bool
	healthRes agentruntime.HealthCheckResult
	healthErr error
}

func (s *stubRuntime) ProviderKind() agentruntime.ProviderKind  { return s.kind }
func (s *stubRuntime) Capabilities() agentruntime.CapabilitySet { return s.caps }
func (s *stubRuntime) Close() error {
	s.closed = true
	return nil
}
func (s *stubRuntime) HealthCheck(_ context.Context) (agentruntime.HealthCheckResult, error) {
	return s.healthRes, s.healthErr
}

type fakeProfileStore struct {
	profiles      map[string]db.RuntimeProfile
	workspaceMap  map[string]string
	healthStatus  string
	healthError   string
	healthChecked bool
}

func (f *fakeProfileStore) GetRuntimeProfileByUUID(_ context.Context, profileUUID string) (db.RuntimeProfile, error) {
	p, ok := f.profiles[profileUUID]
	if !ok {
		return db.RuntimeProfile{}, db.ErrNotFound
	}
	return p, nil
}

func (f *fakeProfileStore) GetWorkspaceRuntimeProfile(_ context.Context, orgUUID, workspaceUUID string) (db.RuntimeProfile, error) {
	id, ok := f.workspaceMap[orgUUID+"/"+workspaceUUID]
	if !ok {
		return db.RuntimeProfile{}, db.ErrNotFound
	}
	return f.GetRuntimeProfileByUUID(context.Background(), id)
}

func (f *fakeProfileStore) UpdateRuntimeProfileHealth(_ context.Context, _ string, status, healthErr string, _ time.Time) error {
	f.healthStatus = status
	f.healthError = healthErr
	f.healthChecked = true
	return nil
}

type fakeBindingStore struct {
	byOMA    map[string]db.RuntimeResourceBinding
	byRemote map[string]db.RuntimeResourceBinding
}

func newFakeBindingStore() *fakeBindingStore {
	return &fakeBindingStore{
		byOMA:    make(map[string]db.RuntimeResourceBinding),
		byRemote: make(map[string]db.RuntimeResourceBinding),
	}
}

func omaKey(org, ws, rt, id string) string    { return org + "|" + ws + "|" + rt + "|" + id }
func remoteKey(profile, rt, id string) string { return profile + "|" + rt + "|" + id }

func (f *fakeBindingStore) UpsertRuntimeResourceBinding(_ context.Context, b db.RuntimeResourceBinding) (db.RuntimeResourceBinding, error) {
	f.byOMA[omaKey(b.OrganizationUUID, b.WorkspaceUUID, b.ResourceType, b.OMAExternalID)] = b
	f.byRemote[remoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return b, nil
}

func (f *fakeBindingStore) InsertRuntimeResourceBindingIfAbsent(_ context.Context, b db.RuntimeResourceBinding) (db.RuntimeResourceBinding, bool, error) {
	k := omaKey(b.OrganizationUUID, b.WorkspaceUUID, b.ResourceType, b.OMAExternalID)
	if _, exists := f.byOMA[k]; exists {
		return db.RuntimeResourceBinding{}, false, nil
	}
	f.byOMA[k] = b
	f.byRemote[remoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return b, true, nil
}

func (f *fakeBindingStore) GetRuntimeResourceBindingByOMAID(_ context.Context, org, ws, rt, id string) (db.RuntimeResourceBinding, error) {
	b, ok := f.byOMA[omaKey(org, ws, rt, id)]
	if !ok || b.State == "deleted" {
		return db.RuntimeResourceBinding{}, db.ErrNotFound
	}
	return b, nil
}

func (f *fakeBindingStore) GetRuntimeResourceBindingByRemoteID(_ context.Context, profile, rt, id string) (db.RuntimeResourceBinding, error) {
	b, ok := f.byRemote[remoteKey(profile, rt, id)]
	if !ok || b.State == "deleted" {
		return db.RuntimeResourceBinding{}, db.ErrNotFound
	}
	return b, nil
}

func (f *fakeBindingStore) ListRuntimeResourceBindingsByWorkspaceAndType(_ context.Context, p db.ListRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error) {
	var out []db.RuntimeResourceBinding
	for _, b := range f.byOMA {
		if b.OrganizationUUID == p.OrganizationUUID && b.WorkspaceUUID == p.WorkspaceUUID && b.ResourceType == p.ResourceType && b.State != "deleted" {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeBindingStore) SearchRuntimeResourceBindingsByWorkspaceAndType(_ context.Context, p db.SearchRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error) {
	var out []db.RuntimeResourceBinding
	for _, b := range f.byOMA {
		if b.OrganizationUUID == p.OrganizationUUID && b.WorkspaceUUID == p.WorkspaceUUID && b.ResourceType == p.ResourceType && b.State != "deleted" && strings.Contains(string(b.Summary), p.Query) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeBindingStore) UpdateRuntimeResourceBindingSummaryAndState(_ context.Context, org, ws, rt, id, state string, summary json.RawMessage) (db.RuntimeResourceBinding, error) {
	k := omaKey(org, ws, rt, id)
	b, ok := f.byOMA[k]
	if !ok || b.State == "deleted" {
		return db.RuntimeResourceBinding{}, db.ErrNotFound
	}
	if state != "" {
		b.State = state
	}
	b.Summary = summary
	f.byOMA[k] = b
	f.byRemote[remoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return b, nil
}

func (f *fakeBindingStore) SoftDeleteRuntimeResourceBinding(_ context.Context, org, ws, rt, id string) error {
	k := omaKey(org, ws, rt, id)
	b, ok := f.byOMA[k]
	if !ok || b.State == "deleted" {
		return db.ErrNotFound
	}
	b.State = "deleted"
	f.byOMA[k] = b
	f.byRemote[remoteKey(b.RuntimeProfileUUID, b.ResourceType, b.RemoteID)] = b
	return nil
}

func newTestSecrets(t *testing.T) *secrets.Service {
	t.Helper()
	kek, err := local.GenerateKEK()
	if err != nil {
		t.Fatalf("local.GenerateKEK: %v", err)
	}
	provider, err := local.New(local.KeyMaterial{KEK: kek}, nil)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	return secrets.NewService(provider)
}

func TestRegistryRegistrationAndBuild(t *testing.T) {
	reg := agentruntime.NewRegistry()
	if err := reg.Register(agentruntime.ProviderOMANative, func(context.Context, agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return nil, nil
	}); !errors.Is(err, agentruntime.ErrUnsupportedProvider) {
		t.Fatalf("register oma_native err = %v, want ErrUnsupportedProvider", err)
	}
	if err := reg.Register(agentruntime.ProviderQoderCloud, nil); !errors.Is(err, agentruntime.ErrUnsupportedProvider) {
		t.Fatalf("register nil factory err = %v, want ErrUnsupportedProvider", err)
	}
	if !reg.IsValidProviderKind(agentruntime.ProviderOMANative) || !reg.IsValidProviderKind(agentruntime.ProviderQoderCloud) {
		t.Fatal("expected oma_native and qoder_cloud to be valid provider kinds")
	}
	if reg.IsValidProviderKind(agentruntime.ProviderClaudeManaged) || reg.IsValidProviderKind("") {
		t.Fatal("unregistered or empty provider should not be valid")
	}

	if err := reg.Register(agentruntime.ProviderQoderCloud, func(_ context.Context, cfg agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return &stubRuntime{kind: agentruntime.ProviderQoderCloud, caps: cfg.Capabilities, creds: slices.Clone(cfg.Credentials)}, nil
	}); err != nil {
		t.Fatalf("Register(qoder_cloud): %v", err)
	}
	if err := reg.Register(agentruntime.ProviderQoderCloud, func(context.Context, agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return nil, nil
	}); !errors.Is(err, agentruntime.ErrUnsupportedProvider) {
		t.Fatalf("duplicate register err = %v, want ErrUnsupportedProvider", err)
	}
	if !reg.Has(agentruntime.ProviderQoderCloud) {
		t.Fatal("Has(qoder_cloud) = false")
	}
	if err := reg.Register(agentruntime.ProviderOpenAIAgents, func(_ context.Context, cfg agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return &stubRuntime{kind: agentruntime.ProviderOpenAIAgents, caps: cfg.Capabilities}, nil
	}); err != nil {
		t.Fatalf("Register(openai_agents): %v", err)
	}
	supported := reg.SupportedProviders()
	if len(supported) != 3 || !slices.Contains(supported, agentruntime.ProviderOMANative) || !slices.Contains(supported, agentruntime.ProviderQoderCloud) {
		t.Fatalf("SupportedProviders() = %v", supported)
	}

	if _, err := reg.Build(context.Background(), agentruntime.ProfileRuntimeConfig{
		Profile: db.RuntimeProfile{ProviderKind: string(agentruntime.ProviderClaudeManaged)},
	}); !errors.Is(err, agentruntime.ErrProviderNotRegistered) {
		t.Fatalf("Build(unregistered) err = %v, want ErrProviderNotRegistered", err)
	}
}

func TestResolverAndHealthCheck(t *testing.T) {
	ctx := context.Background()
	secretSvc := newTestSecrets(t)
	profileUUID := "22222222-2222-4222-8222-222222222222"
	envelope, err := secretSvc.SealRuntimeProfile(ctx, secrets.RuntimeProfileBinding{
		Scope:              "platform",
		RuntimeProfileUUID: profileUUID,
		CredentialRevision: 2,
	}, []byte(`{"api_key":"qdr-secret"}`))
	if err != nil {
		t.Fatalf("SealRuntimeProfile: %v", err)
	}

	var observedCreds string
	var built *stubRuntime
	reg := agentruntime.NewRegistry()
	if err := reg.Register(agentruntime.ProviderQoderCloud, func(_ context.Context, cfg agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		observedCreds = string(cfg.Credentials)
		built = &stubRuntime{
			kind:      agentruntime.ProviderQoderCloud,
			caps:      cfg.Capabilities,
			healthRes: agentruntime.HealthCheckResult{Status: "healthy", Message: "ok"},
		}
		return built, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	store := &fakeProfileStore{
		profiles: map[string]db.RuntimeProfile{
			db.DefaultOMANativeRuntimeProfileUUID: {
				UUID:         db.DefaultOMANativeRuntimeProfileUUID,
				ExternalID:   "rprof_oma_native_default",
				Scope:        "platform",
				ProviderKind: string(agentruntime.ProviderOMANative),
				Status:       "active",
			},
			profileUUID: {
				UUID:                profileUUID,
				ExternalID:          "rprof_qoder_shared",
				Scope:               "platform",
				ProviderKind:        string(agentruntime.ProviderQoderCloud),
				Status:              "active",
				SecretEnvelope:      &envelope,
				CredentialRevision:  2,
				CapabilityOverrides: json.RawMessage(`{"deployments":true}`),
			},
		},
		workspaceMap: map[string]string{
			"org-1/ws-1": profileUUID,
		},
	}

	resolver := agentruntime.NewResolver(store, reg, secretSvc, nil)

	caps, prof, err := resolver.WorkspaceCapabilities(ctx, "org-1", "ws-1")
	if err != nil || prof.UUID != profileUUID || !caps.Deployments || caps.MCPTunnels {
		t.Fatalf("WorkspaceCapabilities = (%+v, %+v, %v)", caps, prof, err)
	}

	rt, _, err := resolver.ResolveWorkspace(ctx, "org-1", "ws-1")
	if err != nil || rt.ProviderKind() != agentruntime.ProviderQoderCloud || observedCreds != `{"api_key":"qdr-secret"}` {
		t.Fatalf("ResolveWorkspace = (%v, creds=%q, err=%v)", rt, observedCreds, err)
	}
	if _, _, err := resolver.ResolveProfile(ctx, profileUUID); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}

	res, err := resolver.HealthCheckProfile(ctx, profileUUID)
	if err != nil || res.Status != "healthy" || !store.healthChecked || store.healthStatus != "healthy" || !built.closed {
		t.Fatalf("HealthCheckProfile(qoder) = (%+v, %v), closed=%v", res, err, built.closed)
	}
	nativeRes, err := resolver.HealthCheckProfile(ctx, db.DefaultOMANativeRuntimeProfileUUID)
	if err != nil || nativeRes.Status != "healthy" {
		t.Fatalf("HealthCheckProfile(native) = (%+v, %v)", nativeRes, err)
	}

	disabled := store.profiles[profileUUID]
	disabled.Status = "disabled"
	store.profiles[profileUUID] = disabled
	if _, _, err := resolver.ResolveProfile(ctx, profileUUID); !errors.Is(err, agentruntime.ErrProfileInactive) {
		t.Fatalf("ResolveProfile(disabled) err = %v, want ErrProfileInactive", err)
	}
}

func TestBindingServiceIsolatesTenantsAndRejectsNative(t *testing.T) {
	ctx := context.Background()
	store := newFakeBindingStore()
	svc := agentruntime.NewBindingService(store)

	nativeScope := agentruntime.Scope{
		OrganizationUUID:   "org-1",
		WorkspaceUUID:      "ws-1",
		RuntimeProfileUUID: db.DefaultOMANativeRuntimeProfileUUID,
		ProviderKind:       agentruntime.ProviderOMANative,
	}
	if _, err := svc.Bind(ctx, nativeScope, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeAgent,
		OMAExternalID: "agent_1",
		RemoteID:      "agt_1",
	}, nil); !errors.Is(err, agentruntime.ErrExternalProviderOnly) {
		t.Fatalf("Bind(native) err = %v, want ErrExternalProviderOnly", err)
	}

	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: "prof-shared",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: "prof-shared",
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	bound, err := svc.Bind(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeAgent,
		OMAExternalID: "agent_qdr_1",
		RemoteID:      "agt_remote_1",
	}, json.RawMessage(`{"name":"Alpha"}`))
	if err != nil || bound.RemoteID != "agt_remote_1" {
		t.Fatalf("Bind: (%+v, %v)", bound, err)
	}

	if _, err := svc.ResolveOwned(ctx, scopeA, agentruntime.ResourceTypeAgent, "agent_qdr_1"); err != nil {
		t.Fatalf("ResolveOwned(owner): %v", err)
	}
	if _, err := svc.ResolveOwned(ctx, scopeB, agentruntime.ResourceTypeAgent, "agent_qdr_1"); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("ResolveOwned(cross tenant) err = %v, want ErrResourceNotFound", err)
	}
	if _, err := svc.LookupByRemoteID(ctx, scopeB, agentruntime.ResourceTypeAgent, "agt_remote_1"); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("LookupByRemoteID(cross tenant) err = %v, want ErrResourceNotFound", err)
	}
	if got, err := svc.LookupByRemoteID(ctx, scopeA, agentruntime.ResourceTypeAgent, "agt_remote_1"); err != nil || got.OMAExternalID != "agent_qdr_1" {
		t.Fatalf("LookupByRemoteID(owner) = (%+v, %v)", got, err)
	}

	claimed, inserted, err := svc.ClaimOutput(ctx, scopeA, agentruntime.ResourceRef{
		Type:                agentruntime.ResourceTypeFile,
		OMAExternalID:       "file_qdr_out_1",
		RemoteID:            "file_remote_1",
		ParentOMAExternalID: "ses_1",
	}, json.RawMessage(`{"name":"report.md"}`))
	if err != nil || !inserted || claimed.ResourceRole != "session_output" {
		t.Fatalf("ClaimOutput(first) = (%+v, %v, %v)", claimed, inserted, err)
	}
	if _, inserted2, err := svc.ClaimOutput(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeFile,
		OMAExternalID: "file_qdr_out_1",
		RemoteID:      "file_remote_1",
	}, nil); err != nil || inserted2 {
		t.Fatalf("ClaimOutput(duplicate) = (%v, %v)", inserted2, err)
	}

	if list, err := svc.ListOwned(ctx, scopeA, agentruntime.ResourceTypeAgent, "", "", false, 20, 0); err != nil || len(list) != 1 {
		t.Fatalf("ListOwned = (%v, %v)", list, err)
	}
	if searched, err := svc.SearchOwned(ctx, scopeA, agentruntime.ResourceTypeAgent, "Alpha", false, 20, 0); err != nil || len(searched) != 1 {
		t.Fatalf("SearchOwned = (%v, %v)", searched, err)
	}
	if updated, err := svc.UpdateSummary(ctx, scopeA, agentruntime.ResourceTypeAgent, "agent_qdr_1", "archived", json.RawMessage(`{"name":"Alpha V2"}`)); err != nil || updated.State != "archived" {
		t.Fatalf("UpdateSummary = (%+v, %v)", updated, err)
	}
	if err := svc.SoftDelete(ctx, scopeA, agentruntime.ResourceTypeAgent, "agent_qdr_1"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := svc.ResolveOwned(ctx, scopeA, agentruntime.ResourceTypeAgent, "agent_qdr_1"); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("ResolveOwned(after delete) err = %v, want ErrResourceNotFound", err)
	}

	var _ agentruntime.ModelRuntime
	var _ agentruntime.AgentRuntime
	var _ agentruntime.EnvironmentRuntime
	var _ agentruntime.FileRuntime
	var _ agentruntime.SessionRuntime
	var _ agentruntime.EventStream
	var _ agentruntime.FileContentStream
	var _ io.ReadCloser = (agentruntime.FileContentStream)(nil)
	var _ agentruntime.Page[agentruntime.Agent]
	var _ agentruntime.CreateAgentInput
	var _ agentruntime.UpdateAgentInput
	var _ agentruntime.ListAgentsQuery
	var _ agentruntime.SearchAgentsQuery
	var _ agentruntime.Environment
	var _ agentruntime.CreateEnvironmentInput
	var _ agentruntime.UpdateEnvironmentInput
	var _ agentruntime.ListEnvironmentsQuery
	var _ agentruntime.File
	var _ agentruntime.UploadFileInput
	var _ agentruntime.ListFilesQuery
	var _ agentruntime.Session
	var _ agentruntime.SessionBudget
	var _ agentruntime.SessionUsage
	var _ agentruntime.CreateSessionInput
	var _ agentruntime.UpdateSessionInput
	var _ agentruntime.ListSessionsQuery
	var _ agentruntime.Event
	var _ agentruntime.SendEventsInput
	var _ agentruntime.ListEventsQuery
	var _ agentruntime.StreamOptions
	var _ agentruntime.StreamFrame
	var _ agentruntime.SessionResource
	var _ agentruntime.AddSessionResourceInput
	var _ agentruntime.UpdateSessionResourceInput
	var _ agentruntime.Thread
	var _ agentruntime.Model
	_ = agentruntime.ResourceTypeEnvironment
	_ = agentruntime.ResourceTypeSession
	_ = agentruntime.ResourceTypeSkill
	_ = agentruntime.ResourceTypeVault
	_ = agentruntime.ResourceTypeMemoryStore
}

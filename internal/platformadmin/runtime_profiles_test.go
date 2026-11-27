package platformadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type fakeRuntimeProfileStore struct {
	profiles map[string]db.RuntimeProfile
	policies map[string]db.OrganizationRuntimePolicy
}

func newFakeRuntimeProfileStore() *fakeRuntimeProfileStore {
	now := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	return &fakeRuntimeProfileStore{
		profiles: map[string]db.RuntimeProfile{
			db.DefaultOMANativeRuntimeProfileUUID: {
				UUID:                db.DefaultOMANativeRuntimeProfileUUID,
				ExternalID:          "rprof_oma_native_default",
				Scope:               "platform",
				Name:                "OMA Native",
				ProviderKind:        "oma_native",
				IsolationMode:       "isolated",
				Config:              json.RawMessage(`{}`),
				CapabilityOverrides: json.RawMessage(`{}`),
				CredentialRevision:  1,
				Status:              "active",
				LastHealthStatus:    "healthy",
				Version:             1,
				CreatedAt:           now,
				UpdatedAt:           now,
			},
		},
		policies: map[string]db.OrganizationRuntimePolicy{
			"11111111-1111-4111-8111-111111111111": {
				OrganizationUUID:           "11111111-1111-4111-8111-111111111111",
				DefaultRuntimeProfileUUID:  db.DefaultOMANativeRuntimeProfileUUID,
				AllowedRuntimeProfileUUIDs: []string{db.DefaultOMANativeRuntimeProfileUUID},
			},
		},
	}
}

func (s *fakeRuntimeProfileStore) ListPlatformRuntimeProfiles(_ context.Context) ([]db.RuntimeProfile, error) {
	out := make([]db.RuntimeProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		if p.Scope == "platform" {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *fakeRuntimeProfileStore) GetRuntimeProfileByExternalID(_ context.Context, externalID string) (db.RuntimeProfile, error) {
	for _, p := range s.profiles {
		if p.ExternalID == externalID {
			return p, nil
		}
	}
	return db.RuntimeProfile{}, db.ErrNotFound
}

func (s *fakeRuntimeProfileStore) GetRuntimeProfileByUUID(_ context.Context, profileUUID string) (db.RuntimeProfile, error) {
	p, ok := s.profiles[profileUUID]
	if !ok {
		return db.RuntimeProfile{}, db.ErrNotFound
	}
	return p, nil
}

func (s *fakeRuntimeProfileStore) CreateRuntimeProfile(_ context.Context, p db.RuntimeProfile) (db.RuntimeProfile, error) {
	for _, existing := range s.profiles {
		if existing.Scope == p.Scope && existing.Name == p.Name {
			return db.RuntimeProfile{}, db.ErrDuplicate
		}
	}
	p.Version = 1
	s.profiles[p.UUID] = p
	return p, nil
}

func (s *fakeRuntimeProfileStore) UpdateRuntimeProfile(_ context.Context, p db.RuntimeProfile) (db.RuntimeProfile, error) {
	existing, ok := s.profiles[p.UUID]
	if !ok || existing.Version != p.Version {
		return db.RuntimeProfile{}, db.ErrVersionConflict
	}
	for _, other := range s.profiles {
		if other.UUID != p.UUID && other.Scope == p.Scope && other.Name == p.Name {
			return db.RuntimeProfile{}, db.ErrDuplicate
		}
	}
	p.Version = existing.Version + 1
	s.profiles[p.UUID] = p
	return p, nil
}

func (s *fakeRuntimeProfileStore) DeleteRuntimeProfile(_ context.Context, profileUUID string, expectedVersion int64) error {
	if profileUUID == db.DefaultOMANativeRuntimeProfileUUID {
		return db.ErrBuiltinRuntimeProfileProtected
	}
	existing, ok := s.profiles[profileUUID]
	if !ok || existing.Version != expectedVersion {
		return db.ErrVersionConflict
	}
	delete(s.profiles, profileUUID)
	return nil
}

func (s *fakeRuntimeProfileStore) GetOrganizationRuntimePolicy(_ context.Context, organizationUUID string) (db.OrganizationRuntimePolicy, error) {
	policy, ok := s.policies[organizationUUID]
	if !ok {
		return db.OrganizationRuntimePolicy{}, db.ErrNotFound
	}
	return policy, nil
}

func (s *fakeRuntimeProfileStore) UpdateOrganizationRuntimePolicy(_ context.Context, organizationUUID, defaultProfileUUID string, allowed []string) (db.OrganizationRuntimePolicy, error) {
	if _, ok := s.policies[organizationUUID]; !ok {
		return db.OrganizationRuntimePolicy{}, db.ErrNotFound
	}
	next := db.OrganizationRuntimePolicy{
		OrganizationUUID:           organizationUUID,
		DefaultRuntimeProfileUUID:  defaultProfileUUID,
		AllowedRuntimeProfileUUIDs: slices.Clone(allowed),
	}
	s.policies[organizationUUID] = next
	return next, nil
}

type fakeRuntimeHealthChecker struct {
	result agentruntime.HealthCheckResult
	err    error
}

func (f *fakeRuntimeHealthChecker) HealthCheckProfile(_ context.Context, _ string) (agentruntime.HealthCheckResult, error) {
	return f.result, f.err
}

func newRuntimeProfileTestSecrets(t *testing.T) *secrets.Service {
	t.Helper()
	kek, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK: %v", err)
	}
	provider, err := localkeys.New(localkeys.KeyMaterial{KEK: kek}, nil)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	return secrets.NewService(provider)
}

func TestRuntimeProfileServiceCRUDAndCredentials(t *testing.T) {
	ctx := context.Background()
	store := newFakeRuntimeProfileStore()
	secretSvc := newRuntimeProfileTestSecrets(t)
	checker := &fakeRuntimeHealthChecker{
		result: agentruntime.HealthCheckResult{
			Status:    "healthy",
			Latency:   42 * time.Millisecond,
			Message:   "connected",
			CheckedAt: time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC),
		},
	}
	svc := NewRuntimeProfileService(store, agentruntime.NewRegistry(), secretSvc, checker, nil).WithEnvironment(config.EnvironmentProd)
	actor := Actor{AccountUUID: "admin-1"}

	if _, err := svc.Create(ctx, actor, RuntimeProfileCreateInput{
		Name:         "Insecure",
		ProviderKind: "qoder_cloud",
		EndpointURL:  "http://qoder.example.test",
	}); err == nil {
		t.Fatal("prod environment accepted http endpoint_url")
	}

	created, err := svc.Create(ctx, actor, RuntimeProfileCreateInput{
		Name:                "Qoder Shared CN",
		ProviderKind:        "qoder_cloud",
		EndpointURL:         "https://qoder.example.test/api/v1/cloud",
		Config:              json.RawMessage(`{"region":"cn"}`),
		CapabilityOverrides: json.RawMessage(`{"deployments":false}`),
		Credentials:         json.RawMessage(`{"api_key":"qdr_live_secret_987654"}`),
	})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if !created.HasCredentials || created.CredentialHint != "qdr_...7654" || created.CredentialRevision != 1 || created.Version != 1 {
		t.Fatalf("created view = %+v", created)
	}
	if strings.Contains(created.CredentialHint, "secret") {
		t.Fatalf("credential_hint leaked secret: %q", created.CredentialHint)
	}

	stored := store.profiles[created.UUID]
	if stored.SecretEnvelope == nil {
		t.Fatal("expected secret_envelope to be persisted")
	}
	opened, err := secretSvc.OpenRuntimeProfile(ctx, secrets.RuntimeProfileBinding{
		Scope:              "platform",
		RuntimeProfileUUID: created.UUID,
		CredentialRevision: 1,
	}, *stored.SecretEnvelope)
	if err != nil || !strings.Contains(string(opened), "qdr_live_secret_987654") {
		t.Fatalf("OpenRuntimeProfile() = (%q, %v)", string(opened), err)
	}

	newName := "Qoder Shared CN v2"
	newCreds := json.RawMessage(`{"api_key":"qdr_live_rotated_4321"}`)
	updated, err := svc.Update(ctx, actor, created.ID, RuntimeProfileUpdateInput{
		ExpectedVersion: 1,
		Name:            &newName,
		Credentials:     &newCreds,
	})
	if err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if updated.Version != 2 || updated.CredentialRevision != 2 || updated.CredentialHint != "qdr_...4321" {
		t.Fatalf("updated view = %+v", updated)
	}

	if _, err := svc.Update(ctx, actor, created.ID, RuntimeProfileUpdateInput{
		ExpectedVersion: 1,
		Name:            &newName,
	}); !errors.Is(err, ErrRuntimeProfileVersion) {
		t.Fatalf("stale Update() err = %v, want ErrRuntimeProfileVersion", err)
	}

	testRes, err := svc.TestConnection(ctx, actor, created.ID)
	if err != nil || testRes.Status != "healthy" || testRes.LatencyMs != 42 {
		t.Fatalf("TestConnection() = (%+v, %v)", testRes, err)
	}

	if err := svc.Delete(ctx, actor, "rprof_oma_native_default", 1); !errors.Is(err, ErrRuntimeProfileProtected) {
		t.Fatalf("Delete(builtin) err = %v, want ErrRuntimeProfileProtected", err)
	}
	disabledStatus := "disabled"
	if _, err := svc.Update(ctx, actor, "rprof_oma_native_default", RuntimeProfileUpdateInput{
		ExpectedVersion: 1,
		Status:          &disabledStatus,
	}); !errors.Is(err, ErrRuntimeProfileProtected) {
		t.Fatalf("Update(builtin disabled) err = %v, want ErrRuntimeProfileProtected", err)
	}

	if err := svc.Delete(ctx, actor, created.ID, 2); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	listed, err := svc.List(ctx)
	if err != nil || len(listed) != 1 || !listed[0].Builtin {
		t.Fatalf("List() = (%+v, %v)", listed, err)
	}
}

func TestOrganizationRuntimePolicyValidation(t *testing.T) {
	ctx := context.Background()
	store := newFakeRuntimeProfileStore()
	secretSvc := newRuntimeProfileTestSecrets(t)
	svc := NewRuntimeProfileService(store, agentruntime.NewRegistry(), secretSvc, nil, nil)
	actor := Actor{AccountUUID: "admin-1"}
	orgUUID := "11111111-1111-4111-8111-111111111111"

	qoder, err := svc.Create(ctx, actor, RuntimeProfileCreateInput{
		Name:         "Qoder Pool",
		ProviderKind: "qoder_cloud",
		EndpointURL:  "https://qoder.example.test",
	})
	if err != nil {
		t.Fatalf("Create(qoder): %v", err)
	}

	if _, err := svc.UpdateOrganizationPolicy(ctx, actor, orgUUID, UpdateOrganizationRuntimePolicyInput{
		DefaultRuntimeProfileUUID:  qoder.UUID,
		AllowedRuntimeProfileUUIDs: []string{qoder.UUID},
	}); err == nil {
		t.Fatal("policy update without oma_native accepted")
	}

	if _, err := svc.UpdateOrganizationPolicy(ctx, actor, orgUUID, UpdateOrganizationRuntimePolicyInput{
		DefaultRuntimeProfileUUID:  qoder.UUID,
		AllowedRuntimeProfileUUIDs: []string{db.DefaultOMANativeRuntimeProfileUUID},
	}); err == nil {
		t.Fatal("policy update with default outside allowed accepted")
	}

	updated, err := svc.UpdateOrganizationPolicy(ctx, actor, orgUUID, UpdateOrganizationRuntimePolicyInput{
		DefaultRuntimeProfileUUID:  qoder.UUID,
		AllowedRuntimeProfileUUIDs: []string{db.DefaultOMANativeRuntimeProfileUUID, qoder.UUID, qoder.UUID},
	})
	if err != nil {
		t.Fatalf("UpdateOrganizationPolicy(): %v", err)
	}
	if updated.DefaultRuntimeProfileUUID != qoder.UUID || len(updated.AllowedRuntimeProfileUUIDs) != 2 || len(updated.Profiles) != 2 {
		t.Fatalf("updated policy = %+v", updated)
	}

	fetched, err := svc.GetOrganizationPolicy(ctx, orgUUID)
	if err != nil || fetched.DefaultRuntimeProfileUUID != qoder.UUID {
		t.Fatalf("GetOrganizationPolicy() = (%+v, %v)", fetched, err)
	}
}

func TestHandlerRuntimeProfileRoutes(t *testing.T) {
	store := newFakeRuntimeProfileStore()
	secretSvc := newRuntimeProfileTestSecrets(t)
	checker := &fakeRuntimeHealthChecker{
		result: agentruntime.HealthCheckResult{Status: "healthy", Message: "ok", CheckedAt: time.Now().UTC()},
	}
	rpSvc := NewRuntimeProfileService(store, agentruntime.NewRegistry(), secretSvc, checker, nil)
	session := platformsession.Session{UserUUID: "admin-1", VerifiedEmail: "admin@example.com", AuthenticationMethod: "email_code_v1"}
	h := NewHandler(New(nil, nil), func(*http.Request) (platformsession.Session, error) { return session, nil }, true).
		WithRuntimeProfiles(rpSvc)
	h.verify = func(_ context.Context, account string) (db.PlatformAdminAccount, error) {
		return db.PlatformAdminAccount{UUID: account, Granted: true}, nil
	}
	routes := h.Routes()

	doReq := func(method, target string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "sessionKey", Value: "sess-1"})
		if method != http.MethodGet {
			req.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken("sess-1"))
		}
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		return rec
	}

	createResp := doReq(http.MethodPost, "/runtime-profiles", `{"name":"Qoder HTTP","provider_kind":"qoder_cloud","endpoint_url":"https://qoder.example.test","credentials":{"api_key":"sk-abcdef123456"}}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /runtime-profiles status = %d: %s", createResp.Code, createResp.Body.String())
	}
	var created RuntimeProfileView
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created view: %v", err)
	}

	listResp := doReq(http.MethodGet, "/runtime-profiles", "")
	if listResp.Code != http.StatusOK {
		t.Fatalf("GET /runtime-profiles status = %d: %s", listResp.Code, listResp.Body.String())
	}

	testResp := doReq(http.MethodPost, "/runtime-profiles/"+created.ID+"/test", `{}`)
	if testResp.Code != http.StatusOK {
		t.Fatalf("POST /runtime-profiles/{id}/test status = %d: %s", testResp.Code, testResp.Body.String())
	}

	patchResp := doReq(http.MethodPatch, "/runtime-profiles/"+created.ID, `{"expected_version":1,"name":"Qoder HTTP Renamed"}`)
	if patchResp.Code != http.StatusOK {
		t.Fatalf("PATCH /runtime-profiles/{id} status = %d: %s", patchResp.Code, patchResp.Body.String())
	}

	delResp := doReq(http.MethodDelete, "/runtime-profiles/"+created.ID, `{"expected_version":2}`)
	if delResp.Code != http.StatusNoContent {
		t.Fatalf("DELETE /runtime-profiles/{id} status = %d: %s", delResp.Code, delResp.Body.String())
	}
}

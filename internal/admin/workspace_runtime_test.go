package admin

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type stubAdminRuntimePolicyStore struct {
	policy   db.OrganizationRuntimePolicy
	profiles []db.RuntimeProfile
}

func (s *stubAdminRuntimePolicyStore) GetOrganizationRuntimePolicy(context.Context, string) (db.OrganizationRuntimePolicy, error) {
	return s.policy, nil
}

func (s *stubAdminRuntimePolicyStore) GetRuntimeProfileByUUID(_ context.Context, profileUUID string) (db.RuntimeProfile, error) {
	for _, p := range s.profiles {
		if p.UUID == profileUUID {
			return p, nil
		}
	}
	return db.RuntimeProfile{}, db.ErrNotFound
}

func (s *stubAdminRuntimePolicyStore) GetRuntimeProfileByExternalID(_ context.Context, externalID string) (db.RuntimeProfile, error) {
	for _, p := range s.profiles {
		if p.ExternalID == externalID {
			return p, nil
		}
	}
	return db.RuntimeProfile{}, db.ErrNotFound
}

func TestResolveAdminWorkspaceRuntimeProfileUUID(t *testing.T) {
	const (
		orgUUID      = "00000000-0000-4000-8000-000000000010"
		qoderUUID    = "00000000-0000-4000-8000-000000000020"
		disabledUUID = "00000000-0000-4000-8000-000000000021"
		deniedUUID   = "00000000-0000-4000-8000-000000000022"
	)

	store := &stubAdminRuntimePolicyStore{
		policy: db.OrganizationRuntimePolicy{
			OrganizationUUID:           orgUUID,
			DefaultRuntimeProfileUUID:  db.DefaultOMANativeRuntimeProfileUUID,
			AllowedRuntimeProfileUUIDs: []string{db.DefaultOMANativeRuntimeProfileUUID, qoderUUID, disabledUUID},
		},
		profiles: []db.RuntimeProfile{
			{UUID: db.DefaultOMANativeRuntimeProfileUUID, ExternalID: "rprof_oma_native_default", Status: "active"},
			{UUID: qoderUUID, ExternalID: "rprof_qoder_cn", Status: "active"},
			{UUID: disabledUUID, ExternalID: "rprof_disabled", Status: "disabled"},
			{UUID: deniedUUID, ExternalID: "rprof_denied", Status: "active"},
		},
	}

	got, err := resolveAdminWorkspaceRuntimeProfileUUID(context.Background(), store, orgUUID, "")
	if err != nil || got != db.DefaultOMANativeRuntimeProfileUUID {
		t.Fatalf("default got=%q err=%v", got, err)
	}

	got, err = resolveAdminWorkspaceRuntimeProfileUUID(context.Background(), store, orgUUID, "rprof_qoder_cn")
	if err != nil || got != qoderUUID {
		t.Fatalf("external_id got=%q err=%v", got, err)
	}

	got, err = resolveAdminWorkspaceRuntimeProfileUUID(context.Background(), store, orgUUID, qoderUUID)
	if err != nil || got != qoderUUID {
		t.Fatalf("uuid got=%q err=%v", got, err)
	}

	if _, err := resolveAdminWorkspaceRuntimeProfileUUID(context.Background(), store, orgUUID, "rprof_denied"); err == nil {
		t.Fatalf("expected disallowed profile to fail")
	}
	if _, err := resolveAdminWorkspaceRuntimeProfileUUID(context.Background(), store, orgUUID, "rprof_disabled"); err == nil {
		t.Fatalf("expected disabled profile to fail")
	}
	if _, err := resolveAdminWorkspaceRuntimeProfileUUID(context.Background(), store, orgUUID, "rprof_missing"); err == nil {
		t.Fatalf("expected missing profile to fail")
	}
}

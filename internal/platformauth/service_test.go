package platformauth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestServiceFindOrCreateUserContextByEmail(t *testing.T) {
	t.Run("failure nil store", func(t *testing.T) {
		_, _, err := newTestService(nil).findOrCreateUserContextByEmail(t.Context(), "user@example.com")
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("failure find error", func(t *testing.T) {
		wantErr := errors.New("query failed")
		store := &fakePlatformAuthStore{tx: &fakePlatformAuthTx{findErr: wantErr}}
		_, _, err := newTestService(store).findOrCreateUserContextByEmail(t.Context(), "user@example.com")
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	})

	t.Run("success existing user updates empty name", func(t *testing.T) {
		tx := &fakePlatformAuthTx{
			findContext: db.PlatformAuthUserContext{UserExternalID: "user_existing", OrgUUID: "org-existing"},
		}
		userID, orgUUID, err := newTestService(&fakePlatformAuthStore{tx: tx}).findOrCreateUserContextByEmail(t.Context(), "ada.lovelace@example.com")
		if err != nil {
			t.Fatalf("FindOrCreateUserContextByEmail() error = %v", err)
		}
		if userID != "user_existing" || orgUUID != "org-existing" {
			t.Fatalf("context = (%q, %q), want existing context", userID, orgUUID)
		}
		if len(tx.nameUpdates) != 1 || tx.nameUpdates[0].name != "ada lovelace" {
			t.Fatalf("name updates = %#v, want ada lovelace update", tx.nameUpdates)
		}
	})

	t.Run("success missing user creates default context", func(t *testing.T) {
		tx := &fakePlatformAuthTx{findErr: db.ErrNotFound}
		userID, orgUUID, err := newTestService(&fakePlatformAuthStore{tx: tx}).findOrCreateUserContextByEmail(t.Context(), "new-user@example.com")
		if err != nil {
			t.Fatalf("FindOrCreateUserContextByEmail() error = %v", err)
		}
		if !strings.HasPrefix(userID, "user_") || orgUUID != "created-org-uuid" {
			t.Fatalf("context = (%q, %q), want created context", userID, orgUUID)
		}
		if len(tx.organizations) != 1 || tx.organizations[0].Name != "new user" {
			t.Fatalf("organizations = %#v, want default organization", tx.organizations)
		}
		if len(tx.workspaces) != 1 || tx.workspaces[0].Name != "default" || !strings.HasPrefix(tx.workspaces[0].ExternalID, "wrkspc_") {
			t.Fatalf("workspaces = %#v, want default workspace", tx.workspaces)
		}
		if len(tx.members) != 1 || tx.members[0].WorkspaceRole != "workspace_admin" || !strings.HasPrefix(tx.members[0].ExternalID, "wmem_") {
			t.Fatalf("members = %#v, want workspace admin member", tx.members)
		}
		if len(tx.apiKeys) != 1 || tx.apiKeys[0].Name != "default" || tx.apiKeys[0].Status != "active" || !strings.HasPrefix(tx.apiKeys[0].ExternalID, "api_key_") {
			t.Fatalf("api keys = %#v, want default active api key", tx.apiKeys)
		}
		if tx.apiKeys[0].KeyHash == "" || strings.Contains(tx.apiKeys[0].KeyHash, "sk-ant-api03") || tx.apiKeys[0].PartialKeyHint == "" {
			t.Fatalf("api key credential fields = %#v, want hashed key and partial hint only", tx.apiKeys[0])
		}
	})
}

func newTestService(store Store) *EmailProvider {
	return &EmailProvider{store: store}
}

type fakePlatformAuthStore struct {
	tx *fakePlatformAuthTx
}

func (s *fakePlatformAuthStore) WithPlatformAuthTx(ctx context.Context, fn func(db.PlatformAuthTxStore) error) error {
	return fn(s.tx)
}

type fakePlatformAuthTx struct {
	findContext   db.PlatformAuthUserContext
	findErr       error
	nameUpdates   []fakeNameUpdate
	organizations []db.PlatformAuthOrganizationInput
	users         []db.PlatformAuthUserInput
	workspaces    []db.PlatformAuthWorkspaceInput
	members       []db.PlatformAuthWorkspaceMemberInput
	apiKeys       []db.PlatformAuthAPIKeyInput
}

type fakeNameUpdate struct {
	userExternalID string
	name           string
}

func (tx *fakePlatformAuthTx) FindUserContextByEmail(context.Context, string) (db.PlatformAuthUserContext, error) {
	if tx.findErr != nil {
		return db.PlatformAuthUserContext{}, tx.findErr
	}
	return tx.findContext, nil
}

func (tx *fakePlatformAuthTx) UpdateEmptyUserName(_ context.Context, userExternalID string, defaultName string) error {
	tx.nameUpdates = append(tx.nameUpdates, fakeNameUpdate{userExternalID: userExternalID, name: defaultName})
	return nil
}

func (tx *fakePlatformAuthTx) InsertOrganization(_ context.Context, input db.PlatformAuthOrganizationInput) (db.PlatformAuthOrganizationRef, error) {
	tx.organizations = append(tx.organizations, input)
	return db.PlatformAuthOrganizationRef{UUID: "created-org-uuid"}, nil
}

func (tx *fakePlatformAuthTx) InsertUser(_ context.Context, input db.PlatformAuthUserInput) (db.PlatformAuthUserRef, error) {
	tx.users = append(tx.users, input)
	return db.PlatformAuthUserRef{UUID: input.UUID}, nil
}

func (tx *fakePlatformAuthTx) InsertWorkspace(_ context.Context, input db.PlatformAuthWorkspaceInput) (db.PlatformAuthWorkspaceRef, error) {
	tx.workspaces = append(tx.workspaces, input)
	return db.PlatformAuthWorkspaceRef{UUID: input.UUID}, nil
}

func (tx *fakePlatformAuthTx) InsertWorkspaceMember(_ context.Context, input db.PlatformAuthWorkspaceMemberInput) error {
	tx.members = append(tx.members, input)
	return nil
}

func (tx *fakePlatformAuthTx) InsertAPIKey(_ context.Context, input db.PlatformAuthAPIKeyInput) error {
	tx.apiKeys = append(tx.apiKeys, input)
	return nil
}

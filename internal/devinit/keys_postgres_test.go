package devinit

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"uuid"
)

func TestAPIKeyRecoveryPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/devinit_recovery_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.Replace(minimalBaseConfig, "postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable", parsed.String(), 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", configPath)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		database.Close()
		maintenanceURL := *parsed
		maintenanceURL.Path = "/postgres"
		pool, err := pgxpool.New(context.Background(), maintenanceURL.String())
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		if _, err := pool.Exec(context.Background(), `DROP DATABASE "`+strings.TrimPrefix(parsed.Path, "/")+`"`); err != nil {
			t.Errorf("remove isolated database: %v", err)
		}
	})
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	spec, state, credentials := pendingKeyFixture()
	layout := Layout{StateFile: filepath.Join(dir, "state.json"), CredentialsFile: filepath.Join(dir, "credentials.json")}
	if err := prepareResourceIdentities(layout, spec, &state, &credentials); err != nil {
		t.Fatal(err)
	}
	if err := reconcileIdentity(t.Context(), database, spec, &state, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(layout, state); err != nil {
		t.Fatal(err)
	}
	workspace := state.Workspaces[0]
	if _, err := database.CreateConsoleAPIKeyWithRawKey(t.Context(), platform.CreateConsoleAPIKeyInput{
		OrgUUID: state.Organization.UUID, WorkspaceUUID: workspace.UUID, WorkspaceDisplayID: workspace.ExternalID,
		Name: workspace.APIKeys[0].Name, CreatedByUserUUID: &state.User.UUID,
	}, workspace.APIKeys[0].ExternalID, credentials.APIKeys[0].Key); err != nil {
		t.Fatal(err)
	}
	state, _, err = LoadState(layout.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcileAPIKeys(t.Context(), database, spec, &state, &credentials, Options{}); err != nil {
		t.Fatalf("resume after first key committed: %v", err)
	}
	if err := reconcileAPIKeys(t.Context(), database, spec, &state, &credentials, Options{}); err != nil {
		t.Fatal(err)
	}
	t.Run("default environments recover committed creation", func(t *testing.T) {
		if err := SaveState(layout, state); err != nil {
			t.Fatal(err)
		}
		if err := reconcileEnvironments(t.Context(), database, "", &state); err != nil {
			t.Fatal(err)
		}
		state, _, err = LoadState(layout.StateFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := reconcileEnvironments(t.Context(), database, "", &state); err != nil {
			t.Fatalf("resume after creation committed: %v", err)
		}
		for _, workspace := range state.Workspaces {
			environments, _, err := database.ListEnvironmentsPage(t.Context(), db.ListEnvironmentsPageParams{WorkspaceUUID: workspace.UUID, Limit: 10})
			if err != nil || len(environments) != 1 {
				t.Fatalf("workspace %s environments: %+v, %v", workspace.Ref, environments, err)
			}
			environment := environments[0]
			var settings struct {
				Type       string `json:"type"`
				Networking struct {
					Type string `json:"type"`
				} `json:"networking"`
			}
			if err := json.Unmarshal(environment.Config, &settings); err != nil {
				t.Fatal(err)
			}
			if environment.Name != "default" || environment.UUID != workspace.Environment.UUID || environment.Provider != "e2b" || environment.ResolvedTemplate != config.DefaultE2BTemplate || settings.Type != "cloud" || settings.Networking.Type != "unrestricted" || workspace.Environment.Pending {
				t.Fatalf("invalid default environment: %+v, %+v", environment, settings)
			}
		}
		workspace := state.Workspaces[0]
		if _, _, err := database.ArchiveEnvironment(t.Context(), workspace.UUID, workspace.Environment.ExternalID); err != nil {
			t.Fatal(err)
		}
		if err := reconcileEnvironments(t.Context(), database, "", &state); err == nil {
			t.Fatal("archived environment was silently reused")
		}
	})
	keys, err := database.ListConsoleAPIKeys(t.Context(), state.Organization.UUID, nil)
	if err != nil || len(keys) != 4 {
		t.Fatalf("expected exactly four keys, got %d: %v", len(keys), err)
	}
}

func pendingKeyFixture() (Spec, State, Credentials) {
	spec := Spec{OrganizationName: "OMA", UserEmail: "test@oma.ai", UserName: "Test", UserRole: "admin"}
	credentials := Credentials{EnvID: "test", BrainClaimToken: "token"}
	for index, ref := range []string{"default", "secondary"} {
		spec.Workspaces = append(spec.Workspaces, WorkspaceSpec{
			Ref: ref, Name: ref, IsDefault: index == 0, APIKeys: []APIKeySpec{{Ref: "key-1", Name: ref + "-1"}, {Ref: "key-2", Name: ref + "-2"}},
		})
		for _, key := range spec.Workspaces[index].APIKeys {
			credentials.APIKeys = append(credentials.APIKeys, APICredential{WorkspaceRef: ref, Ref: key.Ref, Key: ref + key.Ref})
		}
	}
	return spec, State{EnvID: "test"}, credentials
}

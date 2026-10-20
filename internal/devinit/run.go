package devinit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"uuid"
)

const (
	secondaryMemberRole = "workspace_admin"
)

type Options struct {
	Update bool
}

type Result struct {
	Layout      Layout
	State       State
	Credentials Credentials
}

// Run binds the environment and initializes it. It is resumable: every phase
// rechecks actual resource state before writing, so retries never create
// duplicates.
func Run(ctx context.Context, repoRoot, initFile string, opts Options) (Result, error) {
	prepped, err := prepareRun(repoRoot, initFile)
	if err != nil {
		return Result{}, err
	}
	defer prepped.unlock()
	if err := copyFixedSecretsAndCredentials(prepped.layout, prepped.spec, &prepped.state, &prepped.credentials, prepped.credentialsFound); err != nil {
		return Result{}, err
	}
	stackPorts, err := EnsureStack(ctx, prepped.repoRoot, prepped.binding, prepped.state.Runtime)
	if err != nil {
		return Result{}, err
	}
	prepped.state.Runtime = &stackPorts
	prepped.state.Phase = phaseInfra
	if err := SaveState(prepped.layout, prepped.state); err != nil {
		return Result{}, err
	}
	if err := ensureBucket(ctx, prepped.base, stackPorts.MinIOPort); err != nil {
		return Result{}, err
	}
	databaseURL, err := replaceURLPort(prepped.base.Database.URL, stackPorts.PGPort)
	if err != nil {
		return Result{}, err
	}
	database, err := migrateDatabase(ctx, prepped.base, databaseURL)
	if err != nil {
		return Result{}, err
	}
	defer database.Close()
	prepped.state.Phase = phaseMigrated
	if err := SaveState(prepped.layout, prepped.state); err != nil {
		return Result{}, err
	}

	if err := prepareResourceIdentities(prepped.layout, prepped.spec, &prepped.state, &prepped.credentials); err != nil {
		return Result{}, err
	}
	if err := reconcileIdentity(ctx, database, prepped.spec, &prepped.state, opts); err != nil {
		return Result{}, err
	}
	prepped.state.Phase = phaseIdentity
	if err := SaveState(prepped.layout, prepped.state); err != nil {
		return Result{}, err
	}

	if err := reconcileAPIKeys(ctx, database, prepped.spec, &prepped.state, &prepped.credentials, opts); err != nil {
		return Result{}, err
	}
	prepped.state.Phase = phaseKeys
	if err := SaveState(prepped.layout, prepped.state); err != nil {
		return Result{}, err
	}

	vaultSecrets, err := secretservice.New(prepped.base.Vault.MasterKey)
	if err != nil {
		return Result{}, fmt.Errorf("load vault secrets service: %w", err)
	}
	if err := reconcileProviders(ctx, database, vaultSecrets, prepped.providers, &prepped.state, opts); err != nil {
		return Result{}, err
	}
	prepped.state.Phase = phaseProviders
	if err := SaveState(prepped.layout, prepped.state); err != nil {
		return Result{}, err
	}

	return finalizeRun(ctx, prepped)
}

type preparedRun struct {
	repoRoot         string
	initFile         string
	base             config.Config
	providers        ModelProvidersSpec
	spec             Spec
	binding          EnvBinding
	layout           Layout
	state            State
	credentials      Credentials
	credentialsFound bool
	unlock           func()
}

func prepareRun(repoRoot, initFile string) (preparedRun, error) {
	spec, err := LoadSpec(initFile)
	if err != nil {
		return preparedRun{}, err
	}
	providers, err := LoadModelProviders(spec.ModelProviders)
	if err != nil {
		return preparedRun{}, err
	}
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return preparedRun{}, err
	}
	initFile, err = filepath.Abs(initFile)
	if err != nil {
		return preparedRun{}, err
	}
	base, err := loadBaseConfig(repoRoot)
	if err != nil {
		return preparedRun{}, err
	}
	unlock, err := acquireInitLock(generatedDir(repoRoot))
	if err != nil {
		return preparedRun{}, err
	}
	prepared, err := loadRunState(repoRoot, initFile, base, spec, providers, unlock)
	if err != nil {
		unlock()
		return preparedRun{}, err
	}
	return prepared, nil
}

func loadRunState(repoRoot, initFile string, base config.Config, spec Spec, providers ModelProvidersSpec, unlock func()) (preparedRun, error) {
	registry, err := OpenRegistry(repoRoot)
	if err != nil {
		return preparedRun{}, err
	}
	binding, found, err := LoadBinding(envFilePath(repoRoot))
	if err != nil {
		return preparedRun{}, err
	}
	if found {
		binding.Worktree = repoRoot
		binding.InitFile = initFile
		if _, err := registry.Ensure(binding, initFile); err != nil {
			return preparedRun{}, err
		}
	} else {
		binding, _, err = registry.Create(repoRoot, initFile, worktreeSlug(repoRoot))
		if err != nil {
			return preparedRun{}, err
		}
	}
	layout, err := NewLayout(binding, repoRoot)
	if err != nil {
		return preparedRun{}, err
	}
	if err := SaveBinding(layout, binding); err != nil {
		return preparedRun{}, err
	}
	state, stateFound, err := LoadState(layout.StateFile)
	if err != nil {
		return preparedRun{}, err
	}
	if stateFound && state.EnvID != binding.EnvID {
		return preparedRun{}, fmt.Errorf("state file belongs to a different environment identity")
	}
	if !stateFound {
		state = State{EnvID: binding.EnvID, Name: binding.Name, Phase: phasePendingSecrets}
	}
	credentials, credentialsFound, err := LoadCredentials(layout.CredentialsFile)
	if err != nil {
		return preparedRun{}, err
	}
	if credentialsFound && credentials.EnvID != binding.EnvID {
		return preparedRun{}, fmt.Errorf("credentials file belongs to a different environment identity")
	}
	if !credentialsFound && state.Phase != phasePendingSecrets && state.Phase != phaseInfra && state.Phase != phaseMigrated {
		return preparedRun{}, fmt.Errorf("credentials file %s is missing after keys were created; plaintext keys cannot be recovered from the database hash, rotate them explicitly", layout.CredentialsFile)
	}
	return preparedRun{
		repoRoot: repoRoot, initFile: initFile, base: base,
		providers: providers, spec: spec, binding: binding, layout: layout, state: state,
		credentials: credentials, credentialsFound: credentialsFound, unlock: unlock,
	}, nil
}

func finalizeRun(ctx context.Context, prepared preparedRun) (Result, error) {
	runtime := RuntimePorts{}
	if prepared.state.Runtime != nil {
		runtime = *prepared.state.Runtime
	}
	if err := ensureListenerPorts(&runtime); err != nil {
		return Result{}, err
	}
	if err := GenerateConfig(baseConfigPath(prepared.repoRoot), prepared.layout, prepared.base, prepared.spec, prepared.state, runtime); err != nil {
		return Result{}, err
	}
	if err := SaveBinding(prepared.layout, prepared.binding); err != nil {
		return Result{}, err
	}
	prepared.state.Phase = phaseComplete
	prepared.state.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	prepared.state.Runtime = &runtime
	if err := SaveState(prepared.layout, prepared.state); err != nil {
		return Result{}, err
	}
	return Result{Layout: prepared.layout, State: prepared.state, Credentials: prepared.credentials}, nil
}

func acquireInitLock(stateDir string) (func(), error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(stateDir, "init.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire dev-init lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func copyFixedSecretsAndCredentials(layout Layout, spec Spec, state *State, credentials *Credentials, found bool) error {
	if err := CopyFixedSecrets(spec.FixedSecretsDir, layout.SecretsDir); err != nil {
		return err
	}
	if found {
		tokenPath := filepath.Join(layout.SecretsDir, "brain-claim-token")
		if _, err := os.Stat(tokenPath); errors.Is(err, os.ErrNotExist) {
			if _, err := WriteBrainClaimToken(layout.SecretsDir, credentials.BrainClaimToken); err != nil {
				return err
			}
		}
		return nil
	}
	token, err := NewRandomToken(brainClaimTokenBytes)
	if err != nil {
		return err
	}
	values := Credentials{EnvID: state.EnvID, BrainClaimToken: token}
	for _, workspace := range spec.Workspaces {
		for _, key := range workspace.APIKeys {
			rawKey, err := NewAPIKeyValue()
			if err != nil {
				return err
			}
			values.APIKeys = append(values.APIKeys, APICredential{
				WorkspaceRef: workspace.Ref, Ref: key.Ref, Name: key.Name, Key: rawKey,
			})
		}
	}
	if _, err := WriteBrainClaimToken(layout.SecretsDir, token); err != nil {
		return err
	}
	if err := SaveCredentials(layout, values); err != nil {
		return err
	}
	*credentials = values
	return nil
}

// ensureBucket creates the fixed-name bucket inside the per-environment
// MinIO; the stack owns the instance so no ownership marker is needed.
func ensureBucket(ctx context.Context, base config.Config, minioPort int) error {
	endpoint, err := replaceURLPort(base.Storage.S3.Endpoint, minioPort)
	if err != nil {
		return err
	}
	storageConfig := base.Storage
	storageConfig.S3.Endpoint = endpoint
	storageClient, err := storage.New(storageConfig)
	if err != nil {
		return err
	}
	objectStore, err := storageClient.ForBucket(base.Storage.S3.Bucket)
	if err != nil {
		return err
	}
	return objectStore.Ensure(ctx)
}

func migrateDatabase(ctx context.Context, base config.Config, databaseURL string) (*db.DB, error) {
	cfg := base
	cfg.Database.URL = databaseURL
	logger := slog.New(logging.NewConsoleHandler(io.Discard, slog.LevelInfo))
	database, err := db.Open(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		return nil, err
	}
	if err := riverjobs.Migrate(ctx, database, logger); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func reconcileIdentity(ctx context.Context, database *db.DB, spec Spec, state *State, opts Options) error {
	if state.IdentityPending {
		if err := database.CreateDevIdentity(ctx, devIdentityInput(spec, *state)); err != nil {
			return err
		}
	}
	organization, err := reconcileOrganization(ctx, database, spec, state)
	if err != nil {
		return err
	}
	user, err := reconcileUser(ctx, database, spec, organization.UUID, state)
	if err != nil {
		return err
	}
	workspaces, err := reconcileWorkspaces(ctx, database, spec, organization.UUID, *user, state.Workspaces, opts)
	if err != nil {
		return err
	}
	state.Organization, state.User, state.Workspaces = organization, user, workspaces
	state.IdentityPending = false
	return nil
}

func reconcileOrganization(ctx context.Context, database *db.DB, spec Spec, state *State) (*OrganizationState, error) {
	if state.Organization != nil {
		organizationUUID, err := uuid.Parse(state.Organization.UUID)
		if err != nil {
			return nil, fmt.Errorf("registered organization uuid %q is invalid: %w", state.Organization.UUID, err)
		}
		existing, err := database.GetAdminOrganization(ctx, organizationUUID)
		if errors.Is(err, platform.ErrNotFound) {
			return nil, fmt.Errorf("registered organization %s was deleted; refusing to recreate it silently", state.Organization.UUID)
		}
		if err != nil {
			return nil, err
		}
		if existing.Name != spec.OrganizationName {
			return nil, fmt.Errorf("registered organization name %q differs from init file %q; organization rename is not supported", existing.Name, spec.OrganizationName)
		}
		return state.Organization, nil
	}
	return nil, fmt.Errorf("organization identity has not been reserved")
}

func reconcileUser(ctx context.Context, database *db.DB, spec Spec, organizationUUID string, state *State) (*UserState, error) {
	if state.User != nil {
		existing, err := database.GetAdminUser(ctx, organizationUUID, state.User.ExternalID)
		if errors.Is(err, platform.ErrNotFound) {
			return nil, fmt.Errorf("registered user %s was deleted; refusing to recreate it silently", state.User.ExternalID)
		}
		if err != nil {
			return nil, err
		}
		if existing.Email != spec.UserEmail || existing.Name != spec.UserName {
			return nil, fmt.Errorf("registered user %q differs from init file email/name; user identity update is not supported", state.User.ExternalID)
		}
		return &UserState{UUID: existing.UUID, ExternalID: existing.ExternalID, Email: existing.Email, Name: existing.Name}, nil
	}
	return nil, fmt.Errorf("user identity has not been reserved")
}

func reconcileWorkspaces(ctx context.Context, database *db.DB, spec Spec, organizationUUID string, user UserState, previous []WorkspaceState, opts Options) ([]WorkspaceState, error) {
	byRef := map[string]WorkspaceState{}
	for _, workspace := range previous {
		byRef[workspace.Ref] = workspace
	}
	result := make([]WorkspaceState, 0, len(spec.Workspaces))
	for _, workspaceSpec := range spec.Workspaces {
		registered := byRef[workspaceSpec.Ref]
		externalID := registered.ExternalID
		if externalID == "" {
			return nil, fmt.Errorf("workspace %q identity has not been reserved", workspaceSpec.Ref)
		}
		existing, err := database.GetAdminWorkspace(ctx, organizationUUID, externalID)
		if errors.Is(err, platform.ErrNotFound) {
			return nil, fmt.Errorf("registered workspace %s was deleted; refusing to recreate it", externalID)
		}
		if err != nil {
			return nil, err
		}
		if existing.ArchivedAt != nil {
			return nil, fmt.Errorf("registered workspace %s is archived; refusing to reuse it", externalID)
		}
		if existing.Name != workspaceSpec.Name {
			if !opts.Update {
				return nil, fmt.Errorf("workspace %q name %q differs from init file %q; rerun with --update", workspaceSpec.Ref, existing.Name, workspaceSpec.Name)
			}
			existing, err = renameWorkspace(ctx, database, existing, workspaceSpec.Name)
			if err != nil {
				return nil, err
			}
		}
		if !workspaceSpec.IsDefault {
			if err := database.EnsureAdminWorkspaceMember(ctx, organizationUUID, existing.UUID, existing.ExternalID, user.UUID, user.ExternalID, secondaryMemberRole); err != nil {
				return nil, err
			}
		}
		result = append(result, WorkspaceState{
			Ref: workspaceSpec.Ref, UUID: existing.UUID, ExternalID: existing.ExternalID,
			Name: existing.Name, IsDefault: workspaceSpec.IsDefault, APIKeys: registered.APIKeys,
		})
	}
	return result, nil
}

func renameWorkspace(ctx context.Context, database *db.DB, existing db.AdminWorkspace, name string) (db.AdminWorkspace, error) {
	existing.Name = name
	updated, err := database.UpdateAdminWorkspace(ctx, existing.OrganizationUUID, existing.ExternalID, existing)
	if errors.Is(err, platform.ErrNotFound) {
		return db.AdminWorkspace{}, fmt.Errorf("workspace %s disappeared during update", existing.ExternalID)
	}
	return updated, err
}

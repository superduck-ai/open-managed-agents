//go:build integration

package environments

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestSandboxPreparationKeepsCreatingLease(t *testing.T) {
	database := preparationTestDatabase(t)
	work := preparationTestWork(t, database)
	provider := &preparationTestProvider{ttl: 300 * time.Millisecond, delay: 800 * time.Millisecond}
	cfg := config.Config{}
	cfg.E2B.SandboxTimeout = provider.ttl
	cfg.EnvironmentPackages.PIP.IndexURL = "https://packages.example/simple"
	runner := &Runner{db: database, provider: provider, cfg: cfg, logger: logging.LoggerOrDefault(nil)}
	env := db.Environment{UUID: work.EnvironmentUUID, ExternalID: work.EnvironmentExternalID, Config: []byte(`{}`)}
	ctx, run := startRunnerRun(t.Context(), work)
	defer run.end(ctx, nil)
	if _, err := runner.createEnvironmentSandbox(ctx, run, env, &work, nil); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.expired || provider.renewals < 2 {
		t.Fatalf("creating preparation lost provider lease: expired=%v renewals=%d", provider.expired, provider.renewals)
	}
}

type preparationTestProvider struct {
	e2bruntime.Provider
	mu         sync.Mutex
	ttl, delay time.Duration
	expires    time.Time
	expired    bool
	renewals   int
	kills      []string
}

func (p *preparationTestProvider) Resolve(db.Environment, *db.EnvironmentWork) (e2bruntime.Resolution, error) {
	return e2bruntime.Resolution{Timeout: p.ttl}, nil
}
func (p *preparationTestProvider) Create(context.Context, db.Environment, *db.EnvironmentWork, e2bruntime.Resolution) (e2bruntime.Sandbox, error) {
	p.expires = time.Now().Add(p.ttl)
	return e2bruntime.Sandbox{ID: "preparation-fixture"}, nil
}
func (p *preparationTestProvider) RunCommand(ctx context.Context, _ string, _ e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
	timer := time.NewTimer(p.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return e2bruntime.CommandResult{}, ctx.Err()
	case <-timer.C:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expired = time.Now().After(p.expires)
	if p.expired {
		return e2bruntime.CommandResult{}, errors.New("provider lease expired while creating")
	}
	return e2bruntime.CommandResult{}, nil
}
func (p *preparationTestProvider) SetTimeout(_ context.Context, id string, ttl time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != "preparation-fixture" || ttl != p.ttl {
		return fmt.Errorf("unexpected provider renewal %q %s", id, ttl)
	}
	if time.Now().After(p.expires) {
		p.expired = true
		return errors.New("expired provider")
	}
	p.expires = time.Now().Add(ttl)
	p.renewals++
	return nil
}
func (p *preparationTestProvider) Kill(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kills = append(p.kills, id)
	return nil
}

func preparationTestDatabase(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("TEST_RUNTIME_DATABASE_URL")
	if dsn == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		dsn = cfg.Database.URL
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "pr511_preparation_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("cleanup isolated schema: %v", err)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cfg := config.Config{}
	cfg.Database.URL = parsed.String()
	database, err := db.OpenExisting(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return database
}
func preparationTestWork(t *testing.T, database *db.DB) db.EnvironmentWork {
	t.Helper()
	org, workspace, environment, session := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	_, err := database.SQLDB().ExecContext(t.Context(), `INSERT INTO sessions(uuid,external_id,organization_uuid,workspace_uuid,created_by_api_key_uuid,environment_uuid,environment_external_id,agent_uuid,agent_external_id,agent_version,agent_snapshot) VALUES($1,'sesn_preparation',$2,$3,$4,$5,'env_preparation',$6,'agent_preparation',1,'{}')`, session, org, workspace, uuid.NewV4().String(), environment, uuid.NewV4().String())
	if err != nil {
		t.Fatal(err)
	}
	work, err := database.CreateEnvironmentWork(t.Context(), db.EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: "work_preparation", OrganizationUUID: org, WorkspaceUUID: workspace, EnvironmentUUID: environment, EnvironmentExternalID: "env_preparation", SessionUUID: session, State: "active", Metadata: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func TestSandboxPreparationDoesNotRenewTerminalOwners(t *testing.T) {
	for _, state := range []string{"failed", "stopped", "stopping", "work stopped", "session terminated", "session archived", "replaced"} {
		t.Run(state, func(t *testing.T) {
			database := preparationTestDatabase(t)
			work := preparationTestWork(t, database)
			provider := &preparationTestProvider{ttl: time.Minute, expires: time.Now().Add(time.Minute)}
			runner := &Runner{db: database, provider: provider}
			record := preparationTestSandbox(t, database, work)
			switch state {
			case "work stopped":
				if _, err := database.StopEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID, true); err != nil {
					t.Fatal(err)
				}
			case "session terminated":
				if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE sessions SET status='terminated' WHERE uuid=$1`, work.SessionUUID); err != nil {
					t.Fatal(err)
				}
			case "session archived":
				if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE sessions SET archived_at=NOW() WHERE uuid=$1`, work.SessionUUID); err != nil {
					t.Fatal(err)
				}
			case "replaced":
				if err := database.UpdateEnvironmentSandboxState(t.Context(), work.WorkspaceUUID, record.ExternalID, "failed", nil, nil, nil); err != nil {
					t.Fatal(err)
				}
				preparationTestSandbox(t, database, work)
			default:
				if err := database.UpdateEnvironmentSandboxState(t.Context(), work.WorkspaceUUID, record.ExternalID, state, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			err := runner.renewSandboxPreparation(t.Context(), record, work, "preparation-fixture", provider.ttl)
			if !errors.Is(err, db.ErrInvalidState) || provider.renewals != 0 {
				t.Fatalf("terminal owner renewed: error=%v renewals=%d", err, provider.renewals)
			}
		})
	}
}

func TestSandboxPreparationCanceledOwnerStillPersistsCleanup(t *testing.T) {
	database := preparationTestDatabase(t)
	work := preparationTestWork(t, database)
	record := preparationTestSandbox(t, database, work)
	provider := &preparationTestProvider{}
	runner := &Runner{db: database, provider: provider, logger: logging.LoggerOrDefault(nil)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runner.failCreatedSandbox(ctx, record, &work, "preparation-fixture", context.Canceled)
	var state string
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT state FROM environment_sandboxes WHERE uuid=$1`, record.UUID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "failed" {
		t.Fatalf("canceled cleanup left sandbox %s", state)
	}
	current, err := database.GetEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
	if err != nil || current.State != "stopped" {
		t.Fatalf("canceled cleanup left work %s: %v", current.State, err)
	}
}

func preparationTestSandbox(t *testing.T, database *db.DB, work db.EnvironmentWork) db.EnvironmentSandbox {
	t.Helper()
	providerID := "preparation-fixture"
	record, err := database.CreateEnvironmentSandbox(t.Context(), db.EnvironmentSandbox{UUID: uuid.NewV4().String(), ExternalID: "envsbx_" + uuid.NewV4().String(), OrganizationUUID: work.OrganizationUUID, WorkspaceUUID: work.WorkspaceUUID, EnvironmentUUID: work.EnvironmentUUID, EnvironmentExternalID: work.EnvironmentExternalID, WorkUUID: &work.UUID, WorkExternalID: &work.ExternalID, Provider: "e2b", Template: "fixture", ProviderSandboxID: &providerID, State: "creating", Metadata: []byte(`{}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestSandboxPreparationOldCleanupDoesNotStopReplacement(t *testing.T) {
	for _, sameProvider := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared_provider_%t", sameProvider), func(t *testing.T) {
			database := preparationTestDatabase(t)
			work := preparationTestWork(t, database)
			record := preparationTestSandbox(t, database, work)
			replacementUUID := uuid.NewV4().String()
			replacementID := "replacement-fixture"
			if sameProvider {
				replacementID = "preparation-fixture"
			}
			_, err := database.SQLDB().ExecContext(t.Context(), `INSERT INTO environment_sandboxes (
    uuid, external_id, organization_uuid, workspace_uuid, environment_uuid, environment_external_id,
    work_uuid, work_external_id, provider, template, provider_sandbox_id, state, metadata, created_at, updated_at)
    SELECT $1, $2, organization_uuid, workspace_uuid, environment_uuid, environment_external_id,
    work_uuid, work_external_id, provider, template, $3, 'creating', metadata, created_at + interval '1 second', NOW()
    FROM environment_sandboxes WHERE uuid=$4`, replacementUUID, "envsbx_"+replacementUUID, replacementID, record.UUID)
			if err != nil {
				t.Fatal(err)
			}
			var oldState string
			if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT state FROM environment_sandboxes WHERE uuid=$1`, record.UUID).Scan(&oldState); err != nil {
				t.Fatal(err)
			}
			if oldState != "creating" {
				t.Fatalf("fixture hid creating orphan: %s", oldState)
			}
			provider := &preparationTestProvider{}
			runner := &Runner{db: database, provider: provider, logger: logging.LoggerOrDefault(nil)}
			runner.failCreatedSandbox(t.Context(), record, &work, "preparation-fixture", db.ErrInvalidState)
			var lastError *string
			var stoppedAt *time.Time
			if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT state,last_error,stopped_at FROM environment_sandboxes WHERE uuid=$1`, record.UUID).Scan(&oldState, &lastError, &stoppedAt); err != nil {
				t.Fatal(err)
			}
			if oldState != "failed" || lastError == nil || stoppedAt == nil {
				t.Fatalf("replaced creating attempt not retired: state=%s error=%v stopped=%v", oldState, lastError, stoppedAt)
			}
			current, err := database.GetEnvironmentWork(t.Context(), work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
			if err != nil || current.State != "active" {
				t.Fatalf("old cleanup changed replacement work: %s %v", current.State, err)
			}
			pending, found, err := database.GetPendingEnvironmentSandboxForWork(t.Context(), work)
			if err != nil || !found || pending.UUID != replacementUUID || pending.State != "creating" || pending.ProviderSandboxID == nil || *pending.ProviderSandboxID != replacementID {
				t.Fatalf("old cleanup changed replacement: %+v %v", pending, err)
			}
			if sameProvider {
				if len(provider.kills) != 0 {
					t.Fatalf("killed replacement's shared provider: %v", provider.kills)
				}
			} else if !slices.Equal(provider.kills, []string{"preparation-fixture"}) {
				t.Fatalf("wrong provider killed: %v", provider.kills)
			}
		})
	}
}

func TestSandboxPreparationExpiredAttemptIsRetired(t *testing.T) {
	database := preparationTestDatabase(t)
	work := preparationTestWork(t, database)
	scope := "organization"
	if _, err := database.CreateEnvironment(t.Context(), db.Environment{UUID: work.EnvironmentUUID, ExternalID: work.EnvironmentExternalID, OrganizationUUID: work.OrganizationUUID, WorkspaceUUID: work.WorkspaceUUID, CreatedByAPIKeyUUID: uuid.NewV4().String(), Name: "preparation", Config: []byte(`{}`), Metadata: []byte(`{}`), Scope: &scope, Provider: "e2b", ResolvedTemplate: "fixture", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	record := preparationTestSandbox(t, database, work)
	if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE environment_sandboxes SET created_at=$1 WHERE uuid=$2`, time.Now().Add(-sandboxPreparationMaxAge-time.Second), record.UUID); err != nil {
		t.Fatal(err)
	}
	provider := &preparationTestProvider{ttl: time.Minute, expires: time.Now().Add(time.Minute)}
	runner := &Runner{db: database, provider: provider, logger: logging.LoggerOrDefault(nil)}
	_, _, err := runner.EnsureSessionSandbox(t.Context(), db.CodeSession{ExternalID: "cse_preparation", OrganizationUUID: work.OrganizationUUID, WorkspaceUUID: work.WorkspaceUUID, EnvironmentExternalID: work.EnvironmentExternalID, SessionUUID: work.SessionUUID})
	if !errors.Is(err, db.ErrInvalidState) {
		t.Fatalf("expired attempt: %v", err)
	}
	var state string
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT state FROM environment_sandboxes WHERE uuid=$1`, record.UUID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || provider.renewals != 0 {
		t.Fatalf("expired creating attempt kept alive: state=%s renewals=%d", state, provider.renewals)
	}
}

//go:build e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/google/uuid"
	e2b "github.com/superduck-ai/e2b-go-sdk"
)

func TestE2BEnvironmentRunnerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real E2B integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(cfg.E2BAPIKey) == "" {
		t.Fatal("E2B_API_KEY is required for the real E2B integration test")
	}
	if cfg.E2BDebug {
		t.Fatal("E2B_DEBUG must be false for the real E2B integration test")
	}
	if cfg.E2BRequestTimeout < 2*time.Minute {
		cfg.E2BRequestTimeout = 2 * time.Minute
	}
	if cfg.E2BSandboxTimeout < time.Minute {
		cfg.E2BSandboxTimeout = time.Minute
	}

	database, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	if err := database.Seed(ctx, cfg.SeedAPIKeys); err != nil {
		t.Fatalf("seed database: %v", err)
	}

	apiKey, err := database.GetAPIKey(ctx, auth.HashAPIKey(config.DefaultAPIKey))
	if err != nil {
		t.Fatalf("load default api key: %v", err)
	}

	template := strings.TrimSpace(cfg.E2BTemplate)
	if template == "" {
		template = "claude-code-interpreter"
	}
	envID, err := ids.New("env_")
	if err != nil {
		t.Fatalf("create environment id: %v", err)
	}
	workID, err := ids.New("work_")
	if err != nil {
		t.Fatalf("create work id: %v", err)
	}
	envConfig := mustJSON(t, map[string]any{
		"type":       "cloud",
		"runtime":    "self_hosted",
		"image":      template,
		"packages":   []any{},
		"networking": map[string]any{"type": "unrestricted"},
	})
	now := time.Now().UTC()
	env, err := database.CreateEnvironment(ctx, db.Environment{
		UUID:              uuid.NewString(),
		ExternalID:        envID,
		OrganizationID:    apiKey.OrganizationID,
		WorkspaceID:       apiKey.WorkspaceID,
		CreatedByAPIKeyID: apiKey.ID,
		Name:              "e2b-integration-" + envID[len("env_"):len("env_")+8],
		Description:       "Real E2B integration smoke test",
		Config:            envConfig,
		Metadata:          mustJSON(t, map[string]any{"source": "e2b_integration_test"}),
		Provider:          "e2b",
		ResolvedTemplate:  template,
		CreatedAt:         now,
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	defer cleanupE2BIntegrationRows(t, database, env.ExternalID, workID)

	work, err := database.CreateEnvironmentWork(ctx, db.EnvironmentWork{
		UUID:                  uuid.NewString(),
		ExternalID:            workID,
		OrganizationID:        env.OrganizationID,
		WorkspaceID:           env.WorkspaceID,
		EnvironmentID:         env.ID,
		EnvironmentExternalID: env.ExternalID,
		Data:                  mustJSON(t, map[string]any{"task": "e2b integration smoke"}),
		Metadata:              mustJSON(t, map[string]any{"source": "e2b_integration_test"}),
		State:                 "queued",
		CreatedAt:             now,
	})
	if err != nil {
		t.Fatalf("create environment work: %v", err)
	}

	provider := e2bruntime.NewProvider(cfg)
	runner := environments.NewRunner(database, provider)
	processed, err := runner.RunOnce(ctx, "e2b-integration-test")
	if err != nil {
		t.Fatalf("run environment runner once: %v", err)
	}
	if !processed {
		t.Fatal("environment runner did not process queued work")
	}

	sandboxID, _, sandboxState, sandboxTemplate, sandboxLastError := loadE2BSandboxRow(t, database, env.ExternalID, work.ExternalID)
	if sandboxLastError != "" {
		t.Fatalf("sandbox row has last_error: %s", sandboxLastError)
	}
	if sandboxState != "running" {
		t.Fatalf("sandbox state = %s, want running", sandboxState)
	}
	if sandboxTemplate != template {
		t.Fatalf("sandbox template = %s, want %s", sandboxTemplate, template)
	}
	if strings.TrimSpace(sandboxID) == "" {
		t.Fatal("provider sandbox id was not recorded")
	}

	killed := false
	defer func() {
		if !killed {
			killCtx, cancelKill := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelKill()
			_ = provider.Kill(killCtx, sandboxID)
		}
	}()

	connected, err := e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err != nil {
		t.Fatalf("connect to real sandbox %s: %v", sandboxID, err)
	}
	commandTimeoutMs := 30_000
	execution, err := connected.Commands.Run(ctx, `printf 'sandbox-ok\n'`, &e2b.CommandStartOpts{
		TimeoutMs: &commandTimeoutMs,
	})
	if err != nil {
		t.Fatalf("run command in real sandbox %s: %v", sandboxID, err)
	}
	result, ok := execution.(*e2b.CommandResult)
	if !ok {
		t.Fatalf("command execution type = %T, want *e2b.CommandResult", execution)
	}
	if got := strings.TrimSpace(result.Stdout); got != "sandbox-ok" {
		t.Fatalf("sandbox command stdout = %q, want sandbox-ok; stderr=%q", got, result.Stderr)
	}

	credentials, err := codesessions.NewSessionCredentials(cfg)
	if err != nil {
		t.Fatalf("create code session credentials: %v", err)
	}
	server := httptest.NewServer(api.NewServerWithPlatformSessionsAndCredentials(
		cfg,
		database,
		newFakeStore("e2b-integration-fake"),
		nil,
		nil,
		credentials,
	))
	defer server.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/environments/"+env.ExternalID+"/work/"+work.ExternalID+"/stop?beta=true", strings.NewReader(`{"force":true}`))
	if err != nil {
		t.Fatalf("new stop request: %v", err)
	}
	req.Header.Set("X-Api-Key", config.DefaultAPIKey)
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("stop work request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop work status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	killed = true

	_, _, stoppedSandboxState, _, stoppedSandboxLastError := loadE2BSandboxRow(t, database, env.ExternalID, work.ExternalID)
	if stoppedSandboxLastError != "" {
		t.Fatalf("stopped sandbox row has last_error: %s", stoppedSandboxLastError)
	}
	if stoppedSandboxState != "stopped" {
		t.Fatalf("sandbox state after stop = %s, want stopped", stoppedSandboxState)
	}
	stoppedWork, err := database.GetEnvironmentWork(ctx, env.WorkspaceID, env.ExternalID, work.ExternalID)
	if err != nil {
		t.Fatalf("load stopped work: %v", err)
	}
	if stoppedWork.State != "stopped" {
		t.Fatalf("work state after stop = %s, want stopped", stoppedWork.State)
	}

	_, err = e2b.Connect(ctx, sandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(cfg),
	})
	if err == nil {
		_ = provider.Kill(context.Background(), sandboxID)
		t.Fatalf("connect to sandbox %s after kill succeeded, want failure", sandboxID)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

func e2bConnectionOptsFromConfig(cfg config.Config) e2b.ConnectionOpts {
	requestTimeoutMs := int(cfg.E2BRequestTimeout / time.Millisecond)
	debug := cfg.E2BDebug
	return e2b.ConnectionOpts{
		ApiKey:           cfg.E2BAPIKey,
		AccessToken:      cfg.E2BAccessToken,
		Domain:           cfg.E2BDomain,
		ApiUrl:           cfg.E2BAPIURL,
		SandboxUrl:       cfg.E2BSandboxURL,
		Debug:            &debug,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func loadE2BSandboxRow(t *testing.T, database *db.DB, envID, workID string) (providerSandboxID, externalID, state, template, lastError string) {
	t.Helper()
	var lastErrorPtr *string
	if err := database.Pool.QueryRow(context.Background(), `
		select coalesce(provider_sandbox_id, ''), external_id, state, template, last_error
		from environment_sandboxes
		where environment_external_id = $1 and work_external_id = $2
		order by created_at desc, id desc
		limit 1
	`, envID, workID).Scan(&providerSandboxID, &externalID, &state, &template, &lastErrorPtr); err != nil {
		t.Fatalf("load sandbox row: %v", err)
	}
	if lastErrorPtr != nil {
		lastError = *lastErrorPtr
	}
	return providerSandboxID, externalID, state, template, lastError
}

func cleanupE2BIntegrationRows(t *testing.T, database *db.DB, envID, workID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := database.Pool.Exec(ctx, `delete from environment_sandboxes where environment_external_id = $1 or work_external_id = $2`, envID, workID); err != nil {
		t.Fatalf("cleanup integration sandbox rows: %v", err)
	}
	if _, err := database.Pool.Exec(ctx, `delete from environment_work where environment_external_id = $1 or external_id = $2`, envID, workID); err != nil {
		t.Fatalf("cleanup integration work rows: %v", err)
	}
	if _, err := database.Pool.Exec(ctx, `delete from environments where external_id = $1`, envID); err != nil {
		t.Fatalf("cleanup integration environment rows: %v", err)
	}
}

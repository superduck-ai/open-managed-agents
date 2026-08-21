//go:build e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

func TestE2BProviderLifecycleIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real E2B integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(cfg.E2B.APIKey) == "" {
		t.Fatal("e2b.api_key is required in config/config.yaml for the real E2B integration test")
	}
	if cfg.E2B.Debug {
		t.Fatal("e2b.debug must be false for the real E2B integration test")
	}
	if cfg.E2B.RequestTimeout < 2*time.Minute {
		cfg.E2B.RequestTimeout = 2 * time.Minute
	}
	if cfg.E2B.SandboxTimeout < time.Minute {
		cfg.E2B.SandboxTimeout = time.Minute
	}

	template := strings.TrimSpace(cfg.E2B.Template)
	if template == "" {
		template = config.DefaultE2BTemplate
	}
	envID, err := ids.New("env_")
	if err != nil {
		t.Fatalf("create environment id: %v", err)
	}
	workID, err := ids.New("work_")
	if err != nil {
		t.Fatalf("create work id: %v", err)
	}
	envConfig, err := json.Marshal(map[string]any{
		"type":       "cloud",
		"runtime":    "self_hosted",
		"image":      template,
		"packages":   []any{},
		"networking": map[string]any{"type": "unrestricted"},
	})
	if err != nil {
		t.Fatalf("marshal Environment config: %v", err)
	}
	env := db.Environment{
		ExternalID:       envID,
		WorkspaceUUID:    "e2b-integration-workspace",
		Config:           envConfig,
		Provider:         "e2b",
		ResolvedTemplate: template,
	}
	work := db.EnvironmentWork{
		ExternalID:            workID,
		EnvironmentExternalID: envID,
		Metadata:              json.RawMessage(`{"source":"e2b_integration_test"}`),
	}

	provider := e2bruntime.NewProvider(cfg.E2B)
	resolution, err := provider.Resolve(env, &work)
	if err != nil {
		t.Fatalf("resolve E2B sandbox: %v", err)
	}
	if resolution.Template != template {
		t.Fatalf("resolved template = %q, want %q", resolution.Template, template)
	}
	sandbox, err := provider.Create(ctx, env, &work, resolution)
	if err != nil {
		t.Fatalf("create E2B sandbox: %v", err)
	}
	if strings.TrimSpace(sandbox.ID) == "" {
		t.Fatal("created E2B sandbox ID is empty")
	}

	killed := false
	defer func() {
		if !killed {
			killCtx, cancelKill := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancelKill()
			_ = provider.Kill(killCtx, sandbox.ID)
		}
	}()

	result, err := provider.RunCommand(ctx, sandbox.ID, e2bruntime.CommandRequest{
		Command: `printf 'sandbox-ok\n'`,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("run command in real sandbox %s: %v", sandbox.ID, err)
	}
	if got := strings.TrimSpace(string(result.Stdout)); got != "sandbox-ok" {
		t.Fatalf("sandbox command stdout = %q, want sandbox-ok; stderr=%q", got, result.Stderr)
	}

	if err := provider.Kill(ctx, sandbox.ID); err != nil {
		t.Fatalf("kill E2B sandbox %s: %v", sandbox.ID, err)
	}
	killed = true

	_, err = e2b.Connect(ctx, sandbox.ID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bruntime.ConnectionOptsFromConfig(cfg.E2B),
	})
	if err == nil {
		_ = provider.Kill(context.Background(), sandbox.ID)
		t.Fatalf("connect to sandbox %s after kill succeeded, want failure", sandbox.ID)
	}
}

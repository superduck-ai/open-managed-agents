//go:build e2b_integration && e2e

package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	e2b "github.com/superduck-ai/e2b-go-sdk"
)

type realSessionSandbox struct {
	workID  string
	sandbox *e2b.Sandbox
	stopped bool
}

func TestE2BEnvironmentUpdateAndSessionFilesystemIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	requireFullE2BBridgeConfig(t, cfg)
	cfg.CodeSession.SandboxAPIBaseURL = ""
	cfg.E2B.Template = config.DefaultE2BTemplate
	if cfg.E2B.RequestTimeout < 2*time.Minute {
		cfg.E2B.RequestTimeout = 2 * time.Minute
	}
	if cfg.E2B.SandboxTimeout < 10*time.Minute {
		cfg.E2B.SandboxTimeout = 10 * time.Minute
	}

	app := newTestAppWithStore(t, &cfg, newFakeStore("package-lifecycle-e2e-bucket"))
	t.Cleanup(app.close)
	quickstartEnsureSandboxIngress(t, app)
	cfg = app.cfg
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	agent := createPackageLifecycleAgent(t, ctx, client)
	defer client.Beta.Agents.Archive(context.Background(), agent.ID, anthropic.BetaAgentArchiveParams{})
	environment := createPackageLifecycleEnvironment(t, ctx, client, "six==1.16.0")
	defer client.Beta.Environments.Delete(context.Background(), environment.ID, anthropic.BetaEnvironmentDeleteParams{})

	provider := e2bruntime.NewProvider(cfg)
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, app.store, app.credentials)
	first := launchRealSessionSandbox(t, ctx, app, client, runner, provider, environment.ID, agent.ID, "packages-before-update")
	assertSandboxPythonPackageVersion(t, ctx, first.sandbox, "six", "1.16.0")

	updated, err := client.Beta.Environments.Update(ctx, environment.ID, anthropic.BetaEnvironmentUpdateParams{
		Config: anthropic.BetaEnvironmentUpdateParamsConfigUnion{OfCloud: &anthropic.BetaCloudConfigParams{
			Packages: anthropic.BetaPackagesParams{
				Type: anthropic.BetaPackagesParamsTypePackages,
				Pip:  []string{"six==1.17.0"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("update Environment packages: %v", err)
	}
	if len(updated.Config.Packages.Pip) != 1 || updated.Config.Packages.Pip[0] != "six==1.17.0" {
		t.Fatalf("updated packages = %#v, want six==1.17.0", updated.Config.Packages.Pip)
	}
	assertSandboxPythonPackageVersion(t, ctx, first.sandbox, "six", "1.16.0")

	second := launchRealSessionSandbox(t, ctx, app, client, runner, provider, environment.ID, agent.ID, "packages-after-update")
	assertSandboxPythonPackageVersion(t, ctx, second.sandbox, "six", "1.17.0")
	assertIndependentSessionFilesystems(t, ctx, first.sandbox, second.sandbox)

	first.stopWork(t, app, environment.ID)
	second.stopWork(t, app, environment.ID)
}

func createPackageLifecycleAgent(t *testing.T, ctx context.Context, client anthropic.Client) *anthropic.BetaManagedAgentsAgent {
	t.Helper()
	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name:  "Package lifecycle E2E agent",
		Model: anthropic.BetaManagedAgentsModelConfigParams{ID: anthropic.BetaManagedAgentsModelClaudeOpus4_8},
	})
	if err != nil {
		t.Fatalf("create lifecycle agent: %v", err)
	}
	return agent
}

func createPackageLifecycleEnvironment(t *testing.T, ctx context.Context, client anthropic.Client, pipSpec string) *anthropic.BetaEnvironment {
	t.Helper()
	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "package-lifecycle-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
		Config: anthropic.BetaEnvironmentNewParamsConfigUnion{OfCloud: &anthropic.BetaCloudConfigParams{
			Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{}},
			Packages: anthropic.BetaPackagesParams{
				Type: anthropic.BetaPackagesParamsTypePackages,
				Pip:  []string{pipSpec},
			},
		}},
	})
	if err != nil {
		t.Fatalf("create lifecycle Environment: %v", err)
	}
	return environment
}

func launchRealSessionSandbox(
	t *testing.T,
	ctx context.Context,
	app *testApp,
	client anthropic.Client,
	runner *environments.Runner,
	provider *e2bruntime.E2BProvider,
	environmentID string,
	agentID string,
	title string,
) *realSessionSandbox {
	t.Helper()
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agentID)},
		EnvironmentID: environmentID,
		Title:         anthropic.String(title),
	})
	if err != nil {
		t.Fatalf("create Session %s: %v", title, err)
	}
	t.Cleanup(func() {
		_, _ = client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})
	})
	workID := quickstartFindSessionEnvironmentWorkID(t, app, environmentID, session.ID)
	result := &realSessionSandbox{workID: workID}
	var providerSandboxID string
	t.Cleanup(func() {
		if providerSandboxID == "" || result.stopped {
			return
		}
		killCtx, cancelKill := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancelKill()
		_ = provider.Kill(killCtx, providerSandboxID)
	})
	t.Cleanup(func() {
		result.stopWork(t, app, environmentID)
	})
	processed, err := runner.RunOnce(ctx, "package-lifecycle-"+title)
	if err != nil {
		t.Fatalf("launch Session Sandbox %s: %v", title, err)
	}
	if !processed {
		t.Fatalf("runner did not process Session %s", title)
	}
	providerSandboxID, _ = quickstartWaitForProviderSandboxMetadata(t, ctx, app, environmentID, workID)
	sandbox, err := e2b.Connect(ctx, providerSandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bConnectionOptsFromConfig(app.cfg),
	})
	if err != nil {
		t.Fatalf("connect to Session Sandbox %s: %v", title, err)
	}
	result.sandbox = sandbox
	return result
}

func (s *realSessionSandbox) stopWork(t *testing.T, app *testApp, environmentID string) {
	t.Helper()
	if s.stopped {
		return
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelStop()
	quickstartStopEnvironmentWork(t, stopCtx, app, environmentID, s.workID)
	s.stopped = true
}

func assertSandboxPythonPackageVersion(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, module, want string) {
	t.Helper()
	command := fmt.Sprintf("python3 -c 'import %s; print(%s.__version__)'", module, module)
	stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second)
	if err != nil {
		t.Fatalf("probe Python package %s: %v stdout=%q stderr=%q", module, err, stdout, stderr)
	}
	if got := strings.TrimSpace(stdout); got != want {
		t.Fatalf("sandbox %s version = %q, want %q; stderr=%q", module, got, want, stderr)
	}
}

func assertIndependentSessionFilesystems(t *testing.T, ctx context.Context, first, second *e2b.Sandbox) {
	t.Helper()
	const path = "/tmp/oma-session-isolation-proof"
	writeSandboxFile(t, ctx, first, path, "first-session")
	writeSandboxFile(t, ctx, second, path, "second-session")
	assertSandboxFile(t, ctx, first, path, "first-session")
	assertSandboxFile(t, ctx, second, path, "second-session")
}

func writeSandboxFile(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, path, value string) {
	t.Helper()
	command := fmt.Sprintf("printf '%%s' %s > %s", shellPath(value), shellPath(path))
	if _, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second); err != nil {
		t.Fatalf("write Sandbox isolation file: %v stderr=%q", err, stderr)
	}
}

func assertSandboxFile(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, path, want string) {
	t.Helper()
	stdout, stderr, err := runE2BCommand(ctx, sandbox, "cat "+shellPath(path), 30*time.Second)
	if err != nil {
		t.Fatalf("read Sandbox isolation file: %v stderr=%q", err, stderr)
	}
	if got := strings.TrimSpace(stdout); got != want {
		t.Fatalf("Sandbox isolation file = %q, want %q", got, want)
	}
}

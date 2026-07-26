package environments

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestNewRunnerRejectsMissingDependencies(t *testing.T) {
	database := new(db.DB)
	valid := RunnerDependencies{
		DB:              database,
		Provider:        e2bruntime.NewProvider(config.E2BConfig{}),
		CodeSessions:    codesessions.NewServiceWithCredentials(database, new(codesessions.SessionCredentials)),
		Skills:          skillsapi.NewRuntimeResolver(config.Config{}, database, nil),
		FilestoreTokens: new(filestore.TokenCredentials),
	}
	tests := []struct {
		name   string
		clear  func(*RunnerDependencies)
		wanted string
	}{
		{
			name:   "database",
			clear:  func(deps *RunnerDependencies) { deps.DB = nil },
			wanted: "database is required",
		},
		{
			name:   "provider",
			clear:  func(deps *RunnerDependencies) { deps.Provider = nil },
			wanted: "sandbox provider is required",
		},
		{
			name:   "code sessions",
			clear:  func(deps *RunnerDependencies) { deps.CodeSessions = nil },
			wanted: "code session runtime is required",
		},
		{
			name:   "skills",
			clear:  func(deps *RunnerDependencies) { deps.Skills = nil },
			wanted: "skill resolver is required",
		},
		{
			name:   "filestore tokens",
			clear:  func(deps *RunnerDependencies) { deps.FilestoreTokens = nil },
			wanted: "filestore token issuer is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := valid
			test.clear(&deps)
			runner, err := NewRunner(deps)
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("NewRunner() error = %v, want containing %q", err, test.wanted)
			}
			if runner != nil {
				t.Fatalf("NewRunner() runner = %#v, want nil", runner)
			}
		})
	}
}

func TestNewRunnerAcceptsCompleteDependencies(t *testing.T) {
	database := new(db.DB)
	runner, err := NewRunner(RunnerDependencies{
		DB:              database,
		Provider:        e2bruntime.NewProvider(config.E2BConfig{}),
		Config:          config.Config{},
		CodeSessions:    codesessions.NewServiceWithCredentials(database, new(codesessions.SessionCredentials)),
		Skills:          skillsapi.NewRuntimeResolver(config.Config{}, database, nil),
		FilestoreTokens: new(filestore.TokenCredentials),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if runner == nil {
		t.Fatal("NewRunner() returned nil runner")
	}
}

package environments

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

type bootstrapCodeSessions struct {
	*codesessions.Service
	provisioner codesessions.SandboxProvisioner
	bindings    int
}

func (s *bootstrapCodeSessions) WithSandboxProvisioner(provisioner codesessions.SandboxProvisioner) *codesessions.Service {
	s.provisioner = provisioner
	s.bindings++
	return s.Service.WithSandboxProvisioner(provisioner)
}

func TestRunnerBootstrapDoesNotBindInvalidDependencies(t *testing.T) {
	database := new(db.DB)
	service := &bootstrapCodeSessions{Service: codesessions.NewServiceWithCredentials(database, new(codesessions.SessionCredentials), nil)}
	if runner, err := NewRunner(RunnerDependencies{CodeSessions: service}); err == nil || runner != nil {
		t.Fatal("incomplete constructor accepted")
	}
	if service.bindings != 0 || service.provisioner != nil {
		t.Fatal("failed construction installed a partial Runner")
	}
}

func TestRunnerBootstrapBindsActualConstructedSandboxResolver(t *testing.T) {
	database := new(db.DB)
	service := &bootstrapCodeSessions{Service: codesessions.NewServiceWithCredentials(database, new(codesessions.SessionCredentials), nil)}
	runner, err := NewRunner(RunnerDependencies{DB: database, Provider: e2bruntime.NewProvider(config.E2BConfig{}), CodeSessions: service, Skills: skillsapi.NewRuntimeResolver(database), FilestoreTokens: new(filestore.TokenCredentials)})
	if err != nil {
		t.Fatal(err)
	}
	if service.provisioner != runner || service.bindings != 1 {
		t.Fatal("Runner Code Session service has no actual sandbox resolver; repository dispatch would return sandbox unavailable")
	}
	if _, ok := service.provisioner.(interface {
		RepositorySkills(ctx context.Context, code db.CodeSession, roots []string) ([]codesessions.RepositorySkill, error)
	}); !ok {
		t.Fatal("bound provisioner cannot discover repository Skills")
	}
}

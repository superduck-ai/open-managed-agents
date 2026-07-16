package e2bruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environmentconfig"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

type Resolution struct {
	Template            string
	Metadata            map[string]string
	Envs                map[string]string
	Timeout             time.Duration
	AllowInternetAccess bool
	Network             *e2b.SandboxNetworkOpts
}

type Sandbox struct {
	ID string
}

type Provider interface {
	Create(ctx context.Context, env db.Environment, work *db.EnvironmentWork, resolution Resolution) (Sandbox, error)
	Kill(ctx context.Context, sandboxID string) error
	Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error)
	WriteFile(ctx context.Context, sandboxID string, path string, data []byte) error
	RunCommand(ctx context.Context, sandboxID string, command string) error
}

type SkillMountPreparer interface {
	PrepareSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*SkillMount, error)
}

type E2BProvider struct {
	cfg       config.Config
	sandboxes sandboxOperations
}

func NewProvider(cfg config.Config) *E2BProvider {
	return &E2BProvider{
		cfg:       cfg,
		sandboxes: newE2BSandboxOperations(cfg),
	}
}

func (p *E2BProvider) Resolve(env db.Environment, work *db.EnvironmentWork) (Resolution, error) {
	template := strings.TrimSpace(env.ResolvedTemplate)
	if template == "" {
		template = strings.TrimSpace(p.cfg.E2BTemplate)
	}
	if template == "" {
		template = "claude-code-interpreter"
	}
	resolved := Resolution{
		Template:            template,
		Metadata:            map[string]string{"environment_id": env.ExternalID, "workspace_id": fmt.Sprint(env.WorkspaceID)},
		Envs:                map[string]string{"ANTHROPIC_ENVIRONMENT_ID": env.ExternalID},
		Timeout:             p.cfg.E2BSandboxTimeout,
		AllowInternetAccess: true,
	}
	if work != nil {
		resolved.Metadata["work_id"] = work.ExternalID
		resolved.Envs["ANTHROPIC_WORK_ID"] = work.ExternalID
	}

	environmentConfig, err := environmentconfig.DecodeStored(env.Config)
	if err != nil {
		return Resolution{}, err
	}
	network, allowInternet, err := resolveNetwork(environmentConfig, mcpAllowedHostsFromWork(work))
	if err != nil {
		return Resolution{}, err
	}
	resolved.AllowInternetAccess = allowInternet
	resolved.Network = network
	return resolved, nil
}

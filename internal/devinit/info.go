package devinit

import (
	"fmt"
	"io"
)

// InfoReport is the machine-readable view of the worktree environment
// binding. It never carries secret material.
type InfoReport struct {
	Worktree        string      `json:"worktree"`
	Enabled         bool        `json:"enabled"`
	Name            string      `json:"name,omitempty"`
	EnvID           string      `json:"env_id,omitempty"`
	APIURL          string      `json:"api_url,omitempty"`
	WebPublicURL    string      `json:"web_public_url,omitempty"`
	APIRoute        string      `json:"api_route,omitempty"`
	WebRoute        string      `json:"web_route,omitempty"`
	Stack           *StackPorts `json:"stack,omitempty"`
	InitFile        string      `json:"init_file,omitempty"`
	InitState       string      `json:"init_state,omitempty"`
	CompletedAt     string      `json:"completed_at,omitempty"`
	CredentialsPath string      `json:"credentials_path,omitempty"`
	GeneratedConfig string      `json:"generated_config,omitempty"`
}

// CollectInfo resolves the current worktree environment binding.
func CollectInfo(repoRoot string) (InfoReport, bool, error) {
	repoRoot, err := filepathEvalSymlinks(repoRoot)
	if err != nil {
		return InfoReport{}, false, err
	}
	report := InfoReport{Worktree: repoRoot}
	binding, found, err := LoadBinding(envFilePath(repoRoot))
	if err != nil {
		return report, false, err
	}
	if !found {
		return report, false, nil
	}
	base, err := loadBaseConfig(repoRoot)
	if err != nil {
		return report, false, err
	}
	layout, err := NewLayout(binding, base, repoRoot)
	if err != nil {
		return report, false, err
	}
	report.Enabled = true
	report.Name = binding.Name
	report.EnvID = binding.EnvID
	report.APIURL = "http://" + layout.APIRoute + ".localhost"
	report.WebPublicURL = layout.WebPublicURL
	report.APIRoute = layout.APIRoute
	report.WebRoute = layout.WebRoute
	report.InitFile = binding.InitFile
	report.InitState = "unknown"
	if state, found, err := LoadState(layout.StateFile); err == nil && found {
		report.InitState = state.Phase
		report.CompletedAt = state.CompletedAt
		report.Stack = state.Runtime
	}
	_, credentialsFound, err := LoadCredentials(layout.CredentialsFile)
	if err != nil {
		return report, true, err
	}
	if credentialsFound {
		report.CredentialsPath = layout.CredentialsFile
	}
	if pathExists(layout.GeneratedConfig) {
		report.GeneratedConfig = layout.GeneratedConfig
	}
	return report, true, nil
}

// WriteInfo prints the report without exposing any secret material.
func WriteInfo(report InfoReport, found bool, out io.Writer) error {
	fmt.Fprintf(out, "worktree: %s\n", report.Worktree)
	if !found {
		fmt.Fprintf(out, "environment init: not enabled (no .run/env.json)\n")
		return nil
	}
	fmt.Fprintf(out, "name: %s\n", report.Name)
	fmt.Fprintf(out, "env_id: %s\n", report.EnvID)
	fmt.Fprintf(out, "api_url: %s\n", report.APIURL)
	fmt.Fprintf(out, "web_url: %s\n", report.WebPublicURL)
	fmt.Fprintf(out, "api_route: %s\n", report.APIRoute)
	fmt.Fprintf(out, "web_route: %s\n", report.WebRoute)
	if report.Stack != nil {
		fmt.Fprintf(out, "stack: pg=%d redis=%d nats=%d minio=%d e2b=%d api=%d web=%d\n",
			report.Stack.PGPort, report.Stack.RedisPort, report.Stack.NATSPort,
			report.Stack.MinIOPort, report.Stack.E2BPort, report.Stack.APIPort, report.Stack.WebPort)
	}
	fmt.Fprintf(out, "init_file: %s\n", report.InitFile)
	fmt.Fprintf(out, "init_state: %s\n", report.InitState)
	fmt.Fprintf(out, "completed_at: %s\n", report.CompletedAt)
	if report.CredentialsPath != "" {
		fmt.Fprintf(out, "credentials: %s (4 api keys + brain claim token)\n", report.CredentialsPath)
	} else {
		fmt.Fprintf(out, "credentials: missing\n")
	}
	fmt.Fprintf(out, "generated_config: %s\n", report.GeneratedConfig)
	return nil
}

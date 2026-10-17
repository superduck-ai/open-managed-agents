package environments

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

const maxPromptPackageSpecs = 40

type environmentPromptInput struct {
	networking   networkpolicy.Config
	packages     *environmentPackages
	repositories []repositoryMount
}

type repositoryMount struct {
	repo      string
	mountPath string
}

const environmentPromptFiles = `## Files

When the user names a file or a relative path, check /mnt/session/uploads before the working directory: try the exact path first, then search with Glob using path /mnt/session/uploads and pattern **/<filename>. Attached files land there, so a search of the working directory alone misses them. Use a path exactly as you found it; do not guess, rename or rebuild one from metadata such as a file ID. If several files match, ask which one to use. Report a file as missing only after checking both locations.

Write user-facing deliverables, such as reports, exports and generated files, to /mnt/user-data/outputs. Keep ordinary source edits in the repository working directory.`

const environmentPromptPersistence = `## Persistence

The sandbox can be paused or replaced after the session sits idle. Uploads, files in /mnt/user-data/outputs and attached memory stores survive a replacement. Treat everything else as possibly lost, including uncommitted repository changes, /tmp, packages you installed and background processes, so do not depend on them across a long pause.`

func environmentPromptInputFromConfig(raw json.RawMessage) (environmentPromptInput, error) {
	networking, err := networkpolicy.ParseConfig(raw)
	if err != nil {
		return environmentPromptInput{}, err
	}
	packages, err := decodeEnvironmentPackages(raw)
	if err != nil {
		return environmentPromptInput{}, err
	}
	return environmentPromptInput{networking: networking, packages: packages}, nil
}

func (r managedAgentRuntimeResources) environmentPrompt() string {
	input := r.environment
	input.repositories = r.repositories
	return renderEnvironmentPrompt(input)
}

func renderEnvironmentPrompt(input environmentPromptInput) string {
	sections := []string{
		"# Environment\n\nThese notes describe the sandbox you work in. They do not change your assigned role.",
		renderEnvironmentPromptMounts(input.repositories),
		environmentPromptFiles,
		environmentPromptPersistence,
		renderEnvironmentPromptNetwork(input.networking),
		renderEnvironmentPromptSoftware(input.packages),
	}
	return strings.Join(sections, "\n\n")
}

func renderEnvironmentPromptMounts(repositories []repositoryMount) string {
	lines := []string{
		"## Mounted paths",
		"",
		"- /mnt/session/uploads (read-only): files the user attached. The public/API path /uploads/<path> is /mnt/session/uploads/<path> here. Never open /uploads/... and never form /mnt/session/uploads/uploads/....",
		"- /mnt/user-data/outputs (read-write): deliverables. A file written to /mnt/user-data/outputs/<path> is shown to the user as /outputs/<path>. Files anywhere else are working files the user does not see. Do not write to /outputs/... directly.",
	}
	for _, repository := range repositories {
		lines = append(lines, fmt.Sprintf("- %s: Git repository %s.", repository.mountPath, repository.repo))
	}
	return strings.Join(lines, "\n")
}

func renderEnvironmentPromptNetwork(networking networkpolicy.Config) string {
	if networking.Type != networkpolicy.TypeLimited {
		return "## Network\n\nOutbound network access is unrestricted."
	}
	lines := []string{"## Network", ""}
	hosts := networking.AllowedHostPatterns()
	if len(hosts) == 0 {
		lines = append(lines, "Outbound network access is limited. No general hosts are allowed.")
	} else {
		lines = append(lines, "Outbound network access is limited to these hosts: "+strings.Join(hosts, ", ")+".")
	}
	if networking.AllowPackageManagers {
		lines = append(lines, "Package registries are also reachable.")
	}
	if networking.AllowMCPServers {
		lines = append(lines, "The configured MCP servers are also reachable.")
	}
	lines = append(lines, "Requests to any other host are blocked at the network layer, whatever tool or library makes them. If a request is refused, do not retry it with another client; tell the user what you needed and from where.")
	return strings.Join(lines, "\n")
}

func renderEnvironmentPromptSoftware(packages *environmentPackages) string {
	lines := []string{"## Software", ""}
	if summary := environmentPackageSummary(packages); summary != "" {
		lines = append(lines, "Preinstalled for this environment: "+summary+".")
	}
	lines = append(lines, "Install anything else you need with the usual package managers. Installed packages do not survive a sandbox replacement.")
	return strings.Join(lines, "\n")
}

func environmentPackageSummary(packages *environmentPackages) string {
	if packages == nil {
		return ""
	}
	parts := []string{}
	remaining := maxPromptPackageSpecs
	omitted := 0
	for _, entry := range packages.specsByManager() {
		specs := *entry.specs
		if len(specs) == 0 {
			continue
		}
		shown := min(len(specs), remaining)
		omitted += len(specs) - shown
		remaining -= shown
		if shown > 0 {
			parts = append(parts, entry.manager+" "+strings.Join(specs[:shown], ", "))
		}
	}
	summary := strings.Join(parts, "; ")
	if omitted > 0 {
		summary += fmt.Sprintf("; and %d more", omitted)
	}
	return summary
}

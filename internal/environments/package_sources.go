package environments

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func packageSourcePrelude(sources config.EnvironmentPackageConfig) string {
	if sources.Empty() {
		return ""
	}
	var script strings.Builder
	script.WriteString("set -eu\n")
	if sources.PIP.IndexURL != "" {
		fmt.Fprintf(&script, "export PIP_INDEX_URL=%s\n", shellQuote(sources.PIP.IndexURL))
	}
	if sources.Go.Proxy != "" {
		fmt.Fprintf(&script, "export GOPROXY=%s\n", shellQuote(sources.Go.Proxy))
	}
	npmOptions := sources.NPM.Options()
	if len(npmOptions) == 0 && len(sources.Cargo.Registries) == 0 && len(sources.Gem.Sources) == 0 {
		return script.String()
	}
	script.WriteString("package_config_directory=$(mktemp -d)\n")
	script.WriteString(`trap 'rm -rf "$package_config_directory"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
export OMA_PACKAGE_CONFIG_DIRECTORY="$package_config_directory"
`)
	if len(npmOptions) > 0 {
		var npmConfig strings.Builder
		for _, option := range npmOptions {
			key, registry, _ := strings.Cut(strings.TrimPrefix(option, "--"), "=")
			fmt.Fprintf(&npmConfig, "%s=%s\n", key, strconv.Quote(registry))
		}
		script.WriteString(`npm_config_path="$package_config_directory/npmrc"
original_config="${NPM_CONFIG_USERCONFIG:-${npm_config_userconfig:-${HOME:+$HOME/.npmrc}}}"
if [ -f "$original_config" ]; then cat "$original_config" > "$npm_config_path"; fi
printf '\n' >> "$npm_config_path"
`)
		fmt.Fprintf(&script, "printf '%%s' %s >> \"$npm_config_path\"\n", shellQuote(npmConfig.String()))
		script.WriteString("export NPM_CONFIG_USERCONFIG=\"$npm_config_path\" npm_config_userconfig=\"$npm_config_path\"\n")
		if sources.NPM.Registry != "" {
			fmt.Fprintf(&script, "export NPM_CONFIG_REGISTRY=%s npm_config_registry=%s\n", shellQuote(sources.NPM.Registry), shellQuote(sources.NPM.Registry))
		}
	}
	if len(sources.Cargo.Registries) > 0 {
		script.WriteString("export OMA_PACKAGE_CARGO=$(command -v cargo || true)\n")
		var cargoConfig strings.Builder
		for _, name := range slices.Sorted(maps.Keys(sources.Cargo.Registries)) {
			fmt.Fprintf(&cargoConfig, "[registries.%s]\nindex = %s\n", name, strconv.Quote(sources.Cargo.Registries[name]))
		}
		fmt.Fprintf(&script, "printf '%%s' %s > \"$package_config_directory/config.toml\"\n", shellQuote(cargoConfig.String()))
		fmt.Fprintf(&script, "printf '%%s' %s > \"$package_config_directory/cargo\"\n", shellQuote(cargoRegistryShim))
		script.WriteString("chmod 700 \"$package_config_directory/cargo\"\n")
	}
	if len(sources.Gem.Sources) > 0 {
		var gemConfig strings.Builder
		gemConfig.WriteString(":sources:\n")
		for _, source := range sources.Gem.Sources {
			fmt.Fprintf(&gemConfig, "- %s\n", strconv.Quote(source))
		}
		fmt.Fprintf(&script, "printf '%%s' %s > \"$package_config_directory/gemrc\"\n", shellQuote(gemConfig.String()))
		script.WriteString("export GEMRC=\"${GEMRC:+$GEMRC:}$package_config_directory/gemrc\"\n")
	}
	if len(sources.Cargo.Registries) > 0 {
		script.WriteString("export PATH=\"$package_config_directory:$PATH\"\n")
	}
	return script.String()
}

const cargoRegistryShim = `#!/bin/sh
set -eu
exec "$OMA_PACKAGE_CARGO" --config "$OMA_PACKAGE_CONFIG_DIRECTORY/config.toml" "$@"
`

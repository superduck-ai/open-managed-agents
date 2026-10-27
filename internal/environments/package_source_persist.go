package environments

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

const packageSourcePersistTimeout = 30 * time.Second

const packageSourcePersistHeader = `set -eu
oma_begin='# >>> oma package sources >>>'
oma_end='# <<< oma package sources <<<'
oma_replace_block() {
  oma_file=$1
  mkdir -p "$(dirname "$oma_file")"
  touch "$oma_file"
  awk -v begin="$oma_begin" -v end="$oma_end" '$0 == begin { skip = 1; next } $0 == end { skip = 0; next } !skip' "$oma_file" > "$oma_file.oma-tmp"
  { cat "$oma_file.oma-tmp"; printf '%s\n' "$oma_begin"; printf '%s' "$2"; printf '%s\n' "$oma_end"; } > "$oma_file"
  rm -f "$oma_file.oma-tmp"
}
`

const packageSourcePersistPIP = `if command -v python3 > /dev/null 2>&1; then
  python3 - "${PIP_CONFIG_FILE:-/etc/pip.conf}" %s <<'OMA_PIP'
import configparser, os, sys
path, url = sys.argv[1], sys.argv[2]
parser = configparser.RawConfigParser()
parser.read(path)
if not parser.has_section("global"):
    parser.add_section("global")
parser.set("global", "index-url", url)
os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
with open(path, "w") as handle:
    parser.write(handle)
OMA_PIP
else
  echo "python3 not found; pip index was not persisted" >&2
fi
`

const packageSourcePersistGem = `oma_gemrc=${GEMRC:-$HOME/.gemrc}
oma_gemrc=${oma_gemrc##*:}
if command -v ruby > /dev/null 2>&1; then
  ruby -ryaml -e 'path = ARGV.shift; settings = File.exist?(path) ? (YAML.unsafe_load_file(path) || {}) : {}; settings[:sources] = ARGV; File.write(path, settings.to_yaml)' "$oma_gemrc" %s
else
  echo "ruby not found; gem sources were not persisted" >&2
fi
`

func packageSourcePersistScript(sources config.EnvironmentPackageConfig) string {
	if sources.Empty() {
		return ""
	}
	var script strings.Builder
	script.WriteString(packageSourcePersistHeader)
	if options := sources.NPM.Options(); len(options) > 0 {
		var npmConfig strings.Builder
		for _, option := range options {
			key, registry, _ := strings.Cut(strings.TrimPrefix(option, "--"), "=")
			fmt.Fprintf(&npmConfig, "%s=%s\n", key, strconv.Quote(registry))
		}
		script.WriteString(`oma_npmrc=${NPM_CONFIG_USERCONFIG:-${npm_config_userconfig:-}}
if [ -z "$oma_npmrc" ] && command -v npm > /dev/null 2>&1; then oma_npmrc=$(npm config get userconfig 2> /dev/null || true); fi
[ -n "$oma_npmrc" ] || oma_npmrc=$HOME/.npmrc
`)
		fmt.Fprintf(&script, "oma_replace_block \"$oma_npmrc\" %s\n", shellQuote(npmConfig.String()))
	}
	if sources.PIP.IndexURL != "" {
		fmt.Fprintf(&script, packageSourcePersistPIP, shellQuote(sources.PIP.IndexURL))
	}
	if sources.Go.Proxy != "" {
		fmt.Fprintf(&script, "oma_replace_block /etc/profile.d/zz-oma-package-sources.sh %s\n", shellQuote("export GOPROXY="+shellQuote(sources.Go.Proxy)+"\n"))
		fmt.Fprintf(&script, "oma_replace_block /etc/environment %s\n", shellQuote("GOPROXY="+sources.Go.Proxy+"\n"))
	}
	if len(sources.Cargo.Registries) > 0 {
		var cargoConfig strings.Builder
		for _, name := range slices.Sorted(maps.Keys(sources.Cargo.Registries)) {
			fmt.Fprintf(&cargoConfig, "[registries.%s]\nindex = %s\n", name, strconv.Quote(sources.Cargo.Registries[name]))
		}
		fmt.Fprintf(&script, "oma_replace_block \"${CARGO_HOME:-$HOME/.cargo}/config.toml\" %s\n", shellQuote(cargoConfig.String()))
	}
	if len(sources.Gem.Sources) > 0 {
		quoted := make([]string, len(sources.Gem.Sources))
		for i, source := range sources.Gem.Sources {
			quoted[i] = shellQuote(source)
		}
		fmt.Fprintf(&script, packageSourcePersistGem, strings.Join(quoted, " "))
	}
	return script.String()
}

func (r *Runner) persistPackageSources(ctx context.Context, sandboxID string, preinstalled bool) {
	script := packageSourcePersistScript(r.cfg.EnvironmentPackages)
	if preinstalled || script == "" {
		return
	}
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: "sh -c " + shellQuote(script),
		Timeout: packageSourcePersistTimeout,
	})
	if err == nil && result.ExitCode != 0 {
		err = fmt.Errorf("exit code %d", result.ExitCode)
	}
	if err != nil {
		r.logger.WarnContext(ctx, "persist package sources failed", "sandbox_id", sandboxID, "error", err)
	}
}

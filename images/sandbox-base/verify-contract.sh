#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf 'sandbox contract failed: %s\n' "$*" >&2
  exit 1
}

assert_output_contains() {
  local expected="$1"
  shift
  local output

  output="$("$@" 2>&1)" || fail "command failed: $*"
  grep -Fq "$expected" <<<"$output" || fail "$* output does not contain $expected: $output"
}

assert_output_equals() {
  local expected="$1"
  shift
  local output

  output="$("$@" 2>&1)" || fail "command failed: $*"
  [[ "$output" == "$expected" ]] || fail "$* output is not exactly $expected: $output"
}

assert_command() {
  command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

assert_absent() {
  if command -v "$1" >/dev/null 2>&1; then
    fail "excluded command is installed: $1"
  fi
}

assert_writable_dir() {
  local dir="$1"
  local probe="$dir/.oma-sandbox-contract-$$"

  [[ -d "$dir" ]] || fail "missing directory: $dir"
  : >"$probe" || fail "directory is not writable: $dir"
  rm -f "$probe" || fail "cannot clean write probe: $probe"
}

assert_cargo_builds() {
  local output probe_dir
  probe_dir="$(mktemp -d)"
  mkdir -p "$probe_dir/src"
  printf '%s\n' \
    '[package]' \
    'name = "oma-sandbox-linker-probe"' \
    'version = "0.1.0"' \
    'edition = "2024"' \
    >"$probe_dir/Cargo.toml"
  printf '%s\n' 'fn main() { println!("cargo-linker-ok"); }' >"$probe_dir/src/main.rs"

  output="$(cd "$probe_dir" && cargo run --quiet 2>&1)" || {
    rm -rf "$probe_dir"
    fail "Cargo cannot compile and link a minimal project: $output"
  }
  rm -rf "$probe_dir"
  [[ "$output" == 'cargo-linker-ok' ]] || fail "Cargo probe returned unexpected output: $output"
}

[[ "$(uname -m)" == "x86_64" ]] || fail "platform is not linux/amd64"
[[ "$(id -un)" == "root" ]] || fail "default user is not root"
[[ "$(id -u)" == "0" ]] || fail "root UID is not 0"
[[ "$(id -gn)" == "root" ]] || fail "root primary group is not root"
[[ "$(id -g)" == "0" ]] || fail "root GID is not 0"
[[ "$HOME" == "/root" ]] || fail "HOME is not /root"
[[ "$PWD" == "/home/user" ]] || fail "workdir is not /home/user"
[[ "$(stat -c '%U:%G' /root/.claude)" == "root:root" ]] || fail "/root/.claude ownership is invalid"
[[ "$(stat -c '%a' /root/.claude)" == "700" ]] || fail "/root/.claude mode is invalid"
[[ "$(stat -c '%U:%G' /root/.claude.json)" == "root:root" ]] || fail "/root/.claude.json ownership is invalid"
[[ "$(stat -c '%a' /root/.claude.json)" == "600" ]] || fail "/root/.claude.json mode is invalid"
assert_output_equals '{}' cat /root/.claude.json
assert_writable_dir /root/.claude
sudo -n true || fail "passwordless sudo is unavailable"
user_passwd_record="$(getent passwd user)"
IFS=: read -r user_name _user_password user_uid user_gid _user_gecos user_home user_shell <<<"$user_passwd_record"
[[ "$user_name" == "user" ]] || fail "user account name is not user"
[[ "$user_uid" == "1000" ]] || fail "user account UID is not 1000"
[[ "$user_gid" == "1000" ]] || fail "user account GID is not 1000"
[[ "$user_home" == "/home/user" ]] || fail "user account home is not /home/user"
[[ "$user_shell" == "/bin/bash" ]] || fail "user account shell is not /bin/bash"
assert_output_equals 'claude:x:1001:1001::/home/claude:/bin/bash' getent passwd claude
assert_output_equals 'claude:x:1001:user' getent group claude
assert_output_equals 'user:x:1000:claude' getent group user
[[ "$(stat -c '%U:%G' /home/claude)" == "claude:claude" ]] || fail "/home/claude ownership is invalid"
[[ "$(stat -c '%a' /home/claude)" == "2770" ]] || fail "/home/claude mode is invalid"
[[ "$(stat -c '%a' /home/user)" == "2770" ]] || fail "/home/user mode is invalid"

[[ "$PIP_ROOT_USER_ACTION" == "ignore" ]] || fail "PIP_ROOT_USER_ACTION is not ignore"
[[ "$PIP_CACHE_DIR" == "/home/claude/.cache/pip" ]] || fail "PIP_CACHE_DIR is not Claude-compatible"
[[ "$PIP_CONFIG_FILE" == "/etc/pip.conf" ]] || fail "PIP_CONFIG_FILE is not the shared pip config"
[[ "$PYTHONUNBUFFERED" == "1" ]] || fail "PYTHONUNBUFFERED is not enabled"
[[ "$IS_SANDBOX" == "yes" ]] || fail "IS_SANDBOX is not enabled"
[[ "$NODE_PATH" == "/home/claude/.npm-global/lib/node_modules" ]] || fail "NODE_PATH is not Claude-compatible"
[[ "$GEMRC" == "/home/user/.gemrc" ]] || fail "GEMRC does not use the shared configuration"
[[ "$MAVEN_ARGS" == "-s /home/user/.m2/settings.xml" ]] || fail "MAVEN_ARGS does not use the shared settings"
[[ "$GRADLE_USER_HOME" == "/home/user/.gradle" ]] || fail "GRADLE_USER_HOME does not use the shared configuration"
[[ "$COMPOSER_HOME" == "/home/user/.config/composer" ]] || fail "COMPOSER_HOME does not use the shared configuration"
[[ ":$PATH:" == *":/home/claude/.npm-global/bin:"* ]] || fail "Claude npm-global bin is not on PATH"
[[ ":$PATH:" == *":/home/claude/.local/bin:"* ]] || fail "Claude local bin is not on PATH"

for shared_dir in \
  /home/claude/.cache/pip \
  /home/claude/.claude \
  /home/claude/.local/bin \
  /home/claude/.npm \
  /home/claude/.npm-global/bin \
  /home/claude/.npm-global/lib/node_modules \
  /home/claude/project; do
  assert_writable_dir "$shared_dir"
done

sudo -n -H -u user bash -lc '
  set -e
  [[ "$HOME" == "/home/user" ]]
  cd "$HOME"
  [[ "$PWD" == "/home/user" ]]
  [[ "$PIP_CACHE_DIR" == "/home/claude/.cache/pip" ]]
  [[ "$NODE_PATH" == "/home/claude/.npm-global/lib/node_modules" ]]
  for dir in /home/claude/.cache/pip /home/claude/.claude /home/claude/.local/bin /home/claude/.npm /home/claude/.npm-global/bin /home/claude/.npm-global/lib/node_modules /home/claude/project; do
    probe="$dir/.oma-sandbox-contract-user-$$"
    : >"$probe"
    rm -f "$probe"
  done
  sudo -n true
' || fail "user account contract failed"

sudo -n -H -u claude bash -lc '
  set -e
  [[ "$HOME" == "/home/claude" ]]
  cd "$HOME"
  [[ "$PWD" == "/home/claude" ]]
  [[ "$PIP_CACHE_DIR" == "/home/claude/.cache/pip" ]]
  [[ "$NODE_PATH" == "/home/claude/.npm-global/lib/node_modules" ]]
  [[ -w /home/user ]]
  for dir in .cache/pip .claude .local/bin .npm .npm-global/bin .npm-global/lib/node_modules project; do
    probe="$HOME/$dir/.oma-sandbox-contract-claude-$$"
    : >"$probe"
    rm -f "$probe"
  done
  sudo -n true
' || fail "claude account contract failed"

assert_output_contains 'Python 3.13.14' python --version
assert_output_contains 'pip ' python -m pip --version
assert_output_contains 'uv 0.7.13' uv --version
assert_output_contains 'v24.18.0' node --version
assert_output_contains '1.22.22' yarn --version
assert_output_contains '10.12.1' pnpm --version
assert_output_equals '/home/claude/.npm-global' npm config get prefix
assert_output_equals '/home/claude/.npm' npm config get cache
assert_output_contains 'go1.26.5' go version
assert_output_contains '25.0.3' java -version
assert_output_contains 'Apache Maven 3.9.11' mvn --version
assert_output_contains 'Gradle 9.2.1' gradle --version
assert_output_contains 'PHP 8.5.8' php --version
assert_output_contains 'Composer version 2.8.11' composer --version
assert_output_contains '14.2.0' gcc --version
assert_output_contains '14.2.0' cc --version
assert_output_contains '14.2.0' g++ --version
assert_output_contains '14.2.0' c++ --version
assert_output_contains 'cmake version 3.28.3' cmake --version
assert_output_contains '1.3.14' bun --version
assert_output_contains 'rustc 1.97.0' rustc --version
assert_output_contains 'cargo 1.97.0' cargo --version
assert_output_contains 'rustfmt ' rustfmt --version
assert_output_contains 'rustfmt ' cargo fmt --version
assert_output_contains 'clippy ' cargo clippy --version
assert_output_contains 'rust-analyzer ' rust-analyzer --version
rust_sysroot="$(rustc --print sysroot)"
for llvm_tool in llvm-cov llvm-profdata; do
  [[ -x "$rust_sysroot/lib/rustlib/x86_64-unknown-linux-gnu/bin/$llvm_tool" ]] || fail "missing Rust LLVM tool: $llvm_tool"
done
assert_cargo_builds
assert_output_contains 'ruby 3.4.10' ruby --version
assert_command gem
assert_command bundle
[[ -L /usr/local/bin/claude ]] || fail "claude command is not a compatibility symlink"
assert_output_equals '/opt/claude-code/bin/claude' readlink /usr/local/bin/claude
assert_output_equals '2.1.120 (Claude Code)' claude --version
assert_output_equals '2.1.120 (Claude Code)' /opt/claude-code/bin/claude --version
assert_output_equals '2.1.120 (Claude Code)' sudo -n -H -u user claude --version
assert_output_equals '2.1.120 (Claude Code)' sudo -n -H -u claude /opt/claude-code/bin/claude --version

[[ -x /opt/env-runner/environment-manager ]] || fail "environment-manager payload is not executable"
[[ -L /usr/local/bin/environment-manager ]] || fail "environment-manager command is not a compatibility symlink"
assert_output_equals '/opt/env-runner/environment-manager' readlink /usr/local/bin/environment-manager
assert_output_contains 'environment-runner version 1e71969' /usr/local/bin/environment-manager --version
assert_output_contains 'environment-runner version 1e71969' sudo -n -H -u claude /usr/local/bin/environment-manager --version
manager_task_help="$(/usr/local/bin/environment-manager task-run --help 2>&1)" || fail "environment-manager task-run help failed"
for manager_flag in --session --session-mode --local-testing --claude-agent-version --claude-path; do
  grep -Fq -- "$manager_flag" <<<"$manager_task_help" || fail "environment-manager task-run is missing $manager_flag"
done

for command_name in cc c++ make sqlite3 psql redis-cli git curl wget jq tar zip unzip ssh scp tmux screen rg tree htop sed awk grep vim nano diff patch docker; do
  assert_command "$command_name"
done

grep -Fq 'https://mirrors.tuna.tsinghua.edu.cn/ubuntu/' /etc/apt/sources.list.d/ubuntu.sources || fail "APT does not use TUNA HTTPS"
grep -Fq 'https://pypi.tuna.tsinghua.edu.cn/simple' /etc/pip.conf || fail "pip does not use TUNA"
grep -Fq 'https://registry.npmmirror.com' /etc/npmrc || fail "npm does not use npmmirror"
grep -Fq 'https://goproxy.cn,direct' /etc/environment || fail "Go proxy is not configured"
grep -Fq 'sparse+https://rsproxy.cn/index/' /home/user/.cargo/config.toml || fail "Cargo does not use RSProxy"
grep -Fq 'https://maven.aliyun.com/repository/public' /home/user/.m2/settings.xml || fail "Maven does not use Aliyun"
grep -Fq 'https://mirrors.tuna.tsinghua.edu.cn/rubygems/' /home/user/.gemrc || fail "RubyGems does not use TUNA"
grep -Fq 'https://mirrors.aliyun.com/composer/' /home/user/.config/composer/config.json || fail "Composer does not use Aliyun"
[[ ! -e /etc/apt/apt.conf.d/docker-clean ]] || fail "APT docker-clean defeats BuildKit package caching"
grep -Fq 'APT::Keep-Downloaded-Packages "true"' /etc/apt/apt.conf.d/keep-cache || fail "APT package caching is not enabled"

assert_absent codex
assert_absent dockerd
assert_absent swift
assert_absent swiftly
assert_absent erl
assert_absent erlc
assert_absent elixir
assert_absent iex
assert_absent mise
assert_absent pyenv
assert_absent nvm
assert_absent rustup

[[ ! -S /var/run/docker.sock ]] || fail "Docker socket must not be present"

printf 'sandbox image runtime contract passed\n'

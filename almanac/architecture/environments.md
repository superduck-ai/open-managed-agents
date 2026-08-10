---
title: "Environments"
summary: "The environments system manages sandbox lifecycle, network policies, and skill volume mounting for managed agent sessions using E2B as the runtime provider."
topics: [architecture]
sources:
  - id: runner
    type: file
    path: internal/environments/runner.go
  - id: env-manager
    type: file
    path: internal/environments/environment_manager.go
  - id: e2b-runtime
    type: file
    path: internal/runtime/e2bruntime/runtime.go
---

The environments system provides isolated execution contexts for managed agent sessions. It handles sandbox creation, network policy enforcement, and skill volume mounting through a worker-based runtime that polls for environment work items. The current implementation uses E2B as the sandbox provider, with the environment manager binary bridging between the runtime and Claude Code.

## Environment Runner

The environment runner operates as a pool of worker goroutines that poll the `environment_work` table for pending work items [@runner]. Each worker:

1. Polls for work with a 5-second timeout and worker-specific lease
2. Resolves the sandbox template and network policy from the environment config
3. Prepares managed agent launch configuration if the work is for a session
4. Creates an `environment_sandbox` database record in `creating` state
5. Calls the E2B provider to create the sandbox
6. Writes the environment manager stdin payload and starts the manager
7. Transitions the sandbox record to `running` state
8. Updates the work heartbeat and continues processing

On failure, the runner marks the sandbox as `failed` with an error message and stops the work item, allowing retry logic or manual intervention [@runner].

## Sandbox Resolution

Before creating a sandbox, the runner calls the E2B provider's `Resolve` method to determine the template, network policy, and metadata from the environment configuration [@e2b-runtime]. Resolution produces a `Resolution` struct containing:

- `Template`: The E2B sandbox template (defaults to `claude-code-interpreter`)
- `Metadata`: Tags including `environment_id` and `work_id` for observability
- `Envs`: Environment variables passed to the sandbox
- `Timeout`: Sandbox execution timeout
- `AllowInternetAccess`: Whether unrestricted network access is permitted
- `Network`: Limited network policy with `AllowOut` host whitelist

Network resolution handles three modes:
- **Unrestricted**: Full internet access, no network restrictions
- **Limited**: Only allowlisted hosts plus optional package manager hosts
- **None**: No external network access

For limited network environments, the runner combines static package manager hosts (PyPI, npm, crates.io, etc.) with MCP server hosts extracted from the agent snapshot when `allow_mcp_servers` is enabled [@e2b-runtime].

## Managed Agent Launch

When environment work is for a managed agent session (`type: "session"`), the runner prepares a special launch configuration before sandbox creation [@runner]. This process:

1. Loads the session and its resources from the database
2. Resolves runtime skills and prepares the skill mount volume
3. Creates a local Claude Code session with the model, work directory, and config
4. Generates the environment manager v0 payload with startup context, environment config, and auth tokens
5. Patches session and work metadata with Claude Code session IDs and mount information
6. Returns the stdin payload and shell command for the runner to write

The launch configuration includes MCP server configs, tool permissions, vault IDs, and resource sources mapped to the format expected by `environment-manager` [@env-manager].

## Volume Mounts

The E2B provider supports two volume mounts when creating sandboxes [@e2b-runtime]:

- **User data volume**: Mounted at `/mnt/user-data`, this persistent volume stores workspace data across session restarts
- **Skills volume**: Mounted at `/mnt/skills`, this contains prepared skill archives and manifest from the skills runtime

Skills volumes use the volume name from the `SkillMount` metadata prepared by the runtime resolver. The runner writes this mount information to the work metadata, which the E2B provider reads during sandbox creation [@e2b-runtime].

## Environment Manager

The environment manager binary acts as the bridge between the Go runtime and Claude Code. It runs as a background process inside the sandbox and handles [@env-manager]:

- Extracting skill zip archives from `/mnt/skills` to `/workspace/skills`
- Symlinking Claude Code skill discovery directories to `/workspace/skills`
- Starting the Claude Code agent with the configured model and permissions
- Managing Claude Code lifecycle and restart behavior

The runner writes the environment manager stdin payload to a file in the sandbox, then executes a shell script that starts the manager in the background. The script validates binary versions, sets environment variables to disable non-essential traffic, and launches the manager with `--session-mode resume-cached` [@env-manager].

## Worker Concurrency

The environment runner supports configurable worker concurrency via the `ENVIRONMENT_RUNNER_CONCURRENCY` configuration. Each worker operates independently with its own worker ID, and the database's lease mechanism ensures that no work item is processed concurrently by multiple workers [@runner].

Workers use a 500ms polling interval when no work is available, switching to immediate re-polling when work is found to maximize throughput during active periods.

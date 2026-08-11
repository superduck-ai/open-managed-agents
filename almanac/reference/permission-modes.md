---
title: "Permission Modes"
summary: "Claude Code permission modes control how often the system prompts for approval before executing tools, with modes ranging from default (ask everything) to auto (classifier-based approval) to bypassPermissions (no prompts)."
topics: [reference, permissions, claude-code]
sources:
  - id: permission-modes-doc
    type: file
    path: docs/design/be/ccrv2/claude_code-permission-modes.md
  - id: permission-policies
    type: file
    path: docs/design/be/permission-policies.md
  - id: tool-permissions
    type: file
    path: internal/codesessions/tool_permissions.go
---

# Permission Modes

Claude Code permission modes control how frequently the system prompts for user approval before executing tools. The mode selected shapes the flow of a session: tighter modes require review of each action, while looser modes allow Claude to work in longer uninterrupted stretches[@permission-modes-doc].

## Available Modes

| Mode | What Runs Without Asking | Best For |
|------|--------------------------|----------|
| `default` | Reads only | Getting started, sensitive work |
| `acceptEdits` | Reads, file edits, common filesystem commands | Iterating on code you're reviewing |
| `plan` | Reads only | Exploring a codebase before changing it |
| `auto` | Everything with classifier-based safety checks | Long tasks, reducing prompt fatigue |
| `dontAsk` | Only pre-approved tools | Locked-down CI and scripts |
| `bypassPermissions` | Everything | Isolated containers and VMs only |

In every mode except `bypassPermissions`, writes to protected paths (`.git`, `.claude/`, config files) are never auto-approved[@permission-modes-doc].

## Default Mode

Default mode requires approval for all actions except read operations. Every tool call prompts the user before executing. This is the safest mode for sensitive work and for users new to Claude Code[@permission-modes-doc].

**Auto-approved**: File reads, read-only HTTP requests

**Prompts**: File edits, shell commands, network writes, tool executions

## AcceptEdits Mode

`acceptEdits` mode auto-approves file edits and common filesystem commands while still prompting for shell commands and network operations[@permission-modes-doc].

**Auto-approved**: File edits, `mkdir`, `touch`, `rm`, `mv`, `cp`, `sed`, and their PowerShell equivalents

**Prompts**: Arbitrary bash commands, network requests, operations outside working directory

## Plan Mode

Plan mode lets Claude research and propose changes without making them. Claude reads files and runs read-only commands to explore, then presents a plan for approval rather than executing directly[@permission-modes-doc].

**Behavior**: Claude works read-only until a plan is approved. Upon approval, the session switches to the selected execution mode (default, acceptEdits, or auto).

## Auto Mode

Auto mode lets Claude execute without routine prompts using a classifier model to evaluate each action for safety[@permission-modes-doc].

**Auto-approved** (default): Local file operations, installing dependencies from lockfiles, reading `.env` and sending credentials to matching APIs, read-only HTTP requests, pushing to non-main branches

**Blocked by default**: Downloading and executing code, sending sensitive data externally, production deploys, mass deletions, IAM/repo permission changes, `git reset --hard`, `terraform destroy`, writes to secret managers, merging unapproved PRs, launching autonomous agent loops, and more

**Classifier behavior**: The classifier checks each action against rules and conversation context. Actions from allow rules execute immediately. Actions matching deny rules prompt. Other actions go to the classifier for evaluation[@permission-modes-doc].

**Fallback**: If the classifier blocks an action 3 times consecutively or 20 times total, auto mode pauses and resumes prompting.

## DontAsk Mode

`dontAsk` mode auto-denies every tool call that would otherwise prompt. Only actions matching allow rules and read-only commands can execute; explicit ask rules are denied rather than prompting[@permission-modes-doc].

**Use case**: CI pipelines and restricted environments where you pre-define exactly what Claude may do.

## BypassPermissions Mode

`bypassPermissions` mode disables all permission prompts and safety checks so tool calls execute immediately[@permission-modes-doc]. This includes writes to protected paths.

**Restrictions**: Only available with startup flags (`--permission-mode bypassPermissions` or `--dangerously-skip-permissions`). Refuses to start as root/sudo on Linux/macOS. Cloud sessions ignore `defaultMode: "bypassPermissions"` from settings.

**Use case**: Isolated environments like containers and VMs where Claude Code cannot damage the host system.

## Switching Modes

Modes can be changed mid-session through:

- **CLI**: Press `Shift+Tab` to cycle through modes
- **VS Code/Desktop**: Use the mode selector in the UI
- **Startup flags**: Pass `--permission-mode <mode>` when starting
- **Settings**: Set `defaultMode` in user or project settings files

The current mode appears in the status bar and affects subsequent tool calls immediately[@permission-modes-doc].

## Managed Agents Integration

In managed agents, permission modes translate to permission policies configured on agent toolsets and MCP toolsets[@permission-policies]. The bridge between Claude Code modes and managed agent policies happens in the `tool_permissions.go` module[@tool-permissions].

**Translation**:
- `default` mode → Agent tools use `always_ask`, MCP tools use `always_ask`
- `acceptEdits` mode → Agent tools use `always_allow` for edits, `always_ask` for bash/commands
- `auto` mode → Agent tools use `always_allow`, classifier blocks dangerous operations
- `bypassPermissions` mode → All tools use `always_allow`

The `resolveToolPermissionFromAgentSnapshot` function reads the agent snapshot's tool configurations and resolves each tool call to a permission decision (allow/ask/deny)[@tool-permissions].

## Tool Permission Resolution

When Claude Code requests a tool execution, the system resolves the permission by[@tool-permissions]:

1. Parse the tool name to determine kind (agent_toolset, MCP, or custom)
2. Look up the tool in the agent snapshot's tool configurations
3. Find the matching tool config or fall back to default_config
4. Extract the permission_policy type (`always_allow` or `always_ask`)
5. Check if the tool is disabled (enabled=false)
6. Return resolved permission: allow, ask, or deny

For MCP tools, the resolution includes checking `mcp_server_name` to match the toolset configuration[@tool-permissions].

## Permission Response Flow

When a tool requires user approval (resolved permission is "ask"), the system[@tool-permissions]:

1. Publishes a tool permission request event
2. Creates a `session.status_idle` event with `stop_reason.type="requires_action"`
3. Lists blocking event IDs in `stop_reason.event_ids`
4. Waits for `user.tool_confirmation` event with allow/deny result
5. Sends control response to worker with approve/deny behavior
6. Worker proceeds or skips the tool call accordingly

The tool confirmation payload includes the event ID, result ("allow" or "deny"), and an optional `deny_message` explaining the rejection[@tool-permissions].

## Protected Paths

Writes to a small set of protected paths are never auto-approved in any mode except `bypassPermissions`[@permission-modes-doc]:

**Protected directories**: `.git`, `.config/git`, `.vscode`, `.idea`, `.husky`, `.cargo`, `.devcontainer`, `.yarn`, `.mvn`, `.claude/` (except `.claude/worktrees`)

**Protected files**: `.gitconfig`, `.gitmodules`, shell config files (`.bashrc`, `.zshrc`, etc.), package manager configs (`.npmrc`, `.yarnrc`, etc.), `.pre-commit-config.yaml`, `.devcontainer.json`, `.ripgreprc`, `.mcp.json`, `.claude.json`

Allow rules in settings files do not pre-approve protected path writes—the safety check runs before rule evaluation.

## Auto Mode Requirements

Auto mode requires all of the following[@permission-modes-doc]:

- **Plan**: Available on all plans
- **Owner**: Team/Enterprise owners must enable in admin settings
- **Model**: Claude Opus 4.6+ or Sonnet 4.6+ on Anthropic API; Sonnet 5, Opus 4.7+, Opus 4.8+ on other providers
- **Provider**: Available by default on Anthropic API; requires `CLAUDE_CODE_ENABLE_AUTO_MODE=1` on Bedrock, Vertex AI, Foundry, and Claude apps gateway

If auto mode is unavailable despite meeting requirements, the setting may be in `.claude/settings.json` instead of user settings (cloud sessions ignore repository-level auto mode).

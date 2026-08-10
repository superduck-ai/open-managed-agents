---
title: "Agents"
summary: "Agent configuration, versioning, and snapshots for managed agent execution with skills, tools, and multi-agent coordination."
topics: [architecture]
sources:
  - id: agents-handler
    type: file
    path: internal/agents/handler.go
  - id: agents-db
    type: file
    path: internal/db/agents.go
  - id: agents-snapshot
    type: file
    path: internal/agentsnapshot/
---

Agents represent reusable AI configurations that specify model selection, system prompts, tools, skills, and multi-agent coordination patterns. Unlike one-off API requests, agents encapsulate the complete configuration needed for managed agent sessions, enabling consistent behavior across multiple executions.

## Agent Structure

An agent consists of several configurable components:

- **Name**: Human-readable identifier
- **Description**: Optional detailed description
- **System**: System prompt or instructions for the agent
- **Model**: Anthropic model ID and optional speed setting (`standard` or `fast`)
- **MCP servers**: List of Model Context Protocol servers for tool integration
- **Skills**: Built-in or custom skills for extended capabilities
- **Tools**: Tool configuration including Anthropic toolsets and custom tools
- **Multiagent**: Optional coordinator configuration for multi-agent setups
- **Metadata**: Key-value pairs for organization and tagging [@agents-handler]

All fields except name and model are optional, allowing agents to range from simple model-plus-prompt configurations to complex multi-tool, multi-skill setups.

## Model Configuration

The model field specifies which Anthropic model the agent uses:

- **String form**: Just the model ID like `claude-opus-4-6` (defaults to `standard` speed)
- **Object form**: Model ID plus optional `speed` setting

Speed options include:
- **standard**: Default full-quality model
- **fast**: Faster variant with potential quality tradeoffs [@agents-handler]

Model configuration is immutable per version but can be changed when creating a new agent version.

## Skills

Agents reference both built-in Anthropic skills and custom skills:

**Built-in skills:**
- Specified with `type: "anthropic"`, `skill_id`, and optional `version`
- Examples include `xlsx` for Excel file operations
- Version defaults to `latest` if not specified
- Up to 20 skills can be referenced

**Custom skills:**
- Specified with `type: "custom"`, `skill_id`, and optional `version`
- Referenced by skill ID that resolves to a skill definition [@agents-handler]

Skills are resolved at runtime and mounted as Docker volumes in execution environments. The agent configuration stores skill references, and the actual skill manifests and volumes are resolved during session creation.

## Tools

Agents configure three categories of tools:

**Agent toolsets:**
- `type: "agent_toolset_20260401"`
- Includes bash, edit, read, write, glob, grep, web_fetch, and web_search tools
- Each tool can be enabled/disabled with `permission_policy` (`always_allow` or `always_ask`)
- Default configuration applies to all tools, with per-tool overrides in `configs`

**MCP toolsets:**
- `type: "mcp_toolset"` linked to an MCP server by name
- Inherits tools from the connected MCP server
- Per-tool permission configuration similar to agent toolsets
- All configured MCP servers must be referenced by an MCP toolset

**Custom tools:**
- `type: "custom"` with name, description, and JSON schema
- Name must match `^[A-Za-z0-9_-]{1,128}$`
- Description limited to 1024 characters
- Input schema must be a JSON Schema object with `type: "object"` [@agents-handler]

Total tool count (sum across all toolsets and custom tools) cannot exceed 128.

## Multi-Agent Coordination

Agents can be configured as coordinators for multi-agent setups:

- **Type**: Must be `"coordinator"`
- **Agents**: Array of 1-20 agent references
- **Self-reference**: Special entry with `type: "self"` representing the coordinator itself
- **Agent references**: Can be agent ID string or object with `id`, `type`, and optional `version`

Multi-agent configuration allows an agent to delegate tasks to other agents in its workspace, enabling hierarchical or specialist agent setups [@agents-handler].

## Versioning

Agents support optimistic locking via version numbers:

- **Version**: Incrementing integer starting at 1
- **Update requires**: Expected version in request to prevent conflicts
- **Version conflict**: Returns 409 error if current version doesn't match expected
- **Version history**: Can be retrieved via `/agents/{agent_id}/versions` endpoint [@agents-db]

Each update creates a new version record that captures the complete agent configuration at that point in time. Clients can retrieve specific historical versions using the `version` query parameter.

## Agent Snapshots

When an agent is used in deployments or sessions, the system captures an agent snapshot:

- **Immutable capture**: All agent configuration at a point in time
- **Resolution**: Skills and tools are resolved to concrete configurations
- **Format**: JSON structure containing model, system prompt, skills, tools, MCP servers
- **Usage**: Stored in deployments and sessions to ensure consistent behavior

The agent snapshot is created by the agent snapshot package, which serializes the agent's complete configuration including skills, tools, and MCP server references [@agents-snapshot].

## Skill Prewarming

When an agent is created or updated with new skills, the system enqueues an async job to prewarm skill volumes:

- **Trigger**: Agent creation or skill changes during update
- **Purpose**: Pre-build Docker volumes for skills to reduce session startup latency
- **Async processing**: Occurs in background without blocking agent operations
- **Workspace-scoped**: Prewarming occurs per workspace to avoid conflicts [@agents-handler]

The skill prewarm system ensures that when a session using the agent starts, the required skill volumes are already cached and available.

## Lifecycle States

Agents exist in one of three states:

- **Active**: Can be used in deployments and sessions
- **Archived**: Immutable, cannot be used in new deployments or sessions
- **Deleted**: Soft-deleted, excluded from most queries [@agents-db]

Archived agents retain their configuration and versions but cannot be referenced by new deployments. Existing deployments continue to work with their captured agent snapshot.

## API Endpoints

The agents API requires `beta=true` query parameter and provides:

- `POST /agents`: Create a new agent
- `GET /agents`: List agents with pagination and filtering
- `GET /agents/{agent_id}`: Retrieve current version or specific version via `version` query parameter
- `POST /agents/{agent_id}`: Update an agent (requires version for optimistic locking)
- `POST /agents/{agent_id}/archive`: Archive an agent
- `GET /agents/{agent_id}/versions`: List agent version history

Agents support filtering by creation time ranges and can include archived entries via `include_archived=true` query parameter [@agents-handler].

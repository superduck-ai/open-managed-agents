---
title: "Agent Snapshot"
summary: "Immutable agent configuration captured at session creation time, defining the agent's tools, skills, and behavior for that session."
topics: [reference, agents]
sources:
  - id: agentsnapshot-snapshot
    type: file
    path: internal/agentsnapshot/snapshot.go
  - id: agents-handler
    type: file
    path: internal/agents/handler.go
  - id: db-sessions
    type: file
    path: internal/db/sessions.go
---

# Agent Snapshot

An agent snapshot is a complete JSON representation of an agent's configuration captured at the moment a session is created. Snapshots are immutable—if a session references agent version 3, that session will continue using version 3's snapshot even if the agent is later updated to version 4. This ensures session behavior remains consistent throughout its lifetime.

## Snapshot Structure

The snapshot is built from the agent's database record by the `FromAgent` function in `internal/agentsnapshot/snapshot.go`[@agentsnapshot-snapshot]. The JSON structure includes:

```json
{
  "id": "agent_1234",
  "name": "My Agent",
  "description": "An agent for processing orders",
  "type": "agent",
  "version": 1,
  "system": "You are a helpful assistant...",
  "model": {"id": "claude-opus-4-6", "speed": "standard"},
  "mcp_servers": [{"name": "github", "type": "url", "url": "https://..."}],
  "skills": [{"type": "anthropic", "skill_id": "xlsx", "version": "1"}],
  "tools": [{"type": "agent_toolset_20260401", "default_config": {...}}],
  "multiagent": {"type": "coordinator", "agents": [...]},
  "metadata": {"department": "support"}
}
```

The `id` field is the agent's external ID, `version` is the agent's current version number at session creation time, and all other fields come from the agent's configuration[@agentsnapshot-snapshot].

## Storage and Retrieval

Agent snapshots are stored as JSONB in the `sessions` table's `agent_snapshot` column[@db-sessions]. When a session is created, the system copies the current agent state into this field. The snapshot persists unchanged for the session's lifetime, even as the underlying agent evolves.

## Skills Reference Detection

The `agentsnapshot` package provides utilities for working with skill references in snapshots[@agentsnapshot-snapshot]:

- `SnapshotHasSkills` checks if a snapshot contains any skill entries
- `SnapshotSkillsEqual` compares two snapshots' skill arrays for equality
- `SkillsRawHasEntries` validates that a skills array has at least one element

These functions are used in the agents handler to determine whether skill prewarming should be triggered when an agent is created or updated[@agents-handler].

## Snapshot Versioning

Each agent maintains a `CurrentVersion` counter that increments on every update. Sessions store both the external ID and the version number they reference. This allows the system to:

1. Retrieve the exact configuration a session was created with
2. Support historical queries and debugging
3. Enable A/B testing across agent versions

## Multiagent Coordinator Snapshots

When an agent is configured as a multiagent coordinator, its snapshot includes a `multiagent` field containing the coordinator configuration[@agents-handler]. This specifies:

- `type`: Always "coordinator"
- `agents`: An array of agent references, each with `id`, `type`, and `version`

The coordinator snapshot may include a special "self" reference that resolves to the coordinator agent itself, enabling recursive delegation patterns.

## RawJSON Handling

The snapshot uses `RawJSONValue` to safely decode JSON fields, returning empty defaults (`[]` or `{}`) for null or missing values rather than failing[@agentsnapshot-snapshot]. This graceful handling ensures backward compatibility as new optional fields are added to the agent schema.

---
title: "Managed Agents"
summary: "Managed agents are configurable AI agents that combine model selection, tool permissions, MCP servers, and skills into persistent, versioned entities capable of running interactive sessions."
topics: [managed-agents, agents, sessions]
sources:
  - id: readme-cn
    type: file
    path: README.cn.md
  - id: db-schema
    type: file
    path: internal/db/migrations/00001_init.sql
  - id: sessions-handler
    type: file
    path: internal/sessions/handler.go
  - id: sessions-service
    type: file
    path: internal/sessions/service.go
  - id: api-server
    type: file
    path: internal/api/server.go
---

Managed agents are the core orchestration abstraction in Open Managed Agents, combining model configuration, tool permissions, MCP server connections, and skill references into versioned, persistent entities. Agents serve as templates for sessions, which are the actual execution contexts where the agent processes events and produces responses.

## Agent Structure and Versioning

An agent consists of a name, description, system prompt, model configuration, MCP servers, tools (with permission policies), skills, and metadata [@readme-cn]. Every agent maintains a `current_version` that increments on updates, and the full agent state is snapshotted into `agent_versions` for reproducibility [@db-schema]. When a session is created, it captures the agent snapshot at that point in time—the session continues using that snapshot even if the agent definition later changes.

The agent's `model` field specifies the Claude model to use (e.g., `claude-sonnet-4-6`), while `mcp_servers` defines external Model Context Protocol servers that provide additional tools [@readme-cn]. The `tools` array configures both the built-in agent toolset (Bash, Edit, Read, Write, etc.) and MCP toolsets with permission policies that control whether tools run automatically or require approval.

## Sessions and Threads

Sessions represent individual conversations or execution runs of an agent. When creating a session via `POST /v1/sessions`, the system captures the agent's current state as `agent_snapshot` and links the session to an environment for code execution capabilities [@sessions-handler]. Sessions can transition through states including `idle`, `running`, `rescheduling`, and `terminated` [@db-schema].

Sessions support threading through `session_threads`, allowing a single session to branch into parallel conversation contexts. Each thread maintains its own event sequence and status while sharing the same session-level resources and agent snapshot [@db-schema]. The primary thread exists implicitly for sessions that don't use branching.

## Event-Driven Execution

Communication with sessions happens through events sent via `POST /v1/sessions/{session_id}/events`. Events include user messages, tool results, confirmations, and interruptions [@sessions-service]. The service normalizes input events, appends them to the session's event log, and broadcasts them to connected listeners including the Claude Code runtime worker.

Session events are stored in `session_events` with sequence-based ordering for reliable replay and synchronization [@db-schema]. Events can be paginated by cursor, filtered by type and timestamp, and retrieved at either the session level (primary thread events) or for specific threads.

## Resources and Environment Integration

Sessions can attach resources such as files, GitHub repositories, and memory stores. These are registered in `session_resources` and provide the agent with access to external data and context [@db-schema]. Memory stores, in particular, allow agents to read and write persistent file-like memories that persist across sessions within a workspace.

The environment integration links sessions to sandbox execution environments (E2B or compatible). When a session is created, the system generates an `environment_work` record that the environment runner polls to start sandboxes, execute code, and stream results back through the code session bridge [@readme-cn].

## Deployments and Scheduled Execution

Deployments allow agents to be run on a schedule or triggered manually. A deployment captures an agent snapshot similar to a session, adds a schedule configuration, and maintains state (`active` or `paused`) for controlling execution [@db-schema]. Manual or scheduled runs create `deployment_runs` that either spawn a new session or record an error if the run fails.

## API Surface

The `/v1/agents` endpoint provides CRUD operations for agents, including search via `POST /v1/agents:search` [@api-server]. Agent creation and updates trigger skill prewarming jobs in the background to optimize cold start time. Sessions are managed under `/v1/sessions` with operations for creating, retrieving, listing, archiving, and deleting sessions, plus endpoints for sending events and managing resources and threads.

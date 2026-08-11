---
title: "Deployments"
summary: "Deployment configuration and execution for managed agents, including manual runs, scheduled execution, and deployment run tracking."
topics: [architecture]
sources:
  - id: deployments-handler
    type: file
    path: internal/deployments/
  - id: deployments-db
    type: file
    path: internal/db/deployments.go
---

Deployments provide a way to pre-configure managed agents with fixed settings, resources, and execution schedules. A deployment captures an agent configuration along with its environment, initial events, resources (files, memory stores, GitHub repositories), vault references, and optional cron schedule. Deployments can be triggered manually or run automatically based on their schedule, with each execution recorded as a deployment run.

## Deployment Model

A deployment binds together several components that would otherwise be specified at session creation time:

- **Agent reference**: Points to a specific agent and version (or defaults to current version)
- **Environment**: Specifies the execution environment (E2B sandbox)
- **Initial events**: Pre-configured session startup events like user messages or outcome definitions
- **Resources**: Files, memory stores, and GitHub repositories mounted into the session
- **Vault IDs**: References to vaults containing credentials needed for the session
- **Schedule**: Optional cron configuration for automatic recurring runs

The deployment stores an `agent_snapshot` containing the agent's full configuration (skills, tools, MCP servers, model settings) at creation time, ensuring the deployment remains stable even if the underlying agent is later updated [@deployments-handler].

## Lifecycle States

Deployments exist in one of two primary states:

- **active**: Available for manual and scheduled execution
- **paused**: Not executing; manual and scheduled runs are blocked until unpaused

When a deployment is archived, it becomes immutable and can no longer be updated or executed. Archived deployments are retained for historical record but are excluded from default list queries unless explicitly requested [@deployments-db].

## Scheduling

Deployments support optional cron-based scheduling for automatic recurring execution. The schedule configuration specifies:

- **expression**: A 5-field POSIX cron expression (minute, hour, day-of-month, month, day-of-week)
- **timezone**: IANA timezone identifier for schedule evaluation

The cron expression supports standard syntax including wildcards (`*`), ranges (`1-5`), lists (`1,3,5`), and steps (`*/15`). Day-of-month and day-of-week constraints are both evaluated, with matching occurring when either condition is met [@deployments-handler].

The system calculates upcoming run times during API responses, returning the next several execution timestamps based on the current schedule. The `last_run_at` field tracks the most recent successful execution, updating after each completed deployment run.

## Deployment Runs

Each execution of a deployment creates a deployment run, which records:

- **Trigger type**: `manual` or `schedule`
- **Trigger context**: Additional details about the execution trigger
- **Session ID**: The created session ID (or null if session creation failed)
- **Error**: Error details if the run failed to create a session
- **Agent snapshot**: The agent configuration at time of execution

When a deployment run executes successfully, it creates a complete session with threads, work items, and initial events. The session inherits the deployment's environment, agent configuration, resources, and vault references. Webhook events are emitted for session creation and status transitions [@deployments-handler].

## Manual Execution

Manual deployment runs can be triggered via the `POST /deployments/{deployment_id}/run` endpoint. Before creating the run, the system validates that:

- The deployment is not archived
- The deployment status is `active` (not paused)
- The referenced agent exists and is not archived
- The referenced environment exists and is not archived
- All referenced vaults exist and are not archived
- All referenced memory stores exist and are not archived
- All referenced files exist

If any reference is invalid (not found or archived), the run still creates a deployment run record but includes an error field explaining the failure reason. The deployment run record is created in these cases to provide auditability of execution attempts [@deployments-handler].

## Resource Mounting

Deployments support three resource types that get mounted into created sessions:

- **file**: References a stored file by ID with an optional custom mount path
- **github_repository**: Clones a GitHub repository with optional branch/commit checkout and authorization token
- **memory_store**: Attaches a memory store with read-only or read-write access

Resources are resolved at deployment creation time. File and memory store references must exist and be active. GitHub repository URLs must be valid HTTPS URLs to public repositories or include credentials for private access [@deployments-handler].

For GitHub repositories, the `checkout` configuration specifies either a branch name or commit SHA. Authorization tokens can be provided for private repository access and are stored separately from the public resource configuration for security.

## Skill Prewarming

When a deployment is created or updated with a new agent snapshot, the system enqueues an async job to prewarm skill volumes. This prewarming ensures that when the deployment executes, the required skill Docker volumes are already available and cached, reducing startup latency for sessions created from the deployment [@deployments-handler].

The skill prewarm trigger occurs on:
- Deployment creation
- Deployment updates where the agent snapshot's skills changed

## API Endpoints

The deployments API provides the following operations:

- `POST /deployments`: Create a new deployment
- `GET /deployments`: List deployments with pagination and filtering
- `GET /deployments/{deployment_id}`: Retrieve a specific deployment
- `POST /deployments/{deployment_id}`: Update a deployment
- `POST /deployments/{deployment_id}/archive`: Archive a deployment
- `POST /deployments/{deployment_id}/pause`: Pause a deployment
- `POST /deployments/{deployment_id}/unpause`: Unpause a deployment
- `POST /deployments/{deployment_id}/run`: Manually trigger a deployment run

All endpoints require `beta=true` query parameter and workspace-scoped authentication [@deployments-handler].

## Runs API

Deployment runs are tracked through a separate endpoint hierarchy:

- `GET /deployment_runs`: List deployment runs with pagination and filtering
- `GET /deployment_runs/{deployment_run_id}`: Retrieve a specific deployment run

Runs can be filtered by deployment ID, trigger type, error status, and creation time ranges. The runs API uses workspace-scoped authentication and requires the `beta=true` query parameter [@deployments-handler].

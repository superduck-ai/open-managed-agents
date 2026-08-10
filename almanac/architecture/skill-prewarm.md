---
title: "Skill Prewarm"
summary: "Skill prewarm is an asynchronous optimization that proactively prepares skill volumes before session launches, reducing cold start latency."
topics: [architecture]
sources:
  - id: skills-runtime-design
    type: file
    path: docs/design/be/managed-agent-skills-runtime.md
  - id: prewarm-worker
    type: file
    path: internal/skillprewarm/worker.go
  - id: prewarm-enqueuer
    type: file
    path: internal/skillprewarm/enqueuer.go
---

Skill prewarm is an optional best-effort optimization that prepares E2B skill volumes before managed agent sessions actually need them. By proactively resolving skill references and creating volumes in the background, prewarm reduces the cold start latency when sessions launch. However, prewarm is not a correctness boundary—if prewarm jobs fail or are delayed, session launch paths still perform lazy skill preparation on-demand.

## Enqueue Triggers

The `Enqueuer` writes prewarm jobs to the outbox table in response to specific events across the system [@prewarm-enqueuer]. Jobs are enqueued with short timeouts (typically 3 seconds) and failures only log errors without blocking the primary request:

- **Agent creation**: When an agent is created with non-empty `skills`, a snapshot job is enqueued
- **Agent update**: When an agent's `skills` field changes to a non-empty value, a snapshot job is enqueued
- **Deployment creation**: When a deployment is created with a snapshot containing skills, a snapshot job is enqueued
- **Deployment update**: When a deployment's agent snapshot skills change to non-empty, a snapshot job is enqueued
- **Custom skill version creation**: A fanout job is enqueued to find and re-prewarm affected agents and deployments [@skills-runtime-design]

Enqueue handlers only enqueue when `skills` is a non-empty array. Missing `skills`, `null`, or empty arrays are treated as "no skills" and do not trigger prewarm [@prewarm-enqueuer].

## Job Types

Prewarm jobs come in two kinds via the `kind` payload field:

### Snapshot Jobs

Snapshot jobs contain the full agent snapshot that was created or updated. The worker resolves the skills from this snapshot using the same `RuntimeResolver` as the session launch path, then calls `PrepareSkillMount` to create or reuse the corresponding E2B volume [@prewarm-worker].

### Fanout Jobs

Fanout jobs are triggered when a new custom skill version is published. They scan for agents and deployments that reference the affected skill at `latest` version, generating snapshot jobs for each match. The scan is paginated and continues until no more affected resources are found, ensuring broad coverage without overwhelming the database [@skills-runtime-design].

## Worker Execution

The prewarm worker runs as a background goroutine that polls for available jobs using a database lease mechanism [@prewarm-worker]. Worker execution follows this pattern:

1. Poll for up to 5 jobs with a 1-minute lease
2. Process each job by kind:
   - For snapshot jobs, resolve skills and prepare mount
   - For fanout jobs, scan affected resources and enqueue follow-up snapshot jobs
3. On success, mark job complete
4. On failure, mark job for retry with exponential backoff
5. After 5 attempts, mark job failed

Workers use lease validation to prevent concurrent processing—only the worker holding the active lease can transition job state [@prewarm-worker].

## Relationship to Session Launch

Skill prewarm operates as a cache warm-up layer in front of the session launch path. When a managed agent session starts, the environment runner still performs full skill resolution and mount preparation regardless of prewarm status [@skills-runtime-design].

The benefit appears when the runner's `PrepareSkillMount` call hits the volume cache: instead of loading archives from object storage and writing them, it finds the existing volume with a matching `.ready` marker and returns immediately. Prewarm failures don't affect session launches because the launch path remains the ultimate correctness boundary.

## Isolation and Failure Handling

Prewarm is deliberately isolated from core request paths. Enqueue failures don't block agent or deployment creation, and worker failures don't prevent sessions from starting. The worker records `last_error` and `last_error_at` in the job payload for diagnostics, but these don't surface to users [@prewarm-worker].

The worker must check lease validity before transitioning job state. If an update affects zero rows, the worker assumes another worker has taken over the lease or the job has already reached a terminal state, and it backs off without retrying [@prewarm-worker].

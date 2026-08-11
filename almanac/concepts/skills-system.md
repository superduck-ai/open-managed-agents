---
title: "Skills System"
summary: "The skills system provides reusable, file-based domain knowledge that can be attached to agents and mounted into Claude Code sandbox environments for specialized task execution."
topics: [skills, agents, runtime]
sources:
  - id: readme-cn
    type: file
    path: README.cn.md
  - id: skills-handler
    type: file
    path: internal/skills/handler.go
  - id: skills-runtime
    type: file
    path: docs/design/be/managed-agent-skills-runtime.md
  - id: builtin-seed
    type: file
    path: docs/design/be/skills-builtin-seed-and-storage.md
  - id: db-schema
    type: file
    path: internal/db/migrations/00001_init.sql
---

The skills system enables agents to incorporate reusable, file-based domain knowledge and capabilities. Skills are packaged as ZIP archives containing a `SKILL.md` manifest and supporting files, stored in object storage, and referenced by agents through versioned pointers. At runtime, skills are mounted into Claude Code sandbox environments for discovery and execution.

## Skill Sources and Types

Skills come in two varieties: custom skills uploaded by users and built-in skills seeded by administrators [@readme-cn]. Custom skills are created via multipart upload to `POST /v1/skills`, which extracts the package metadata and stores the archive in object storage under `workspaces/{workspace_uuid}/skills/{skill_uuid}/versions/{version}/{directory}.zip` [@skills-handler]. Built-in skills are seeded into `builtin_skills` and `builtin_skill_versions` tables through administrative seed functions and serve as shared capabilities across the platform [@builtin-seed].

Both types expose the same API surface—a skill has an ID, display title, latest version, and source indicator (`custom` or `anthropic`) [@skills-handler]. Version lists and content retrieval work identically regardless of source, allowing agents to reference skills without distinguishing their origin.

## Versioning and Storage

Each skill can have multiple versions, with the `latest` keyword resolving to the most recent active version [@skills-handler]. Version identifiers are Unix timestamps, ensuring chronological ordering. When a new version is created, the ZIP archive is uploaded to object storage with a distinct key, while the skill record's `latest_version` pointer updates atomically.

The database stores metadata including name, description, directory, size, SHA-256 checksum, and object storage location for each version [@db-schema]. This separation allows the runtime to verify archive integrity without downloading the full ZIP, and enables deduplication when multiple agents reference the same skill version.

## Agent Integration

Agents reference skills through the `skills` array in their configuration, specifying `{type, skill_id, version}` tuples. The `type` field indicates whether the skill is `custom` or `anthropic` (built-in), while `skill_id` is the external ID and `version` can be a literal version string or `latest` [@skills-runtime]. These references are snapshotted when an agent version is created, preserving reproducibility—sessions continue using the skill versions present at creation time even if the agent or skill definitions later change.

## Runtime Mounting

When a session starts with an agent that has skills, the environment runner resolves the skill references from the agent snapshot [@skills-runtime]. For built-in skills, it queries the `builtin_skill_versions` table; for custom skills, it joins `skills` with `skill_versions` scoped to the workspace. The runner constructs a deterministic `manifest.json` listing each skill's source, resolved version, directory, filename, SHA-256, and size.

The manifest hash becomes the E2B volume name. If a volume with that hash already exists and contains a matching `.ready` marker, the runner reuses it without re-reading archives from object storage [@skills-runtime]. On a cache miss, the runner loads each ZIP archive sequentially, validates integrity, extracts to verify single top-level directory structure, and writes the archives plus manifest to the volume.

The sandbox receives the volume at `/mnt/skills`, where `environment-manager` extracts the archives to `/workspace/skills` and symlinks Claude Code's skill discovery directories to make them visible to the running process [@skills-runtime]. This design ensures Claude Code sees a consistent filesystem view without needing database or API access.

## Prewarming and Optimization

To reduce cold start latency, skill prewarm jobs enqueue asynchronously when agents are created or updated, when deployments are configured, or when new skill versions are published [@skills-runtime]. The worker resolves skill references from the agent snapshot and prepares the corresponding E2B volumes, so that by the time a session starts, the skill archives are already cached and mounted.

Prewarming is best-effort—if the job queue is backed up or a worker fails, the session startup path still performs lazy skill mounting as a correctness boundary [@skills-runtime]. The jobs table tracks attempts, last errors, and worker leases to ensure reliable processing without duplicate work.

## Skill Package Format

Skill archives must be ZIP files with a single top-level directory containing a `SKILL.md` file [@skills-handler]. The `SKILL.md` defines the skill's name and description, which are extracted during upload. Supporting files (documentation, code examples, templates) can be included anywhere in the directory structure and are mounted into the sandbox at the corresponding paths.

The runtime validates archive size (limited to ~100MB by default), checksum, single-directory constraint, and top-level manifest presence before accepting uploads or mounting into sandboxes, rejecting malformed packages early rather than propagating errors to session execution.

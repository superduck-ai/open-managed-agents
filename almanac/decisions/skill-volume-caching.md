---
title: "Skill Volume Caching"
summary: "E2B volume-based caching strategy for managed agent skill archives that deterministically identifies skill combinations by manifest hash."
topics: [architecture, performance]
sources:
  - id: skills-runtime
    type: file
    path: docs/design/be/managed-agent-skills-runtime.md
  - id: e2b-runtime
    type: file
    path: internal/runtime/e2bruntime/runtime.go
  - id: builtin-skills-migration
    type: file
    path: internal/db/migrations/00010_builtin_skills.sql
---

Managed agent skill archives are cached through E2B volumes keyed by a deterministic manifest SHA256 hash. This allows sessions with identical skill combinations to reuse pre-prepared volumes, reducing cold start latency and avoiding repeated object storage reads.

## Manifest-Based Volume Identity

The volume cache key derives from a `manifest.json` containing resolved skill metadata rather than the original request representation [@skills-runtime]. Each skill entry contributes:

* `source`: `"anthropic"` for built-in skills or `"custom"` for workspace skills
* `id`: Skill identifier
* `resolved_version`: The concrete version (for `latest` references)
* `directory`: Single top-level directory within the archive
* `filename`: Archive filename for volume storage
* `sha256`: Archive checksum
* `size`: Archive byte size

The manifest hash excludes the user's requested version string (`"latest"` versus an explicit version). A session requesting `version: "latest"` that resolves to version `1` will share the same volume as a session explicitly requesting `version: "1"` [@skills-runtime].

## Cache Hit Detection

On session launch, the runner:

1. Computes the manifest SHA256 from the session's resolved skill references
2. Checks if a volume named `managed-agent-skills-{sha256}` exists
3. If present, verifies the `.ready` marker file contains the matching manifest hash
4. When the marker matches, returns the cached volume without any object storage I/O [@e2b-runtime]

Volume existence is checked through E2B's `ListVolumes` API. The `connectOrCreateSkillVolume` method handles concurrent creation races by re-listing after a create conflict to connect to the winner's volume [@e2b-runtime].

## Volume Population

On a cache miss, the runner:

1. Creates a new E2B volume with the manifest-derived name
2. Writes `manifest.json` as the first file
3. Loads each skill archive sequentially via `RuntimeSkill.LoadArchive`
4. Validates archive size, checksum, total extracted size, single top-level directory constraint, and presence of `SKILL.md`
5. Writes each validated archive to the volume
6. Writes the `.ready` marker containing the manifest SHA256
7. Returns the volume for sandbox mounting [@skills-runtime] [@e2b-runtime]

Archives are loaded and written sequentially rather than in parallel to avoid multiple large zip files residing in memory simultaneously [@skills-runtime].

## Volume Mounting

The prepared volume mounts into sandboxes at `/mnt/skills`. The mount metadata persists in the environment work's `metadata` field under the `managed_agent_skills_mount` key [@e2b-runtime]. Sandbox creation receives this as a volume mount mapping alongside the user-data volume.

The mounted volume presents:

```
/mnt/skills/
  manifest.json
  anthropic__xlsx__builtin__<sha>.zip
  custom__skill_...__1__<sha>.zip
  .ready
```

Environment-manager later extracts these zip archives into `/workspace/skills` for Claude Code's filesystem-based discovery [@skills-runtime].

## Async Prewarming

Agent create/update, deployment create/update, and custom skill version create events enqueue best-effort `skill_prewarm` jobs to warm the cache proactively [@skills-runtime].

* **Job types**: `snapshot` jobs contain an agent snapshot; `fanout` jobs scan for agents/deployments referencing a newly-published custom skill version
* **Best-effort enqueue**: Uses a short timeout context; failures only log,不影响主请求响应
* **Worker behavior**: Reuses the same `PrepareSkillMount` path; failures respect the jobs table retry state machine

Prewarming is a pure optimization. The session launch path always performs lazy preparation if the prewarm job hasn't completed or has failed [@skills-runtime].

## Built-in Skill Catalog

Built-in skills (`source: "anthropic"`) persist in the `builtin_skills` and `builtin_skill_versions` tables with their S3 archive metadata [@builtin-skills-migration]. The runtime resolver reads from this catalog rather than a local filesystem directory, enabling the same volume caching path for both built-in and custom skills.

## Resolution Semantics

The `latest` version designation resolves at session launch time to the currently active version in the database. Once a session starts, it uses the fixed skill view from its launch-time manifest—subsequent skill version publishing does not affect running sessions [@skills-runtime].

Multiple skills with identical top-level directory names are rejected during volume preparation to avoid ambiguous discovery entries, since Claude Code discovers skills by directory name [@skills-runtime].

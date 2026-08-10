---
title: "Skills Runtime"
summary: "The skills runtime resolves skill references from agent snapshots into mounted zip archives accessible to Claude Code in sandbox environments."
topics: [architecture]
sources:
  - id: skills-runtime-design
    type: file
    path: docs/design/be/managed-agent-skills-runtime.md
  - id: resolver
    type: file
    path: internal/skills/resolver.go
  - id: mount-manifest
    type: file
    path: internal/skills/mount_manifest.go
  - id: runner
    type: file
    path: internal/environments/runner.go
  - id: e2b-runtime
    type: file
    path: internal/runtime/e2bruntime/runtime.go
---

The skills runtime is responsible for taking skill references from managed agent snapshots and making them available to Claude Code at runtime. Rather than having Claude Code query an API or database for skills, the runtime provides a stable `/mnt/skills` mount point in sandbox environments containing resolved skill archives as zip files with an accompanying manifest. This approach isolates Claude Code from backend concerns while providing a deterministic, cacheable skill delivery mechanism.

## Resolution

Skill resolution begins with the `RuntimeResolver`, which parses agent snapshots and resolves `{type, skill_id, version}` references into concrete `RuntimeSkill` objects containing metadata and archive loaders [@resolver]. The resolver supports two skill types:

- **Built-in skills** (`type: "anthropic"`): Resolved from the `builtin_skills` and `builtin_skill_versions` database tables, with archives stored in object storage
- **Custom skills** (`type: "custom"`): Resolved from workspace-scoped `skill_versions` tables, also with object storage archives

The `latest` version keyword is resolved at resolution time to the current active version, avoiding torn reads between the version lookup and archive fetch. For custom skills, a single join query fetches the latest version atomically [@skills-runtime-design].

Resolution deduplicates skill references by `{source, skill_id, version}` and validates that no two skills use the same install directory, since Claude Code discovers skills by directory name [@resolver].

## Mount Manifest

Once skills are resolved, the runtime builds a deterministic `MountManifest` containing an ordered list of skill entries [@mount-manifest]. Each entry includes:

- `source`, `skill_id`, `version` - The skill identity
- `directory` - The single top-level directory containing the skill
- `filename` - The zip archive filename (e.g., `anthropic__xlsx__builtin__<sha>.zip`)
- `sha256` - Archive checksum for validation
- `size_bytes` - Expected uncompressed size

The manifest is sorted lexicographically to ensure consistent hashing, and the entire manifest JSON is SHA256-hashed to generate a volume name. This means identical skill combinations resolve to the same volume regardless of whether `latest` or an explicit version was requested [@skills-runtime-design].

## Volume Preparation

The `PrepareSkillMount` function uses the manifest hash to create or reuse an E2B volume for the skill archives [@e2b-runtime]. The process follows these steps:

1. Compute manifest hash and derive volume name as `managed-agent-skills-<sha>`
2. List existing volumes and connect if one with the matching name exists
3. If creating new or reconnecting, check for `.ready` marker file containing the manifest hash
4. If ready marker matches, return cached mount without reading archives
5. Otherwise, load each archive from object storage, validate checksum and size, and write to volume
6. Write `manifest.json` and `.ready` marker atomically

Archive loading is lazy and sequential—the resolver only fetches metadata initially, and `LoadArchive` is called only during volume miss handling. Archives are validated for size, SHA256 checksum, single top-level directory structure, and presence of `SKILL.md` before being written [@resolver].

## Sandbox Integration

When the environment runner prepares a managed agent session, it calls the runtime resolver and mount preparer, then patches the environment work metadata with the skill mount information [@runner]. The E2B provider uses this metadata to mount the prepared volume at `/mnt/skills` when creating the sandbox [@e2b-runtime].

The mounted volume presents a consistent view to `environment-manager`:

```text
/mnt/skills/
  manifest.json
  anthropic__xlsx__builtin__<sha>.zip
  custom__skill_name__1__<sha>.zip
  .ready
```

The `environment-manager` binary is responsible for extracting these zip archives to `/workspace/skills` before starting Claude Code, but it does not validate the manifest or checksum—that responsibility belongs to the runtime [@skills-runtime-design].

## Failure Handling

The skills runtime enforces strict correctness boundaries. If a referenced skill is missing, unavailable, or fails validation, the entire environment work fails rather than continuing with a partial skill set. This prevents silent failures where Claude Code appears to run but lacks declared capabilities [@skills-runtime-design].

The runtime also ensures that if object storage is unavailable or misconfigured, sessions with skills fail fast during the mount preparation phase, while sessions without skills can proceed normally [@resolver].

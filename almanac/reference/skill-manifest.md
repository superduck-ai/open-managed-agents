---
title: "Skill Manifest"
summary: "The skill manifest is a deterministic JSON document that lists all skills mounted in a worker environment, used for volume caching and prewarm optimization."
topics: [reference, skills, runtime]
sources:
  - id: mount-manifest
    type: file
    path: internal/skills/mount_manifest.go
  - id: skills-resolver
    type: file
    path: internal/skills/resolver.go
  - id: skill-prewarm
    type: file
    path: internal/skillprewarm/
---

# Skill Manifest

The skill manifest is a JSON document that enumerates all skills mounted into a Claude Code worker environment. It serves as the basis for deterministic volume naming and caching, enabling skill prewarm jobs to prepare environments before sessions start[@mount-manifest].

## Manifest Structure

The manifest is defined by the `MountManifest` struct in `internal/skills/mount_manifest.go`[@mount-manifest]:

```json
{
  "version": 1,
  "skills": [
    {
      "source": "anthropic",
      "skill_id": "xlsx",
      "version": "1",
      "directory": "/skills/xlsx",
      "name": "Excel",
      "description": "Excel file processing",
      "filename": "anthropic__xlsx__latest__a1b2c3d4e5f6.zip",
      "sha256": "a1b2c3d4e5f6...",
      "size_bytes": 12345
    }
  ]
}
```

**Fields**:
- `version`: Manifest format version (currently 1)
- `skills`: Array of skill entries ordered deterministically

Each skill entry includes[@mount-manifest]:
- `source`: Skill source ("anthropic" or "custom")
- `skill_id`: Identifier for the skill
- `version`: Skill version or "latest"
- `directory`: Mount path within the worker
- `name`: Human-readable name (optional)
- `description`: Skill description (optional)
- `filename`: Archive filename used for volume naming
- `sha256`: Checksum of the skill archive
- `size_bytes`: Archive size in bytes

## Deterministic Ordering

Skills are sorted by a compound key of `source + "\x00" + skill_id + "\x00" + version + "\x00" + directory`[@mount-manifest]. This ensures that identical skill sets produce identical manifests regardless of the order they were specified in the agent configuration.

The deterministic ordering is critical for caching—the manifest hash becomes the volume identifier, so stable ordering prevents cache misses due to reordering.

## Manifest Hash

The `BuildMountManifest` function generates a SHA-256 hash of the entire manifest JSON[@mount-manifest]. This hash serves as:

1. **Volume cache key**: Skills with the same hash share a prewarmed volume
2. **Change detection**: Agent updates that don't change skills reuse existing volumes
3. **Prewarm job identity**: Each unique hash triggers at most one prewarm job

The hash is computed as hex-encoded SHA-256 of the manifest JSON bytes.

## Archive Filename

Each skill gets a filename constructed from its properties[@mount-manifest]:

```
{source}__{skill_id}__{version}__{prefix}.zip
```

The `prefix` is the first 12 characters of the SHA-256 checksum (or "unknown" if unavailable). Filename components are sanitized to only include alphanumeric characters, hyphens, underscores, and periods, with a maximum length of 80 characters per component[@mount-manifest].

**Example**: `anthropic__xlsx__latest__a1b2c3d4e5f6.zip`

## Skill Resolution

The `RuntimeResolver` in `internal/skills/resolver.go` builds the manifest by resolving agent snapshot skill references to concrete skills[@skills-resolver]:

1. Extract skill refs from agent snapshot's `skills` array
2. Deduplicate refs by `type + skill_id + version`
3. Resolve each ref to a `RuntimeSkill` with archive bytes or loader
4. Validate no duplicate directory conflicts
5. Build manifest entries with checksums and sizes

Resolution supports both "anthropic" (built-in) and "custom" skill types[@skills-resolver]. Built-in skills are fetched from the `builtin_skill_versions` table, while custom skills come from the `skills` table with workspace-scoped access.

## Prewarm Integration

The manifest hash drives the skill prewarm system[@skill-prewarm]:

1. Agent create/update triggers skill change detection
2. If skills changed, prewarm service receives the agent snapshot
3. Prewarm resolves skills and builds manifest
4. Existing prewarm jobs with same hash are reused
5. New hash triggers async volume preparation job

Prewarm jobs mount skill archives at configured directories, warm language servers, and prepare the environment for immediate session start.

## Validation Rules

The manifest enforces several invariants[@mount-manifest]:

- **Duplicate filenames**: Multiple skills cannot produce the same filename
- **Checksum required**: Each skill must have a non-empty SHA-256 checksum
- **Size validation**: Archive size defaults to bytes length if not provided
- **Directory conflicts**: No two skills can mount to the same directory

When building a manifest from runtime skills, the system validates the archive checksum matches the declared checksum (if provided), failing with an error if they differ[@mount-manifest].

## Mount Paths

The `directory` field specifies where within the worker environment the skill is mounted. This becomes part of the skill volume's structure and must be unique across all skills in a single agent[@skills-resolver].

Typical directories follow patterns like `/skills/{skill_id}` or `/tools/{name}`, but agents can configure any path structure that doesn't conflict with other skills.

## Version Handling

Skills specify a version string or use "latest" as a default[@skills-resolver]. The resolver normalizes "latest" to the actual current version at resolution time, storing the concrete version in the manifest.

Versioning enables:
- Reproducible environments (specific versions)
- Rolling updates (latest resolves to newest)
- A/B testing (different versions in different agents)

## Error Cases

Manifest construction fails with descriptive errors for[@mount-manifest]:

- Empty or invalid checksums
- Duplicate filenames in skill set
- Checksum mismatch between archive and declared value
- Missing required fields in skill definition

These errors surface during agent creation/update if skill configuration is invalid, preventing sessions from starting with broken skill references.

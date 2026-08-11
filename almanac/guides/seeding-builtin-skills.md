---
title: "Seeding Builtin Skills"
summary: "Import prebuilt skills into the builtin catalog using the seed-builtin-skills command."
topics: [skills, deployment, backend]
sources:
  - id: seed-main
    type: file
    path: cmd/seed-builtin-skills/main.go
  - id: skills-seed-go
    type: file
    path: internal/skills/seed.go
  - id: migration-10
    type: file
    path: internal/db/migrations/00010_builtin_skills.sql
  - id: skills-examples
    type: file
    path: skills/examples/
---

Builtin skills are prepackaged skill archives managed centrally and available to all workspaces. They're stored in the `builtin_skills` and `builtin_skill_versions` tables with version tracking [@migration-10].

## Database Schema

Builtin skills use two tables for skill and version management [@migration-10]:

- `builtin_skills`: Skill catalog entries with `external_id`, `display_title`, and `latest_version`
- `builtin_skill_versions`: Specific versions with `s3_bucket`, `s3_key`, `sha256`, and package metadata

Each skill can have multiple versions, with `builtin_skills.latest_version` pointing to the most recent.

## Running the Seed Command

The seed command imports `.skill` archives from a directory into the builtin catalog [@seed-main]:

```bash
go run cmd/seed-builtin-skills/main.go \
  --dir skills/examples/ \
  --versions versions.json
```

Required flags:
- `--dir`: Directory containing `.skill` archives

Optional flags:
- `--versions`: JSON file or `skill_id=version` pairs mapping skill IDs to versions
- `--prune`: Soft-delete builtin versions not present in the source directory

## Version Management

Without `--versions`, the command generates versions from file modification times and SHA256 hashes [@skills-seed-go]:

```go
version := sha[:12] // First 12 characters of SHA256
```

Provide explicit versions with a JSON file:

```json
{
  "grocery-shopping": "2025-01-15",
  "theme-factory": "2.0.0"
}
```

Or a simple text file with `skill_id=version` pairs:

```text
grocery-shopping=2025-01-15
theme-factory=2.0.0
```

## Storage and Metadata

Each skill archive is uploaded to object storage with a predictable key pattern [@skills-seed-go]:

```
builtin-skills/{skill_id}/versions/{version}/{sha256}.skill
```

The database stores the `s3_bucket`, `s3_key`, `size_bytes`, and `sha256` for each version, enabling content integrity verification.

## Pruning Old Versions

With `--prune`, the command soft-deletes builtin skill versions that no longer exist in the source directory [@skills-seed-go]. Deleted versions are also removed from object storage to clean up orphaned artifacts.

The command reports imported and pruned counts:

```
Imported 3 builtin skill(s), pruned 2 version(s): [grocery-shopping theme-factory mcp-builder]
```

## Builtin Skills in the Repository

The repository maintains a collection of example skills in `skills/examples/` that can be used as builtin skill sources [@skills-examples]. These include production-ready skills for common workflows like grocery shopping, event planning, and document co-authoring.

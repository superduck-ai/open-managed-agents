---
title: "Built-in Skills"
summary: "How built-in skills from Anthropic are seeded, versioned, and stored in the catalog."
topics: [skills, catalog, storage]
sources:
  - id: skills-seed
    type: file
    path: internal/skills/seed.go
  - id: builtin-design
    type: file
    path: docs/design/be/skills-builtin-seed-and-storage.md
---

Built-in skills are global, read-only skills provided by Anthropic that are seeded into the database catalog and stored in object storage. They differ from custom workspace skills in that they're organization-scoped and managed centrally.

## Data Model

Built-in skills use two separate tables from custom skills:

- **`builtin_skills`**: Catalog of skill definitions with `external_id` (e.g., `xlsx`), `display_title`, and `latest_version`
- **`builtin_skill_versions`**: Versioned skill packages with archive metadata, S3 location, size, and SHA256 hash [@builtin-design]

The tables maintain application-layer integrity without foreign key constraints, following project schema conventions.

## Seeding Process

The seed tool (`cmd/seed-builtin-skills`) imports `.skill` archives from a directory:

```bash
go run ./cmd/seed-builtin-skills --dir /path/to/skills --versions versions.txt --prune
```

Archive validation rules:
- Must unpack to a single top-level directory
- Must contain a `SKILL.md` metadata file
- Paths must be safe relative paths (no absolute paths, `..`, or NUL)
- Maximum size is 8 MiB [@skills-seed]

Version assignment uses an optional `versions` file mapping `skill_id=version` per line, or defaults to the first 12 characters of the SHA256 hash or the modification date.

## Object Storage

Archives are stored in MinIO with the key pattern:

```
builtin-skills/{skill_id}/versions/{version}/{sha256}.skill
```

The `version` in the storage path is the platform version identifier, not the SHA256 hash, allowing for human-readable versioning while maintaining content-addressable storage [@builtin-design].

## API Behavior

The `/v1/skills` endpoint distinguishes between sources:

- `source=anthropic`: Returns only built-in skills from the catalog
- `source=custom`: Returns only workspace-specific custom skills
- No `source`: Returns merged results with cursor-based pagination across both catalogs [@builtin-design]

Built-in skills are read-only through the API:
- Creating versions returns a read-only error
- Deleting skills or versions returns a read-only error

Custom skills enforce workspace-level uniqueness by `display_title`, with a partial unique index preventing duplicate active skill names within a workspace.

## Pruning

The `--prune` flag soft-deletes built-in skill versions that are no longer present in the input directory, both from the database and from object storage. This allows catalog updates to remove deprecated skills while maintaining audit trails.

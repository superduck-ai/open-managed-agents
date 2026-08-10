---
title: "Creating Custom Skill"
summary: "Create custom skills by writing a SKILL.md file, packaging it as a .skill archive, and uploading via the API."
topics: [skills, development, tutorial]
sources:
  - id: skills-handler
    type: file
    path: internal/skills/handler.go
  - id: skills-api-test
    type: file
    path: tests/skills_api_test.go
  - id: grocery-skill
    type: file
    path: assets/skills/examples/grocery-shopping/SKILL.md
  - id: skills-examples-dir
    type: file
    path: assets/skills/examples/
---

Custom skills extend Open Managed Agents with new capabilities. A skill is a ZIP archive containing a `SKILL.md` file at the root, plus any supporting documentation and code files.

## Skill Structure

Every skill must have a `SKILL.md` file with YAML frontmatter defining metadata [@grocery-skill]:

```markdown
---
name: grocery-shopping
description: Help order groceries for delivery. Concierge-style flow.
---

You're helping me order groceries for delivery. Act like a concierge — warm, natural, and one step at a time.
```

The name and description in frontmatter become the skill's display title and summary in the catalog. The body content provides the system prompt that guides agent behavior.

## Packaging the Skill

Package the skill directory into a `.skill` archive:

```bash
zip -r grocery-shopping.skill grocery-shopping/
```

The archive must contain the skill files with the skill name as the top-level directory. Multiple top-level directories are rejected during upload [@skills-api-test].

## Uploading via API

Upload the skill as multipart form data with the required beta header [@skills-handler]:

```bash
curl -X POST http://localhost:38080/v1/skills?beta=true \
  -H "anthropic-beta: skills-2025-10-02" \
  -H "Authorization: Bearer sk-ant-..." \
  -F "files[]=@grocery-shopping.skill" \
  -F "display_title=Grocery Shopping"
```

The API returns the created skill with generated IDs:

```json
{
  "id": "skill_abc123...",
  "created_at": "2025-01-15T10:30:00Z",
  "display_title": "Grocery Shopping",
  "latest_version": "20250115abc123",
  "source": "workspace",
  "type": "skill"
}
```

## Skill Content Guidelines

- **One skill, one directory**: The archive root must contain exactly one skill directory
- **Path traversal protection**: Paths like `../file` are rejected [@skills-api-test]
- **Required SKILL.md**: Every archive must have `SKILL.md` at the top level
- **Supporting files**: Include reference docs, examples, and schemas as subdirectories

## Managing Versions

Each upload creates a new version. Delete older versions to clean up:

```bash
curl -X DELETE http://localhost:38080/v1/skills/skill_abc123.../versions/20250115abc123?beta=true \
  -H "anthropic-beta: skills-2025-10-02" \
  -H "Authorization: Bearer sk-ant-..."
```

## Skill Examples

The repository includes example skills demonstrating different patterns [@skills-examples-dir]:
- `grocery-shopping`: Concierge-style workflow with multi-step state management
- `theme-factory`: Generative design with multiple output variations
- `mcp-builder`: Tool creation with reference documentation
- `internal-comms`: Enterprise communication templates

# Design Doc Surface Audit

Keeps `docs/design/` in sync with code surfaces (API mounts, `internal/` packages,
SQL migrations, FE routes). Docs live in this same repo.

## What is verified vs not

| Piece | Status |
|-------|--------|
| `audit_design_docs.py` (7 surface types + staleness + map hygiene) + unit tests | Verified locally and on PR CI |
| `classify_changes.py` (deterministic doc-need triage) + unit tests | Verified locally |
| `design-doc-audit.yml` soft CI | Verified (ran on PR #32) |
| Deterministic PR audit comment (`@duckpr docs` / workflow_dispatch) | Implemented; no LLM required |
| DuckPR / Pullfrog LLM docs-sync (`push: restricted`) | Wiring fixed to match pullfrog-private capabilities; **end-to-end agent run still needs a live dispatch with secrets** |

## Commands

```bash
python3 scripts/docs-audit/audit_design_docs.py
python3 scripts/docs-audit/audit_design_docs.py --diff
python3 scripts/docs-audit/audit_design_docs.py --update-snapshot
python3 scripts/docs-audit/audit_design_docs.py --list-extracted
python3 scripts/docs-audit/test_audit_design_docs.py

# Classify whether a PR's changes need design-doc updates (deterministic triage)
python3 scripts/docs-audit/classify_changes.py --files <path> [<path>...] [--keywords kw1 kw2]
python3 scripts/docs-audit/classify_changes.py --files <path> --output /tmp/classify.json
python3 scripts/docs-audit/test_classify_changes.py
```

Or: `just docs-audit` / `just docs-audit-diff` / `just docs-audit-test`.

Docs agent (after merge / with secrets). Model must match DuckPR Review
(successful runs use e.g. `anthropic/glm-5.2` + `LLM_BASE_URL=https://api.kimi.com/coding/`).
Do **not** default to Claude models — this repo's DuckPR wiring is Kimi/OpenCode.

```bash
# audit comment only
gh workflow run "DuckPR Docs Sync" -f pr_number=<N> -f skip_agent=true

# audit + LLM agent (same-repo PR branch; pass the same model DuckPR Review uses)
gh workflow run "DuckPR Docs Sync" -f pr_number=<N> -f model=anthropic/glm-5.2
# or comment on the PR: @duckpr docs
```

## Surface map

Edit `surface_map.md`. Tracked surface types:

| Section | Source |
|---------|--------|
| `## API mounts` | `internal/api/server.go` `Mount` / `Post` registrations |
| `## API subroutes` | `Register*Routes` entry points across `internal/**` (per-package HTTP resources) |
| `## Packages` | `internal/*` directories containing `*.go` |
| `## Migrations` | `internal/db/migrations/*.sql` |
| `## FE routes` | `web/src/app/router.tsx` `path:` entries |
| `## Event contracts` | event-type literals in `internal/managedagentsevents/events.go` |
| `## Auth middleware` | `(s *Server) xxxMiddleware` defs + `.Use(...)` in `internal/api/server.go` |
| `## Unlisted design docs` | `docs/design/**` pages not mapped by any surface above |

Mapping targets:

```
SurfaceID -> docs/design/path.md
SurfaceID -> internal
SurfaceID -> gated:<reason>
```

The audit also runs a **staleness check**: it scans `docs/design/**` prose for
`internal/<pkg>` references and flags any package that no longer exists in code
(catches renames/removals the forward coverage audit cannot see).

## Classifying PR changes (deterministic)

`classify_changes.py` triages whether a PR needs design-doc updates **before**
the LLM agent runs. It is the anti-drop guardrail: it covers only
high-confidence cases (CI/test/config → exclude; events/migrations/auth/router
→ must_document) and routes everything it cannot decide to `needs_review` with
a reason — it never silently buckets an ambiguous change as exclude. The
docs-sync skill (step 2) consumes its verdict as binding input.

## Writing design docs (agent + humans)

Doc sync must follow `AGENTS.md` §「设计文档同步」. The Pullfrog skill
`.agents/skills/docs-sync/SKILL.md` operationalizes that contract: when to
write, no padding, truth-first, pick an existing `docs/design/` exemplar, and
keep compatibility + test/acceptance notes aligned with the code.

## Exit codes

| Exit | Meaning |
|------|---------|
| 0 | Clean |
| 1 | Coverage / map hygiene findings (CI soft-fails initially) |
| 2 | Extraction floor or completeness accounting failed |

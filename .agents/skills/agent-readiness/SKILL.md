---
name: agent-readiness
description: Run a full 5-phase agent-readiness audit on the current repository — scan, discover apps, evaluate 82 criteria via parallel subagents, validate, and generate report + dashboard with persistent score history
---

# Agent Readiness

You are the Agent Readiness Auditor, a static repository auditor specialized in evaluating codebases for autonomous agent readiness. You are objective, thorough, and deterministic in your evaluations.

## Pre-flight Check

Before starting, verify the environment and capture metadata:

```bash
git rev-parse --is-inside-work-tree
```

If this fails, stop and tell the user they must be inside a git repository.

Then run:

```bash
REPO_ROOT=$(git rev-parse --show-toplevel)
RAW_REPO_URL=$(git remote get-url origin 2>/dev/null || true)
if [ -z "$RAW_REPO_URL" ]; then
  echo "ERROR: Could not determine the origin remote URL."
  exit 1
fi
REPO_URL=$(node -e 'const raw = process.argv[1]; const ssh = raw.match(/^[^@]+@([^:]+):(.+)$/); if (ssh) { console.log(`https://${ssh[1]}/${ssh[2].replace(/\.git$/, "")}`); process.exit(0); } try { const url = new URL(raw); url.pathname = url.pathname.replace(/\.git$/, ""); console.log(url.toString().replace(/\/$/, "")); } catch { console.log(raw); }' "$RAW_REPO_URL")
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
COMMIT_HASH=$(git rev-parse HEAD)
REPORT_CREATED_AT=$(node -e 'console.log(Date.now())')
HAS_LOCAL_CHANGES=false
if [ -n "$(git status --porcelain)" ]; then
  HAS_LOCAL_CHANGES=true
fi
HAS_NON_REMOTE_COMMITS=false
if git rev-parse --verify @{upstream} >/dev/null 2>&1; then
  if [ "$(git rev-list --count @{upstream}..HEAD)" -gt 0 ]; then
    HAS_NON_REMOTE_COMMITS=true
  fi
fi
ARTIFACT_DIR="$REPO_ROOT/.agent-readiness"
LATEST_DIR="$ARTIFACT_DIR/latest"
HISTORY_DIR="$ARTIFACT_DIR/history"
mkdir -p "$LATEST_DIR" "$HISTORY_DIR"
ARTIFACT_GITIGNORE="$ARTIFACT_DIR/.gitignore"
if [ -f "$ARTIFACT_GITIGNORE" ]; then
  if ! grep -Eq '^latest/?$' "$ARTIFACT_GITIGNORE"; then
    printf '\nlatest/\n' >> "$ARTIFACT_GITIGNORE"
  fi
else
  printf 'latest/\n' > "$ARTIFACT_GITIGNORE"
fi
touch "$HISTORY_DIR/.gitkeep"
```

Also locate the bundled CLI that ships with this skill. It lives at
`bin/agent-readiness.mjs` *inside the skill directory itself* — search the
common host install locations:

```bash
SKILL_BIN=""
for d in \
  "$REPO_ROOT/.claude/skills/agent-readiness" \
  "$HOME/.claude/skills/agent-readiness" \
  "$REPO_ROOT/.claude/skills/agent-readiness-report" \
  "$HOME/.claude/skills/agent-readiness-report"; do
  if [ -f "$d/bin/agent-readiness.mjs" ]; then
    SKILL_BIN="$d/bin/agent-readiness.mjs"
    break
  fi
done
if [ -z "$SKILL_BIN" ]; then
  echo "ERROR: Could not locate agent-readiness.mjs. Make sure the skill"
  echo "directory is installed at one of the known host paths."
  exit 1
fi
SKILL_DIR=$(dirname "$(dirname "$SKILL_BIN")")
```

`$SKILL_BIN` is the bundled CLI used in Phases 4–5. `$SKILL_DIR` is the
skill's root directory; subagents read criteria definitions from
`$SKILL_DIR/criteria/<category>.md`.

Your goal: Inspect the current local repository *without modifying source files* and emit an **Agent-Readiness Report** that scores the repository on 82 criteria. You may create audit artifacts only inside `$REPO_ROOT/.agent-readiness/`.

Report the repo context to the user, then proceed phase-by-phase.

---

## Phase 1 - Repository Scan

**NOTE: Repository Boundary Restrictions**
• You MUST stay within the git repository boundaries (where .git directory exists)
• Parent directories are allowed as long as they remain within the repository
• NEVER explore directories outside the git repository root
• If the command is run from a subdirectory, you should explore the entire repository including parent dirs up to the repo root
• All exploration must stay within the repository - do not traverse outside the git repository boundaries

1. **Detect repository language**
   • JavaScript/TypeScript clues: package.json, tsconfig.json, .js/.ts/.jsx/.tsx files
   • Python clues: pyproject.toml, setup.py, requirements.txt, .py files
   • Rust clues: Cargo.toml, .rs files
   • Go clues: go.mod, .go files
   • Java clues: pom.xml, build.gradle, .java files
   • Ruby clues: Gemfile, .gemspec, .rb files
   • Record primary language(s) detected

2. **Explore the repository structure**
   • Walk the file tree within the entire git repository (from repository root, even if command was run from a subdirectory)
   • Stay within the git repository boundaries - ignore .git, node_modules, dist, build directories
   • Identify the main source directories (src/, app/, lib/, etc.)
   • Locate configuration files, documentation, and test directories

---

## Phase 2 - Application Discovery

**CRITICAL: This phase must be completed BEFORE Phase 3.**

**Goal: Identify the applications that exist in the repository by thoroughly exploring the directory structure (staying within the git repository's boundaries)**

### What is an Application?

An application is a **directory** (not a file) that represents an independently deployable unit:
- Has its own deployment lifecycle (can be deployed separately from other code)
- Can be built and run independently
- Serves end users or other systems directly

**Key test**: Could this directory be moved to its own repository and still function? If yes, it's likely an application.

---

### Discovery Guidelines

**Scan the repository and identify all directories that meet the application definition above.**

**Common patterns:**
- Single-purpose repositories → Usually 1 application (the root)
- Monorepos with service directories → Count each independently deployable service
- Library repositories → Usually 1 application (the root), even if it's a library
- Showcase/tutorial repositories → Usually 1 application (the collection itself)

**Important:**
- Applications are **directories**, never individual files
- Shared libraries or utility packages are NOT applications (they're imported by applications)
- Examples or demos that share infrastructure are NOT separate applications

**If you find 0 applications, count the repository root (.) as 1 application.**

---

### Catalog all applications in the repository

- For each app, record the relative path from repository root (e.g., "apps/backend")
- Create a concise description based on:
  - README.md or package.json description field
  - Primary purpose inferred from directory name and package.json scripts
  - Example: "Main Next.js application for user interface" or "CLI tool for local development"
- Classify each app's source type:
  - **source**: Contains source code that should be linted, formatted, and tested
  - **distribution**: Only contains build artifacts, re-exports, or packaging for publishing (e.g., npm wrapper packages that re-publish outputs from another app). Has no meaningful source code of its own.
- List your findings in plaintext format:
    ```
    APPLICATIONS_IDENTIFIED: N

    Applications:
    1. [path] - [brief description] (source)
    2. [path] - [brief description] (distribution)
    ...
    ```

**Commitment:**
Once you identify N applications, you MUST use:
- denominator = N for ALL 38 Application Scope criteria
- denominator = 1 for ALL 44 Repository Scope criteria

---

## Phase 3 - Criterion Evaluation (Parallel Subagents)

**Use the Agent tool to dispatch 9 subagents in parallel**, one per category. Each subagent evaluates its category's criteria independently.

### Shared context for all subagents

Before dispatching, prepare a context block from Phase 1 & 2 results:

```
REPO_ROOT: <value>
REPO_URL: <value>
Languages: <detected languages>
Applications (N total):
  1. <path> - <description> (<sourceType>)
  2. ...

Evaluation rules:
- Repository Scope: numerator = 1 (pass) / 0 (fail) / null (skipped), denominator = 1
- Application Scope: numerator = count of passing apps (0..N) / null (skipped), denominator = N
- null numerator ONLY for criteria marked [Skippable]
- Distribution-only apps auto-PASS all Application Scope criteria (count them in numerator)
  Example: 3 apps (2 source + 1 distribution), both source pass → numerator = 3, denominator = 3
  Example: 3 apps (2 source + 1 distribution), 1 source passes → numerator = 2, denominator = 3
- Rationale: max 500 chars
- Be deterministic. If evidence is ambiguous, fail the item.
- Prefer existence checks, but read config file contents when needed to verify what a tool enables.
- For monorepos: if a tool is configured at repo root, read its config to determine which apps it covers.
```

### Dispatch 9 subagents in parallel

Send **all 9 Agent calls in a single message** so they run concurrently. Each subagent prompt should follow this template:

```
You are evaluating agent-readiness criteria for category "<CATEGORY_NAME>" on a git repository.

<paste shared context block above>

Read the criteria file to get evaluation instructions:

    $SKILL_DIR/criteria/<CATEGORY_ID>.md

Then evaluate EVERY criterion listed. For each one:
1. Follow the `instructions` field — use bash (find, grep, ls) and Read to check for evidence
2. Read config file contents when needed — don't just check if a file exists, check what it enables
3. Record: { "criterion_id": { "numerator": <int|null>, "denominator": <int>, "rationale": "<string>" } }

Return your results as a single JSON object containing ONLY the criteria from this category. Example:

{
  "lint_config": { "numerator": 1, "denominator": 1, "rationale": "ESLint configured via eslint.config.js" },
  "type_check": { "numerator": 0, "denominator": 1, "rationale": "No tsconfig.json found" }
}

Return ONLY the JSON. No markdown fences, no explanation.
```

**The 9 categories (file → category_id):**
- `$SKILL_DIR/criteria/style.md` → Style & Validation
- `$SKILL_DIR/criteria/build.md` → Build System
- `$SKILL_DIR/criteria/testing.md` → Testing
- `$SKILL_DIR/criteria/docs.md` → Documentation
- `$SKILL_DIR/criteria/dev_env.md` → Development Environment
- `$SKILL_DIR/criteria/debugging.md` → Debugging & Observability
- `$SKILL_DIR/criteria/security.md` → Security
- `$SKILL_DIR/criteria/task_discovery.md` → Task Discovery
- `$SKILL_DIR/criteria/product.md` → Product & Experimentation

### Merge results

After all 9 subagents complete, merge their JSON outputs into a single object with all 82 criteria. Build the wrapped input as a shell variable with repository metadata (no manual JSON escaping required):

```bash
WRAPPED_JSON="$(cat <<'REPORT_EOF' | node -e '
const fs = require("node:fs");
const report = JSON.parse(fs.readFileSync(0, "utf8"));
const payload = {
  repoUrl: process.argv[1],
  commitHash: process.argv[2],
  branch: process.argv[3],
  hasLocalChanges: process.argv[4] === "true",
  hasNonRemoteCommits: process.argv[5] === "true",
  createdAt: Number(process.argv[6]),
  report,
};
process.stdout.write(JSON.stringify(payload));
' "$REPO_URL" "$COMMIT_HASH" "$CURRENT_BRANCH" "$HAS_LOCAL_CHANGES" "$HAS_NON_REMOTE_COMMITS" "$REPORT_CREATED_AT"
{ ...merged 82 criteria... }
REPORT_EOF
)"
```

---

## Phase 4 - Report Validation

**CRITICAL: Before proceeding, validate your report.**

1. **Self-check:**
   ✓ All 38 Application Scope criteria have denominator = N
   ✓ All 44 Repository Scope criteria have denominator = 1
   ✓ Report contains EXACTLY 82 criterion keys
   ✓ No invented/extra criterion names
   ✓ null numerator only on [Skippable] criteria

2. **Schema validation via CLI:**

```bash
echo "$WRAPPED_JSON" | node "$SKILL_BIN" validate
```

- Exit 0 → pass, proceed to Phase 5
- Exit 3 → schema validation failure. Read the error output, fix the report JSON, and re-validate
- Exit 1 → I/O error, investigate

If ANY validation check fails, STOP and revise before proceeding.

---

## Phase 5 - Scoring & Report Generation

### 5a. Calculate scores

```bash
printf '%s\n' "$WRAPPED_JSON" > "$LATEST_DIR/agent-readiness-report.json"
echo "$WRAPPED_JSON" | node "$SKILL_BIN" score \
  --output "$LATEST_DIR/agent-readiness-score.json" \
  --history-dir "$HISTORY_DIR"
```

### 5b. Generate dashboard

```bash
echo "$WRAPPED_JSON" | node "$SKILL_BIN" dashboard \
  --output "$LATEST_DIR/agent-readiness-dashboard.html" \
  --history-dir "$HISTORY_DIR"
```

### 5c. Provide a human-readable summary

After generating scores, present a structured report in this EXACT format:

```markdown
# Level
<Output the achieved level: Level 1, Level 2, Level 3, Level 4, or Level 5>

# Applications
<List all applications discovered with their descriptions>
1. [path] - [brief description] ([sourceType])

# Criteria
<For each criterion evaluated, show: criterion name → score (numerator/denominator) with brief rationale>
Format as:
**Category Name**
- Criterion Name: X/Y - Rationale for the score (especially if failing)
- Another Criterion: X/Y - Rationale

Organize by category (Style & Validation, Build System, Testing, Documentation, Dev Environment, Debugging & Observability, Security, Task Discovery, Product)

# Action Items
<List 2-3 high-impact next steps to reach the next level>

# Generated Files
- .agent-readiness/latest/agent-readiness-report.json — latest wrapped report with repo metadata
- .agent-readiness/latest/agent-readiness-score.json — computed scores, level, and raw evaluation data
- .agent-readiness/latest/agent-readiness-dashboard.html — visual dashboard (open in browser)
- .agent-readiness/history/*.json — append-only score snapshots used for the Level Over Time chart
```

• Focus on being concise yet informative
• For criteria, highlight rationale especially for failing checks (0 score)
• Action items should be specific and achievable

---

## Behavioral Guidelines

• Be deterministic: identical repo → identical output
• Prefer existence checks over deep semantic analysis
• Assume default branch is the evaluation target
• If evidence is ambiguous, fail the item
• Keep notes terse, actionable, and under 500 characters
• Application count from Phase 2 is fixed for the entire evaluation
• Repository Scope denominators are ALWAYS 1
• Application Scope denominators are ALWAYS N (from Phase 2)
• Use ONLY the 82 defined criterion IDs from the catalog
• The validate command will reject your report if you violate schema constraints
• NEVER modify existing repository files — only create audit artifacts inside `.agent-readiness/`

### Task Discovery (4 criteria)

**Repository Scope:**
- **issue_templates** (Level 2, Repo): Issue templates – .github/ISSUE_TEMPLATE/ (GitHub) or .gitlab/issue_templates/ (GitLab) directory exists with structured templates for bugs, features, etc. Teaches agents what information to provide when creating issues.
- **issue_labeling_system** (Level 2, Repo): Issue labeling system – Consistent labels exist for priority (P0-P3 or critical/high/medium/low), type (bug, feature, chore), and area (frontend, backend, infra). Enables agents to filter and prioritize work programmatically.
- **backlog_health** (Level 4, Repo) [Skippable]: Backlog health – Issues have clear titles and recent activity. If `gh` or `glab` CLI is available and authenticated, run `gh issue list --state open --limit 50 --json title,createdAt,labels`. Count issues with: 1) titles > 10 characters, 2) at least one label. PASS if >70% of open issues have both a descriptive title (>10 chars) AND at least one label. Also check `gh issue list --state open --json createdAt` - FAIL if >50% of issues are older than 365 days with no recent comments. Skip if `gh`/`glab` CLI is not available or not authenticated.
- **pr_templates** (Level 2, Repo): PR templates – .github/pull_request_template.md (GitHub) or merge request templates (GitLab) exist with sections for description, testing done, and relevant context. Ensures agent PRs include necessary information for reviewers.



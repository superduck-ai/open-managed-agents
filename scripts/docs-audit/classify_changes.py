#!/usr/bin/env python3
"""Classify whether a PR's changes need design-doc updates.

Deterministic, rule-based triage inspired by warp's classify-changelog-pr skill
but scoped to this repo's single-repo docs/design/ model. Consumes a list of
changed files (paths) and optional diff keywords, and produces a classification
for every file plus a PR-level verdict.

Design principle (the no-silent-drop guarantee):
    Rules cover only HIGH-confidence, unambiguous cases. Anything the rules
    cannot classify with confidence is emitted as ``needs_review`` with a
    human-readable reason — it is NEVER silently bucketed as exclude or
    must_document. This is the guardrail against missing a real doc-impacting
    change. A needs_review item surfaces the decision to the LLM agent / human
    instead of guessing.

Output schema (JSON, --output):
    {
      "files": [
        {
          "path": "internal/api/server.go",
          "action": "must_document|should_document|exclude|needs_review",
          "confidence": "high|medium|low",
          "reason": "...",
          "surface_hint": "auth_middleware|api_subroutes|...|null",
          "signals": ["internal/api", "middleware-file"]
        }
      ],
      "verdict": {
        "action": "must_document|exclude|needs_review",
        "reason": "PR touches >=1 must_document file",
        "must_document": [...],
        "should_document": [...],
        "needs_review": [...],
        "excluded": int
      }
    }
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

# ---------------------------------------------------------------------------
# Rule tables
#
# Each rule is (predicate, action, confidence, reason_template). Rules are tried
# in order; the first match wins for a given file. The order is deliberately:
#   1. explicit-exclude signals first (CI/test/config/docs/bots) — these are
#      unambiguous and must not be overridden by a later include rule.
#   2. high-confidence include signals (event contracts, state machines,
#      migrations, auth/permission code) — AGENTS.md §设计文档同步 names these
#      as "must document" categories.
#   3. medium-confidence include signals (behavior-bearing packages).
#   4. fallback: needs_review with the specific reason the rules ran out.
# ---------------------------------------------------------------------------

# File-path patterns that are UNAMBIGUOUSLY exclude (no design-doc consequence).
EXCLUDE_PATH_PATTERNS: list[tuple[re.Pattern[str], str]] = [
    (re.compile(r"^\.github/(workflows|actions)/"), "CI workflow / composite action"),
    (re.compile(r"\.github/(PULL_REQUEST_TEMPLATE|ISSUE_TEMPLATE)"), "PR/issue template"),
    (re.compile(r"_test\.go$"), "Go test file"),
    (re.compile(r"\.(test|spec)\.(ts|tsx|js|jsx)$"), "JS/TS test file"),
    (re.compile(r"^docs/design/"), "design doc itself (not code)"),
    (re.compile(r"^scripts/docs-audit/"), "audit tooling (not a code surface)"),
    (re.compile(r"^(README|CHANGELOG|CONTRIBUTING|LICENSE)"), "top-level meta doc"),
    (re.compile(r"^\.agents/skills/"), "agent skill definition (not code)"),
    (re.compile(r"\.(md|mdx)$"), "documentation file"),
    (re.compile(r"^(\.env|docker-compose|Makefile|justfile|Taskfile)"), "build/dev config"),
    (re.compile(r"\.(lock|sum|mod)$"), "lockfile / module manifest"),
    (re.compile(r"^\.github/(dependabot|codeowners|mergify)"), "repo meta config"),
]

# Config-only Go packages: changes here are infra/config, not behavior.
EXCLUDE_PKG_PATTERNS = {
    "observability",
    "cleanup",
    "ids",
}

# Files that map to AGENTS.md "must document" categories. Each entry carries a
# surface_hint so the classifier output can be cross-checked against the audit.
# (path_regex, reason, surface_hint)
MUST_DOCUMENT_PATH_RULES: list[tuple[re.Pattern[str], str, str]] = [
    (
        re.compile(r"internal/managedagentsevents/.*\.go$"),
        "event contract changed (AGENTS.md §设计文档同步: event contracts)",
        "event_contracts",
    ),
    (
        re.compile(r"internal/db/migrations/.*\.sql$"),
        "database schema / migration changed (data model)",
        "migrations",
    ),
    (
        re.compile(r"internal/api/.*middleware\.go$"),
        "HTTP middleware changed (platform / auth boundary)",
        "auth_middleware",
    ),
    (
        re.compile(r"internal/api/(auth|platform_mcp_vault_auth)\.go$"),
        "authentication / authorization code changed (permission boundary)",
        "auth_middleware",
    ),
    (
        re.compile(r"internal/api/server\.go$"),
        "API router wiring changed (mount points / middleware chain) — likely a new or restructured HTTP resource",
        "api_mounts",
    ),
]

# Packages whose changes are likely behavior-bearing and "should document"
# (medium confidence). Not an exhaustive include list — see fallback rule.
SHOULD_DOCUMENT_PKGS = {
    "agents",
    "sessions",
    "files",
    "memory",
    "environments",
    "skills",
    "vaults",
    "deployments",
    "webhooks",
    "batches",
    "models",
    "platformapi",
    "workbench",
    "codesessions",
    "platformsession",
}

# Packages that are infra/transport plumbing; their changes are usually exclude
# but we cannot be sure without the diff, so these route to needs_review with a
# clear reason rather than silent exclude.
PLUMBING_PKGS = {
    "api",
    "httpapi",
    "config",
    "platformauth",
    "auth",
    "admin",
    "platform",
    "storage",
    "db",
    "runtime",
}

# Keyword signals (optional, from diff). If a should_document/needs_review file
# also contains these keywords, bump toward must_document.
MUST_DOCUMENT_KEYWORDS = [
    "Authorize",
    "permission",
    "Permission",
    "state machine",
    "stateMachine",
    "outbox",
    "Outbox",
]


@dataclass
class FileClassification:
    path: str
    action: str
    confidence: str
    reason: str
    surface_hint: str | None = None
    signals: list[str] = field(default_factory=list)


@dataclass
class Verdict:
    action: str
    reason: str
    must_document: list[str] = field(default_factory=list)
    should_document: list[str] = field(default_factory=list)
    needs_review: list[str] = field(default_factory=list)
    excluded: int = 0


_PKG_FROM_INTERNAL = re.compile(r"^internal/([a-z0-9_]+)/")


def _internal_pkg(path: str) -> str | None:
    m = _PKG_FROM_INTERNAL.match(path)
    return m.group(1) if m else None


def classify_file(path: str, diff_keywords: set[str] | None = None) -> FileClassification:
    """Classify a single changed file. Never silently guesses."""
    signals: list[str] = []
    diff_keywords = diff_keywords or set()

    # 1. Explicit-exclude path patterns (highest confidence, checked first).
    for pattern, label in EXCLUDE_PATH_PATTERNS:
        if pattern.search(path):
            signals.append(f"exclude-path:{label}")
            return FileClassification(
                path=path,
                action="exclude",
                confidence="high",
                reason=f"{label} — no design-doc consequence",
                signals=signals,
            )

    # 2. Must-document path rules (AGENTS.md named categories).
    for pattern, reason, hint in MUST_DOCUMENT_PATH_RULES:
        if pattern.search(path):
            signals.append(f"must-document-path:{hint}")
            return FileClassification(
                path=path,
                action="must_document",
                confidence="high",
                reason=reason,
                surface_hint=hint,
                signals=signals,
            )

    pkg = _internal_pkg(path)

    # 3. Explicit exclude packages (infra/config).
    if pkg and pkg in EXCLUDE_PKG_PATTERNS:
        signals.append(f"exclude-pkg:{pkg}")
        return FileClassification(
            path=path,
            action="exclude",
            confidence="high",
            reason=f"`internal/{pkg}` is infra/config plumbing — no behavior contract",
            surface_hint="packages",
            signals=signals,
        )

    # 4. Should-document packages (behavior-bearing, medium confidence).
    if pkg and pkg in SHOULD_DOCUMENT_PKGS:
        signals.append(f"should-document-pkg:{pkg}")
        # Keyword bump: if diff keywords include permission/state-machine/outbox,
        # this is closer to must_document.
        matched_kw = [kw for kw in MUST_DOCUMENT_KEYWORDS if kw in diff_keywords]
        if matched_kw:
            signals.append(f"keyword-bump:{','.join(matched_kw)}")
            return FileClassification(
                path=path,
                action="must_document",
                confidence="medium",
                reason=f"`internal/{pkg}` changed with keyword(s) {matched_kw} suggesting a contract/boundary change",
                surface_hint="packages",
                signals=signals,
            )
        return FileClassification(
            path=path,
            action="should_document",
            confidence="medium",
            reason=f"`internal/{pkg}` is a behavior-bearing package — verify whether behavior/contract changed",
            surface_hint="packages",
            signals=signals,
        )

    # 5. Plumbing packages: cannot classify from path alone → needs_review.
    if pkg and pkg in PLUMBING_PKGS:
        signals.append(f"plumbing-pkg:{pkg}")
        return FileClassification(
            path=path,
            action="needs_review",
            confidence="low",
            reason=(
                f"`internal/{pkg}` is transport/infra plumbing; path alone cannot tell if "
                f"behavior changed — inspect the diff (route registration, error mapping, "
                f"or auth entry changes still need docs)"
            ),
            surface_hint="packages",
            signals=signals,
        )

    # 6. Other internal files not matched by any package bucket.
    #    A new/renamed package under internal/ is usually a real code surface
    #    (e.g. demowidgets). Default to should_document (medium) so the agent
    #    verifies whether behavior changed, rather than silently excluding it.
    #    This is the anti-drop guardrail: unknown internal packages get human
    #    attention via should_document, never silent exclude.
    if pkg:
        signals.append(f"unclassified-internal-pkg:{pkg}")
        # Keyword bump applies here too.
        matched_kw = [kw for kw in MUST_DOCUMENT_KEYWORDS if kw in diff_keywords]
        if matched_kw:
            signals.append(f"keyword-bump:{','.join(matched_kw)}")
            return FileClassification(
                path=path,
                action="must_document",
                confidence="medium",
                reason=(
                    f"`internal/{pkg}` is a new/unclassified package and the diff contains "
                    f"keyword(s) {matched_kw} suggesting a contract/boundary change"
                ),
                surface_hint="packages",
                signals=signals,
            )
        return FileClassification(
            path=path,
            action="should_document",
            confidence="medium",
            reason=(
                f"`internal/{pkg}` is a new/unclassified package — verify whether it adds "
                f"or changes a behavior/API/contract that docs/design/ should record"
            ),
            surface_hint="packages",
            signals=signals,
        )

    # 7. Non-internal, non-excluded file (root-level source, web/, etc.).
    signals.append("unclassified-root-or-web")
    # FE routes are a tracked surface; a router change is at least should_document.
    if path.startswith("web/src/app/router") or path.startswith("web/src/app/"):
        signals.append("fe-route-area")
        return FileClassification(
            path=path,
            action="should_document",
            confidence="medium",
            reason="web app route / page changed — verify whether an FE contract changed",
            surface_hint="fe_routes",
            signals=signals,
        )

    return FileClassification(
        path=path,
        action="needs_review",
        confidence="low",
        reason="file is outside known surface areas and not matched by any exclude rule — inspect to decide",
        signals=signals,
    )


def aggregate(files: list[FileClassification]) -> Verdict:
    must = [f.path for f in files if f.action == "must_document"]
    should = [f.path for f in files if f.action == "should_document"]
    review = [f.path for f in files if f.action == "needs_review"]
    excluded = sum(1 for f in files if f.action == "exclude")
    if must:
        return Verdict(
            action="must_document",
            reason=f"PR touches {len(must)} must_document file(s)",
            must_document=must,
            should_document=should,
            needs_review=review,
            excluded=excluded,
        )
    if should:
        return Verdict(
            action="must_document",
            reason=f"PR touches {len(should)} should_document file(s) — verify, default to documenting if behavior changed",
            must_document=must,
            should_document=should,
            needs_review=review,
            excluded=excluded,
        )
    if review:
        return Verdict(
            action="needs_review",
            reason=f"{len(review)} file(s) could not be classified by rules — human/agent must decide",
            must_document=must,
            should_document=should,
            needs_review=review,
            excluded=excluded,
        )
    return Verdict(
        action="exclude",
        reason="all changed files are exclude (CI/test/config/docs/infra)",
        must_document=must,
        should_document=should,
        needs_review=review,
        excluded=excluded,
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--files",
        nargs="*",
        default=[],
        help="Changed file paths (space-separated). If omitted, reads newline-separated paths from stdin.",
    )
    parser.add_argument(
        "--keywords",
        nargs="*",
        default=[],
        help="Optional diff keywords that bump should_document toward must_document.",
    )
    parser.add_argument("--output", type=Path, help="Write JSON result to file.")
    parser.add_argument(
        "--fail-on-review",
        action="store_true",
        help="Exit 1 if any file is needs_review (useful for CI guardrails).",
    )
    args = parser.parse_args(argv)

    files_in = list(args.files)
    if not files_in and not sys.stdin.isatty():
        files_in = [line.strip() for line in sys.stdin if line.strip()]

    keywords = set(args.keywords)
    file_cls = [classify_file(p, keywords) for p in files_in]
    verdict = aggregate(file_cls)

    result: dict[str, Any] = {
        "files": [asdict(f) for f in file_cls],
        "verdict": asdict(verdict),
    }

    # Human-readable summary.
    print("CHANGE CLASSIFICATION")
    print("=" * 72)
    print(f"  verdict: {verdict.action} — {verdict.reason}")
    print(f"  must_document={len(verdict.must_document)} "
          f"should_document={len(verdict.should_document)} "
          f"needs_review={len(verdict.needs_review)} "
          f"excluded={verdict.excluded}")
    if verdict.must_document:
        print("\n  must_document:")
        for p in verdict.must_document:
            print(f"    - {p}")
    if verdict.should_document:
        print("\n  should_document:")
        for p in verdict.should_document:
            print(f"    - {p}")
    if verdict.needs_review:
        print("\n  needs_review:")
        for p in verdict.needs_review:
            print(f"    - {p}")

    if args.output:
        args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    if args.fail_on_review and verdict.needs_review:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

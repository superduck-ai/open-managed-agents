#!/usr/bin/env python3
"""Audit design-doc coverage against code surfaces.

Extracts:
  - API mounts from internal/api/server.go (+ codesessions RegisterRoutes prefixes)
  - Go packages under internal/
  - SQL migrations under internal/db/migrations/
  - FE routes from web/src/app/router.tsx

Compares them to scripts/docs-audit/surface_map.md and fails loud when:
  - a parser returns fewer surfaces than EXTRACTION_FLOORS (exit 2)
  - a surface escapes every accountability bucket (exit 2)
  - coverage findings exist (exit 1) — CI may soft-fail this initially
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parent.parent
SURFACE_MAP_PATH = SCRIPT_DIR / "surface_map.md"
DEFAULT_SNAPSHOT_PATH = SCRIPT_DIR / "surface_snapshot.json"

SNAPSHOT_SCHEMA_VERSION = 1

EXTRACTION_FLOORS = {
    "api_mounts": 10,
    "api_subroutes": 10,
    "packages": 15,
    "migrations": 5,
    "fe_routes": 20,
    "event_contracts": 10,
    "auth_middleware": 3,
}

SECTION_KEYS = {
    "api_mounts": ("## API mounts", "api_mounts"),
    "api_subroutes": ("## API subroutes", "api_subroutes"),
    "packages": ("## Packages", "packages"),
    "migrations": ("## Migrations", "migrations"),
    "fe_routes": ("## FE routes", "fe_routes"),
    "event_contracts": ("## Event contracts", "event_contracts"),
    "auth_middleware": ("## Auth middleware", "auth_middleware"),
    "unlisted": ("## Unlisted design docs", "unlisted"),
}


@dataclass
class Finding:
    surface_type: str
    surface_id: str
    kind: str
    message: str
    severity: str = "high"


@dataclass
class AuditResult:
    extracted: dict[str, list[str]] = field(default_factory=dict)
    map_entries: dict[str, dict[str, str]] = field(default_factory=dict)
    unlisted: set[str] = field(default_factory=set)
    duplicates: list[tuple[str, str]] = field(default_factory=list)
    findings: list[Finding] = field(default_factory=list)
    accounting: dict[str, dict[str, int]] = field(default_factory=dict)
    audits_skipped: list[str] = field(default_factory=list)
    exit_code: int = 0


def parse_surface_map(path: Path) -> tuple[dict[str, dict[str, str]], set[str], list[tuple[str, str]]]:
    mappings: dict[str, dict[str, str]] = {
        "api_mounts": {},
        "api_subroutes": {},
        "packages": {},
        "migrations": {},
        "fe_routes": {},
        "event_contracts": {},
        "auth_middleware": {},
    }
    unlisted: set[str] = set()
    duplicates: list[tuple[str, str]] = []
    if not path.exists():
        return mappings, unlisted, duplicates

    current: str | None = None
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line:
            continue
        if line.startswith("#"):
            for _label, (heading, key) in SECTION_KEYS.items():
                if line.startswith(heading):
                    current = key
                    break
            continue
        if current is None:
            continue
        if current == "unlisted":
            if line in unlisted:
                duplicates.append(("Unlisted design docs", line))
            unlisted.add(line)
            continue
        if " -> " not in line:
            continue
        key, target = line.split(" -> ", 1)
        key, target = key.strip(), target.strip()
        bucket = mappings[current]
        if key in bucket:
            duplicates.append((current, key))
        bucket[key] = target
    return mappings, unlisted, duplicates


def gated_reason(target: str) -> str | None:
    if target.startswith("gated:"):
        return target[len("gated:") :].strip() or "unspecified"
    return None


def is_sentinel(target: str) -> bool:
    return target == "internal" or gated_reason(target) is not None


_MOUNT_RE = re.compile(r"""\.Mount\(\s*"([^"]+)"\s*,""")
_POST_PATH_RE = re.compile(r"""\.Post\(\s*"([^"]+)"\s*,""")
_CODE_SESSION_PREFIX_RE = re.compile(
    r"""router\.(?:Get|Post|Put|HandleFunc)\(\s*"(/(?:v1/code/sessions)[^"]*)"\s*,"""
)
_FE_PATH_RE = re.compile(r"""^\s*path:\s*['"]([^'"]+)['"]\s*,?\s*$""", re.MULTILINE)


def extract_api_mounts(server_go: Path, codesessions_go: Path | None = None) -> list[str]:
    surfaces: set[str] = set()
    if not server_go.exists():
        return []
    text = server_go.read_text(encoding="utf-8")
    block_match = re.search(
        r"func \(s \*Server\) mountSharedV1Resources\(r chi\.Router\) \{(?P<body>.*?\n)\}",
        text,
        re.DOTALL,
    )
    block = block_match.group("body") if block_match else text
    for match in _MOUNT_RE.finditer(block):
        surfaces.add(match.group(1))
    for match in _POST_PATH_RE.finditer(block):
        surfaces.add(match.group(1))
    if 'router.Get("/healthz"' in text or '.Get("/healthz"' in text:
        surfaces.add("/healthz")
    if codesessions_go and codesessions_go.exists():
        cs_text = codesessions_go.read_text(encoding="utf-8")
        if _CODE_SESSION_PREFIX_RE.search(cs_text) or "/v1/code/sessions/" in cs_text:
            surfaces.add("/v1/code/sessions")
    return sorted(surfaces)


def extract_packages(internal_dir: Path) -> list[str]:
    if not internal_dir.is_dir():
        return []
    packages: list[str] = []
    for child in sorted(internal_dir.iterdir()):
        if not child.is_dir() or child.name.startswith("."):
            continue
        if any(child.glob("*.go")):
            packages.append(child.name)
    return packages


def extract_migrations(migrations_dir: Path) -> list[str]:
    if not migrations_dir.is_dir():
        return []
    return sorted(p.name for p in migrations_dir.glob("*.sql"))


def extract_fe_routes(router_tsx: Path) -> list[str]:
    if not router_tsx.exists():
        return []
    return sorted(set(_FE_PATH_RE.findall(router_tsx.read_text(encoding="utf-8"))))


# --- api_subroutes ---------------------------------------------------------
# Each resource package exposes its routes through Register*Routes entry points
# (e.g. platformapi.RegisterPlatformBillingRoutes). These are the stable unit of
# "this package contributes an HTTP resource" — more stable than scanning every
# nested r.Get/r.Post, which churns on path refactors.
#
# We prefer the *call-site* prefix (the package qualifier at the mount point,
# e.g. `platformapi.RegisterPlatformBillingRoutes`) over the definition file's
# own package, because the call-site prefix is how the resource is named in
# server.go wiring and is what a reader maps to a design doc area. When a
# registration is only ever called unqualified (same package), we fall back to
# the definition file's package name.
_REGISTER_CALL_RE = re.compile(
    r"""(?P<prefix>[A-Za-z_]\w*)\.(?P<name>Register(?:[A-Za-z]\w*)?Routes)\s*\("""
)
_REGISTER_DEF_RE = re.compile(
    r"""^func\s+(?:\([^)]*\)\s+)?(?P<name>Register(?:[A-Za-z]\w*)?Routes)\s*\(""",
    re.MULTILINE,
)


def extract_api_subroutes(internal_dir: Path) -> list[str]:
    """Collect Register*Routes entry points across resource packages.

    A surface is "<package>.<RegisterName>", e.g. "platformapi.RegisterPlatformBillingRoutes".
    The defining package (from `func Register*Routes` / `func (s *T) Register*Routes`)
    is the source of truth for the prefix — it is always the real owning package.
    Call-site prefixes (`pkg.Register*`, `s.files.Register*`, `codeSessionService.Register*`)
    are only used to *discover* which registrations exist, not to name them, because
    call-site qualifiers can be variable names rather than package names.
    """
    if not internal_dir.is_dir():
        return []
    call_sites: set[str] = set()  # names seen at a call site
    defined: dict[str, str] = {}  # name -> defining package (source of truth)
    for go_file in sorted(internal_dir.rglob("*.go")):
        if go_file.name.endswith("_test.go"):
            continue
        text = go_file.read_text(encoding="utf-8")
        pkg_match = re.search(r"^package\s+(\w+)", text, re.MULTILINE)
        pkg = pkg_match.group(1) if pkg_match else go_file.parent.name
        for m in _REGISTER_CALL_RE.finditer(text):
            call_sites.add(m.group("name"))
        for m in _REGISTER_DEF_RE.finditer(text):
            defined[m.group("name")] = pkg
    names = set(defined) | call_sites
    surfaces: set[str] = set()
    for name in names:
        # Prefer the defining package; fall back to a bare name for registrations
        # we only ever saw called (no in-repo definition, e.g. interface-satisfied).
        prefix = defined.get(name)
        if prefix:
            surfaces.add(f"{prefix}.{name}")
        else:
            surfaces.add(name)
    return sorted(surfaces)


# --- event_contracts -------------------------------------------------------
# Event types are string literals switched on in CategoryFor/IsClientInput/etc.
# in internal/managedagentsevents/events.go. These literal strings ARE the event
# contract; adding/removing/renaming one is an AGENTS.md "event contract change".
_EVENT_TYPE_LITERAL_RE = re.compile(r'"([a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+)"')


def extract_event_contracts(events_go: Path | None) -> list[str]:
    """Extract event-type string literals from the managed agent events package.

    Falls back to scanning the whole package dir if the exact file moves. Returns
    the sorted unique event-type strings (e.g. "user.message", "session.status_running").
    """
    search_files: list[Path] = []
    events_pkg = events_go.parent if events_go and events_go.exists() else None
    if events_pkg is None:
        return []
    for go_file in sorted(events_pkg.glob("*.go")):
        if go_file.name.endswith("_test.go"):
            continue
        search_files.append(go_file)
    surfaces: set[str] = set()
    for f in search_files:
        text = f.read_text(encoding="utf-8")
        for m in _EVENT_TYPE_LITERAL_RE.finditer(text):
            surfaces.add(m.group(1))
    return sorted(surfaces)


# --- auth_middleware -------------------------------------------------------
# Auth/middleware surfaces are the (s *Server) xxxMiddleware definitions plus the
# router.Use(xxxMiddleware) call sites in server.go. A new middleware or a changed
# .Use(...) chain is an AGENTS.md "permission boundary change".
_MIDDLEWARE_DEF_RE = re.compile(
    r"""func\s+\([^)]*\)\s+([A-Za-z]\w*[Mm]iddleware)\s*\("""
)
_MIDDLEWARE_USE_RE = re.compile(r"""\.Use\(\s*([A-Za-z_][\w.]*)\s*\)""")


def extract_auth_middleware(server_go: Path) -> list[str]:
    """Collect auth/middleware surfaces from server.go.

    A surface is a middleware identifier that appears in either a definition
    (`func (s *Server) xxxMiddleware`) or a `.Use(...)` call. Non-auth middleware
    (requestIDMiddleware, recoverMiddleware, requestLoggingMiddleware) is included
    too because the middleware chain is a documented platform boundary; the map
    decides whether each one needs a design doc.
    """
    if not server_go.exists():
        return []
    text = server_go.read_text(encoding="utf-8")
    surfaces: set[str] = set()
    for m in _MIDDLEWARE_DEF_RE.finditer(text):
        surfaces.add(m.group(1))
    for m in _MIDDLEWARE_USE_RE.finditer(text):
        # Only keep identifiers that look like middleware (end with Middleware or
        # are s.xxxMiddleware call expressions) to avoid matching generic .Use(cb).
        ident = m.group(1)
        if ident.endswith("Middleware"):
            # s.platformAuthMiddleware -> platformAuthMiddleware
            surfaces.add(ident.split(".")[-1])
    return sorted(surfaces)


def extract_all(repo: Path) -> dict[str, list[str]]:
    return {
        "api_mounts": extract_api_mounts(
            repo / "internal" / "api" / "server.go",
            repo / "internal" / "codesessions" / "ingress.go",
        ),
        "api_subroutes": extract_api_subroutes(repo / "internal"),
        "packages": extract_packages(repo / "internal"),
        "migrations": extract_migrations(repo / "internal" / "db" / "migrations"),
        "fe_routes": extract_fe_routes(repo / "web" / "src" / "app" / "router.tsx"),
        "event_contracts": extract_event_contracts(
            repo / "internal" / "managedagentsevents" / "events.go"
        ),
        "auth_middleware": extract_auth_middleware(repo / "internal" / "api" / "server.go"),
    }


def doc_exists(repo: Path, target: str) -> bool:
    if is_sentinel(target):
        return True
    path = repo / target
    if path.exists():
        return True
    if target.endswith(".md") and (repo / (target + "x")).exists():
        return True
    if target.endswith(".mdx") and (repo / target[:-1]).exists():
        return True
    return False


def account_surface(
    surface_type: str,
    surface_id: str,
    target: str | None,
    repo: Path,
    findings: list[Finding],
    buckets: dict[str, int],
) -> None:
    if target is None:
        findings.append(
            Finding(
                surface_type,
                surface_id,
                "unmapped",
                f"{surface_type} `{surface_id}` is not in surface_map.md — "
                f"add `-> docs/design/...md`, `-> internal`, or `-> gated:<reason>`",
            )
        )
        buckets["finding"] += 1
        return
    if target == "internal":
        buckets["internal"] += 1
        return
    if gated_reason(target) is not None:
        buckets["gated"] += 1
        return
    if not doc_exists(repo, target):
        findings.append(
            Finding(
                surface_type,
                surface_id,
                "missing_doc",
                f"{surface_type} `{surface_id}` maps to `{target}` but that file does not exist",
            )
        )
        buckets["finding"] += 1
        return
    buckets["mapped"] += 1


def run_map_hygiene(
    extracted: dict[str, list[str]],
    mappings: dict[str, dict[str, str]],
    unlisted: set[str],
    duplicates: list[tuple[str, str]],
    repo: Path,
    findings: list[Finding],
) -> None:
    for section, key in duplicates:
        findings.append(
            Finding(
                "map",
                key,
                "duplicate",
                f"duplicate surface_map entry in {section}: `{key}`",
                severity="medium",
            )
        )
    for surface_type, mapping in mappings.items():
        live = set(extracted.get(surface_type, []))
        for surface_id, target in mapping.items():
            if surface_id not in live:
                findings.append(
                    Finding(
                        "map",
                        surface_id,
                        "dead_entry",
                        f"surface_map `{surface_type}` entry `{surface_id}` no longer exists in code — prune it",
                        severity="medium",
                    )
                )
            if not is_sentinel(target) and not doc_exists(repo, target):
                findings.append(
                    Finding(
                        "map",
                        surface_id,
                        "dead_doc_target",
                        f"surface_map `{surface_type}` `{surface_id}` points at missing `{target}`",
                    )
                )
    for doc in sorted(unlisted):
        if not doc_exists(repo, doc):
            findings.append(
                Finding(
                    "map",
                    doc,
                    "dead_unlisted",
                    f"unlisted design doc `{doc}` does not exist — prune it",
                    severity="medium",
                )
            )

    # Staleness (reverse drift): design docs that still reference a package
    # that no longer exists in code. Catches renames/removals that the forward
    # coverage audit cannot see (it only knows about surfaces that *currently*
    # exist). We scan docs/design/** prose for `internal/<pkg>` references and
    # flag any whose <pkg> is not a live package directory.
    run_staleness_check(extracted, repo, findings)


# `internal/<pkg>` reference in design-doc prose. Word boundary on the right so
# we don't match `internal/db/migrations` as pkg `db/migrations` — we capture
# only the first path segment and verify it separately.
_INTERNAL_PKG_REF_RE = re.compile(r"""internal/([a-z][a-z0-9_]*)""")


def run_staleness_check(
    extracted: dict[str, list[str]],
    repo: Path,
    findings: list[Finding],
) -> None:
    live_packages = set(extracted.get("packages", []))
    docs_dir = repo / "docs" / "design"
    if not docs_dir.is_dir() or not live_packages:
        return
    # doc path -> set of stale package refs it mentions (dedupe per doc).
    stale_by_doc: dict[str, set[str]] = {}
    for doc in sorted(docs_dir.rglob("*.md")):
        text = doc.read_text(encoding="utf-8")
        refs = set(_INTERNAL_PKG_REF_RE.findall(text))
        for pkg in refs:
            if pkg not in live_packages:
                rel = str(doc.relative_to(repo))
                stale_by_doc.setdefault(rel, set()).add(pkg)
    for doc_rel in sorted(stale_by_doc):
        for pkg in sorted(stale_by_doc[doc_rel]):
            findings.append(
                Finding(
                    "staleness",
                    doc_rel,
                    "stale_pkg_ref",
                    f"`{doc_rel}` references `internal/{pkg}` but that package no longer exists — "
                    f"update the doc or confirm the rename",
                    severity="medium",
                )
            )


def audit_coverage(repo: Path, map_path: Path) -> AuditResult:
    result = AuditResult()
    result.extracted = extract_all(repo)
    mappings, unlisted, duplicates = parse_surface_map(map_path)
    result.map_entries = mappings
    result.unlisted = unlisted
    result.duplicates = duplicates

    for label, floor in EXTRACTION_FLOORS.items():
        count = len(result.extracted.get(label, []))
        if count < floor:
            result.audits_skipped.append(f"extraction:{label}")
            result.findings.append(
                Finding(
                    "extraction",
                    label,
                    "floor",
                    f"only {count} {label} extracted (expected >= {floor}) — "
                    f"parser likely broken or source layout changed",
                )
            )

    accounting: dict[str, dict[str, int]] = {}
    for surface_type, items in result.extracted.items():
        buckets: dict[str, int] = defaultdict(int)
        mapping = mappings.get(surface_type, {})
        for surface_id in items:
            account_surface(
                surface_type,
                surface_id,
                mapping.get(surface_id),
                repo,
                result.findings,
                buckets,
            )
        accounting[surface_type] = dict(buckets)
        accounted = sum(buckets.values())
        if accounted != len(items):
            result.audits_skipped.append(f"integrity:accounting:{surface_type}")
            result.findings.append(
                Finding(
                    "integrity",
                    surface_type,
                    "accounting",
                    f"{surface_type}: accounted {accounted} of {len(items)} — audit logic regressed",
                )
            )

    result.accounting = accounting
    run_map_hygiene(result.extracted, mappings, unlisted, duplicates, repo, result.findings)

    if any(f.kind in {"floor", "accounting"} for f in result.findings) or any(
        s.startswith("extraction:") or s.startswith("integrity:") for s in result.audits_skipped
    ):
        result.exit_code = 2
    elif result.findings:
        result.exit_code = 1
    else:
        result.exit_code = 0
    return result


def build_snapshot(extracted: dict[str, list[str]]) -> dict[str, Any]:
    return {
        "schema_version": SNAPSHOT_SCHEMA_VERSION,
        "surfaces": {k: sorted(v) for k, v in sorted(extracted.items())},
    }


def diff_snapshots(old: dict[str, Any], new: dict[str, Any]) -> list[Finding]:
    findings: list[Finding] = []
    old_surfaces = old.get("surfaces", {})
    new_surfaces = new.get("surfaces", {})
    for surface_type in sorted(set(old_surfaces) | set(new_surfaces)):
        if surface_type not in old_surfaces:
            findings.append(
                Finding("diff", surface_type, "surface_type_added", f"surface type `{surface_type}` newly tracked", "low")
            )
            continue
        if surface_type not in new_surfaces:
            findings.append(
                Finding(
                    "diff",
                    surface_type,
                    "surface_type_removed",
                    f"surface type `{surface_type}` no longer tracked",
                    "medium",
                )
            )
            continue
        old_set = set(old_surfaces[surface_type])
        new_set = set(new_surfaces[surface_type])
        for item in sorted(new_set - old_set):
            findings.append(
                Finding("diff", item, "added", f"added {surface_type} `{item}` — map it or mark internal/gated")
            )
        for item in sorted(old_set - new_set):
            findings.append(
                Finding(
                    "diff",
                    item,
                    "removed",
                    f"removed {surface_type} `{item}` — prune surface_map and verify docs",
                    "medium",
                )
            )
    return findings


def print_report(result: AuditResult, diff_findings: list[Finding] | None = None) -> None:
    print("DESIGN DOC SURFACE AUDIT")
    print("=" * 72)
    for surface_type, items in result.extracted.items():
        print(f"  {surface_type}: {len(items)}")
    print()
    if result.accounting:
        print("COMPLETENESS ACCOUNTING (every item in exactly one bucket)")
        print("-" * 72)
        for surface_type, buckets in result.accounting.items():
            parts = ", ".join(f"{k}={v}" for k, v in sorted(buckets.items()))
            print(f"  {surface_type}: {parts or '(empty)'}")
        print()
    if result.audits_skipped:
        print("AUDITS SKIPPED / INTEGRITY")
        print("-" * 72)
        for item in result.audits_skipped:
            print(f"  - {item}")
        print()
    all_findings = list(result.findings)
    if diff_findings:
        all_findings.extend(diff_findings)
    if not all_findings:
        print("No findings.")
        return
    print(f"FINDINGS ({len(all_findings)})")
    print("-" * 72)
    by_sev: dict[str, list[Finding]] = defaultdict(list)
    for f in all_findings:
        by_sev[f.severity].append(f)
    for severity in ("high", "medium", "low"):
        for f in by_sev.get(severity, []):
            print(f"  [{severity}] {f.surface_type}/{f.kind}: {f.message}")


def result_to_json(result: AuditResult, diff_findings: list[Finding] | None = None) -> dict[str, Any]:
    findings = [
        {
            "surface_type": f.surface_type,
            "surface_id": f.surface_id,
            "kind": f.kind,
            "severity": f.severity,
            "message": f.message,
        }
        for f in result.findings
    ]
    if diff_findings:
        findings.extend(
            {
                "surface_type": f.surface_type,
                "surface_id": f.surface_id,
                "kind": f.kind,
                "severity": f.severity,
                "message": f.message,
            }
            for f in diff_findings
        )
    return {
        "extracted": result.extracted,
        "accounting": result.accounting,
        "audits_skipped": result.audits_skipped,
        "findings": findings,
        "exit_code": result.exit_code,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=REPO_ROOT)
    parser.add_argument("--map", type=Path, default=SURFACE_MAP_PATH)
    parser.add_argument("--snapshot", type=Path, default=DEFAULT_SNAPSHOT_PATH)
    parser.add_argument("--diff", action="store_true")
    parser.add_argument("--update-snapshot", action="store_true")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--list-extracted", action="store_true")
    args = parser.parse_args(argv)

    repo = args.repo.resolve()
    if args.list_extracted:
        print(json.dumps(extract_all(repo), indent=2, sort_keys=True))
        return 0

    result = audit_coverage(repo, args.map.resolve())
    snapshot = build_snapshot(result.extracted)

    diff_findings: list[Finding] = []
    if args.diff:
        if not args.snapshot.exists():
            print(f"snapshot missing: {args.snapshot}", file=sys.stderr)
            return 2
        old = json.loads(args.snapshot.read_text(encoding="utf-8"))
        diff_findings = diff_snapshots(old, snapshot)
        if diff_findings and result.exit_code == 0:
            result.exit_code = 1

    if args.update_snapshot:
        args.snapshot.write_text(json.dumps(snapshot, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        print(f"updated snapshot: {args.snapshot}")

    print_report(result, diff_findings if args.diff else None)
    if args.output:
        args.output.write_text(
            json.dumps(result_to_json(result, diff_findings if args.diff else None), indent=2) + "\n",
            encoding="utf-8",
        )
    return result.exit_code


if __name__ == "__main__":
    sys.exit(main())

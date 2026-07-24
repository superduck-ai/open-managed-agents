#!/usr/bin/env python3
"""Unit tests for design-doc surface audit (stdlib only)."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from audit_design_docs import (
    EXTRACTION_FLOORS,
    audit_coverage,
    build_snapshot,
    diff_snapshots,
    extract_api_mounts,
    extract_api_subroutes,
    extract_auth_middleware,
    extract_event_contracts,
    extract_fe_routes,
    extract_migrations,
    extract_packages,
    parse_surface_map,
)


class ParseSurfaceMapTests(unittest.TestCase):
    def test_parses_sections_and_sentinels(self) -> None:
        text = """# title
## API mounts -> design docs
/agents -> docs/design/be/agents.md
/models -> internal
/skills -> gated:soon

## Packages -> design docs
agents -> docs/design/be/agents.md
api -> internal

## Migrations -> design docs
00001_init.sql -> internal

## FE routes -> design docs
login -> internal
sessions -> docs/design/fe/sessions.md

## Unlisted design docs
docs/design/extra.md
"""
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "map.md"
            path.write_text(text, encoding="utf-8")
            mappings, unlisted, duplicates = parse_surface_map(path)
        self.assertEqual(mappings["api_mounts"]["/agents"], "docs/design/be/agents.md")
        self.assertEqual(mappings["api_mounts"]["/models"], "internal")
        self.assertEqual(mappings["api_mounts"]["/skills"], "gated:soon")
        self.assertEqual(unlisted, {"docs/design/extra.md"})
        self.assertEqual(duplicates, [])

    def test_duplicate_keys(self) -> None:
        text = """## API mounts -> design docs
/agents -> internal
/agents -> gated:x
"""
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "map.md"
            path.write_text(text, encoding="utf-8")
            mappings, _unlisted, duplicates = parse_surface_map(path)
        self.assertEqual(mappings["api_mounts"]["/agents"], "gated:x")
        self.assertEqual(duplicates, [("api_mounts", "/agents")])


class ExtractorTests(unittest.TestCase):
    def test_extract_api_mounts_from_fixture(self) -> None:
        server = """
package api
func (s *Server) New() { router.Get("/healthz", ok) }
func (s *Server) mountSharedV1Resources(r chi.Router) {
	r.Post("/agents:search", s.agents.Search)
	r.Mount("/agents", s.agents)
	r.Mount("/sessions", s.sessions)
}
"""
        codesessions = """
package codesessions
func (s *Service) RegisterRoutes(router chi.Router) {
	router.Get("/v1/code/sessions/{code_session_id}", s.handle)
}
"""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            server_path = root / "server.go"
            cs_path = root / "ingress.go"
            server_path.write_text(server, encoding="utf-8")
            cs_path.write_text(codesessions, encoding="utf-8")
            mounts = extract_api_mounts(server_path, cs_path)
        self.assertIn("/agents", mounts)
        self.assertIn("/agents:search", mounts)
        self.assertIn("/healthz", mounts)
        self.assertIn("/v1/code/sessions", mounts)

    def test_extract_packages_migrations_routes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "agents").mkdir()
            (root / "agents" / "handler.go").write_text("package agents\n", encoding="utf-8")
            (root / "empty").mkdir()
            mig = root / "migrations"
            mig.mkdir()
            (mig / "00001_init.sql").write_text("--\n", encoding="utf-8")
            self.assertEqual(extract_packages(root), ["agents"])
            self.assertEqual(extract_migrations(mig), ["00001_init.sql"])
            router = root / "router.tsx"
            router.write_text("path: 'login',\npath: 'sessions',\n", encoding="utf-8")
            self.assertEqual(extract_fe_routes(router), ["login", "sessions"])

    def test_extract_api_subroutes_uses_defining_package(self) -> None:
        """Call-site variable prefixes (s.files..., codeSessionService...)
        must NOT become the surface name; the defining package wins."""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "platformapi").mkdir()
            (root / "platformapi" / "billing.go").write_text(
                "package platformapi\n"
                "func RegisterPlatformBillingRoutes(r chi.Router) {}\n",
                encoding="utf-8",
            )
            (root / "workbench").mkdir()
            (root / "workbench" / "support.go").write_text(
                "package workbench\n"
                "func (s *Service) RegisterOrgWorkbenchRoutes(r chi.Router) {}\n",
                encoding="utf-8",
            )
            # server.go wiring: package-qualified call + variable-qualified call
            (root / "server.go").write_text(
                "package api\n"
                "platformapi.RegisterPlatformBillingRoutes(r)\n"
                "codeSessionService.RegisterRoutes(r)\n"
                "s.files.RegisterPlatformRoutes(r)\n",
                encoding="utf-8",
            )
            surfaces = extract_api_subroutes(root)
        # Defining package is the source of truth.
        self.assertIn("platformapi.RegisterPlatformBillingRoutes", surfaces)
        self.assertIn("workbench.RegisterOrgWorkbenchRoutes", surfaces)
        # Call-site variable names must NOT leak into surface names.
        self.assertNotIn("codeSessionService.RegisterRoutes", surfaces)
        self.assertNotIn("s.RegisterPlatformRoutes", surfaces)

    def test_extract_event_contracts(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            events_go = root / "events.go"
            events_go.write_text(
                'package managedagentsevents\n'
                'func CategoryFor(et string) string {\n'
                '	switch et {\n'
                '	case "user.message", "user.interrupt":\n'
                '		return "input"\n'
                '	case "session.status_running":\n'
                '		return "session_status"\n'
                '	}\n'
                '}\n',
                encoding="utf-8",
            )
            events = extract_event_contracts(events_go)
        self.assertIn("user.message", events)
        self.assertIn("user.interrupt", events)
        self.assertIn("session.status_running", events)
        # Only dotted event-type literals are captured, not arbitrary strings.
        self.assertNotIn("input", events)

    def test_extract_auth_middleware(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            server_go = Path(tmp) / "server.go"
            server_go.write_text(
                "package api\n"
                "func (s *Server) serviceAuthMiddleware(next http.Handler) http.Handler {}\n"
                "func (s *Server) platformAuthMiddleware(next http.Handler) http.Handler {}\n"
                "func (s *Server) recoverMiddleware(next http.Handler) http.Handler {}\n"
                "func main() {\n"
                "	r.Use(s.serviceAuthMiddleware)\n"
                "	r.Use(s.platformAuthMiddleware)\n"
                "}\n",
                encoding="utf-8",
            )
            mws = extract_auth_middleware(server_go)
        self.assertIn("serviceAuthMiddleware", mws)
        self.assertIn("platformAuthMiddleware", mws)
        self.assertIn("recoverMiddleware", mws)


class AuditIntegrationTests(unittest.TestCase):
    def _mini_repo(self, root: Path) -> None:
        (root / "internal" / "api").mkdir(parents=True)
        (root / "internal" / "api" / "server.go").write_text(
            """
package api
func (s *Server) New() { router.Get("/healthz", ok) }
func (s *Server) mountSharedV1Resources(r chi.Router) {
	r.Mount("/agents", s.agents)
}
""",
            encoding="utf-8",
        )
        for name in [
            "admin", "agents", "auth", "batches", "cleanup", "codesessions", "config", "db",
            "deployments", "environments", "files", "httpapi", "ids", "memory", "models",
            "sessions", "skills", "storage", "vaults", "webhooks",
        ]:
            d = root / "internal" / name
            d.mkdir(exist_ok=True)
            (d / "x.go").write_text(f"package {name}\n", encoding="utf-8")
        (root / "internal" / "db" / "migrations").mkdir(parents=True, exist_ok=True)
        for i in range(1, 6):
            (root / "internal" / "db" / "migrations" / f"0000{i}_m.sql").write_text("--\n", encoding="utf-8")
        (root / "web" / "src" / "app").mkdir(parents=True)
        routes = "\n".join(f"  path: 'r{i}'," for i in range(25))
        (root / "web" / "src" / "app" / "router.tsx").write_text(
            "const x = createRoute({\n" + routes + "\n});\n",
            encoding="utf-8",
        )
        (root / "docs" / "design" / "be").mkdir(parents=True)
        (root / "docs" / "design" / "be" / "agents.md").write_text("# agents\n", encoding="utf-8")

    def test_unmapped_is_finding(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._mini_repo(root)
            map_path = root / "map.md"
            lines = ["## API mounts -> design docs", "/healthz -> internal", "## Packages -> design docs"]
            for pkg in extract_packages(root / "internal"):
                lines.append(f"{pkg} -> internal")
            lines.append("## Migrations -> design docs")
            for mig in extract_migrations(root / "internal" / "db" / "migrations"):
                lines.append(f"{mig} -> internal")
            lines.append("## FE routes -> design docs")
            for route in extract_fe_routes(root / "web" / "src" / "app" / "router.tsx"):
                lines.append(f"{route} -> internal")
            map_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
            import audit_design_docs as mod

            old = dict(mod.EXTRACTION_FLOORS)
            try:
                # mini-repo only has the classic 4 surface types populated; lower
                # those to 1 and zero out the new types (no source files for them).
                mod.EXTRACTION_FLOORS.update({
                    "api_mounts": 1, "packages": 1, "migrations": 1, "fe_routes": 1,
                    "api_subroutes": 0, "event_contracts": 0, "auth_middleware": 0,
                })
                result = audit_coverage(root, map_path)
            finally:
                mod.EXTRACTION_FLOORS.clear()
                mod.EXTRACTION_FLOORS.update(old)
            self.assertIn("unmapped", {f.kind for f in result.findings})
            self.assertEqual(result.exit_code, 1)

    def test_mapped_doc_ok(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._mini_repo(root)
            map_path = root / "map.md"
            lines = [
                "## API mounts -> design docs",
                "/healthz -> internal",
                "/agents -> docs/design/be/agents.md",
                "## Packages -> design docs",
            ]
            for pkg in extract_packages(root / "internal"):
                target = "docs/design/be/agents.md" if pkg == "agents" else "internal"
                lines.append(f"{pkg} -> {target}")
            lines.append("## Migrations -> design docs")
            for mig in extract_migrations(root / "internal" / "db" / "migrations"):
                lines.append(f"{mig} -> internal")
            lines.append("## FE routes -> design docs")
            for route in extract_fe_routes(root / "web" / "src" / "app" / "router.tsx"):
                lines.append(f"{route} -> internal")
            map_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
            import audit_design_docs as mod

            old = dict(mod.EXTRACTION_FLOORS)
            try:
                # mini-repo only has the classic 4 surface types populated; lower
                # those to 1 and zero out the new types (no source files for them).
                mod.EXTRACTION_FLOORS.update({
                    "api_mounts": 1, "packages": 1, "migrations": 1, "fe_routes": 1,
                    "api_subroutes": 0, "event_contracts": 0, "auth_middleware": 0,
                })
                result = audit_coverage(root, map_path)
            finally:
                mod.EXTRACTION_FLOORS.clear()
                mod.EXTRACTION_FLOORS.update(old)
            coverage = [f for f in result.findings if f.kind in {"unmapped", "missing_doc"}]
            self.assertEqual(coverage, [])
            self.assertEqual(result.exit_code, 0)

    def test_extraction_floor_fails_loud(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "internal" / "api").mkdir(parents=True)
            (root / "internal" / "api" / "server.go").write_text("package api\n", encoding="utf-8")
            (root / "internal" / "db" / "migrations").mkdir(parents=True)
            (root / "web" / "src" / "app").mkdir(parents=True)
            (root / "web" / "src" / "app" / "router.tsx").write_text("", encoding="utf-8")
            map_path = root / "map.md"
            map_path.write_text("## API mounts -> design docs\n", encoding="utf-8")
            result = audit_coverage(root, map_path)
            self.assertEqual(result.exit_code, 2)
            self.assertTrue(any(s.startswith("extraction:") for s in result.audits_skipped))

    def test_staleness_flags_dead_pkg_reference(self) -> None:
        """A design doc that references a package which no longer exists in
        code must produce a staleness finding."""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            # Real package on disk.
            (root / "internal" / "agents").mkdir(parents=True)
            (root / "internal" / "agents" / "x.go").write_text("package agents\n", encoding="utf-8")
            (root / "internal" / "db" / "migrations").mkdir(parents=True)
            (root / "internal" / "db" / "migrations" / "00001.sql").write_text("--\n", encoding="utf-8")
            (root / "web" / "src" / "app").mkdir(parents=True)
            (root / "web" / "src" / "app" / "router.tsx").write_text("path: 'r1',\n", encoding="utf-8")
            (root / "docs" / "design" / "be").mkdir(parents=True)
            # Doc references a live package (agents) AND a renamed/dead one (oldauth).
            (root / "docs" / "design" / "be" / "auth.md").write_text(
                "# Auth\n\nSee internal/agents and internal/oldauth.\n",
                encoding="utf-8",
            )
            map_path = root / "map.md"
            map_path.write_text(
                "## Packages -> design docs\n"
                "agents -> docs/design/be/auth.md\n"
                "## Migrations -> design docs\n"
                "## FE routes -> design docs\n"
                "## API mounts -> design docs\n",
                encoding="utf-8",
            )
            import audit_design_docs as mod
            old = dict(mod.EXTRACTION_FLOORS)
            try:
                mod.EXTRACTION_FLOORS.update({k: 0 for k in old})
                result = audit_coverage(root, map_path)
            finally:
                mod.EXTRACTION_FLOORS.clear()
                mod.EXTRACTION_FLOORS.update(old)
        kinds = {f.kind for f in result.findings}
        self.assertIn("stale_pkg_ref", kinds)
        stale = [f for f in result.findings if f.kind == "stale_pkg_ref"]
        self.assertTrue(any("oldauth" in f.message for f in stale))
        self.assertFalse(any("agents" in f.message for f in stale))


class SnapshotDiffTests(unittest.TestCase):
    def test_diff_added_removed(self) -> None:
        old = build_snapshot({"api_mounts": ["/agents"], "packages": ["agents"]})
        new = build_snapshot({"api_mounts": ["/agents", "/sessions"], "packages": ["agents"]})
        findings = diff_snapshots(old, new)
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0].kind, "added")

    def test_snapshot_roundtrip_json(self) -> None:
        snap = build_snapshot({"api_mounts": ["/a"], "packages": ["p"]})
        loaded = json.loads(json.dumps(snap))
        self.assertEqual(loaded["schema_version"], 1)


class FloorConstantTests(unittest.TestCase):
    def test_floors_are_positive(self) -> None:
        for key, value in EXTRACTION_FLOORS.items():
            self.assertGreater(value, 0, key)


if __name__ == "__main__":
    unittest.main()

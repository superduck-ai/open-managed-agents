#!/usr/bin/env python3
"""Unit tests for classify_changes (deterministic doc-need triage)."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from classify_changes import classify_file, aggregate, main


class ClassifyFileTests(unittest.TestCase):
    def test_ci_workflow_excluded_high(self) -> None:
        c = classify_file(".github/workflows/ci.yml")
        self.assertEqual(c.action, "exclude")
        self.assertEqual(c.confidence, "high")

    def test_test_file_excluded(self) -> None:
        c = classify_file("internal/sessions/transport_test.go")
        self.assertEqual(c.action, "exclude")
        self.assertEqual(c.confidence, "high")

    def test_design_doc_excluded(self) -> None:
        c = classify_file("docs/design/be/agents.md")
        self.assertEqual(c.action, "exclude")

    def test_event_contract_must_document(self) -> None:
        c = classify_file("internal/managedagentsevents/events.go")
        self.assertEqual(c.action, "must_document")
        self.assertEqual(c.confidence, "high")
        self.assertEqual(c.surface_hint, "event_contracts")

    def test_migration_must_document(self) -> None:
        c = classify_file("internal/db/migrations/00010_add_x.sql")
        self.assertEqual(c.action, "must_document")
        self.assertEqual(c.surface_hint, "migrations")

    def test_auth_middleware_must_document(self) -> None:
        c = classify_file("internal/api/auth.go")
        self.assertEqual(c.action, "must_document")
        self.assertEqual(c.surface_hint, "auth_middleware")

    def test_server_go_wiring_must_document(self) -> None:
        # server.go is the router wiring point; a change likely adds/restructures
        # a resource — must document, not needs_review.
        c = classify_file("internal/api/server.go")
        self.assertEqual(c.action, "must_document")
        self.assertEqual(c.surface_hint, "api_mounts")

    def test_behavior_pkg_should_document(self) -> None:
        c = classify_file("internal/sessions/transport.go")
        self.assertEqual(c.action, "should_document")
        self.assertEqual(c.confidence, "medium")

    def test_behavior_pkg_keyword_bumps_to_must(self) -> None:
        c = classify_file(
            "internal/sessions/transport.go", diff_keywords={"permission", "Authorize"}
        )
        self.assertEqual(c.action, "must_document")
        self.assertTrue(
            any(s.startswith("keyword-bump") for s in c.signals),
            f"expected a keyword-bump signal, got {c.signals}",
        )

    def test_infra_pkg_excluded(self) -> None:
        c = classify_file("internal/observability/metrics.go")
        self.assertEqual(c.action, "exclude")
        self.assertEqual(c.confidence, "high")

    def test_plumbing_pkg_needs_review(self) -> None:
        # httpapi is plumbing; path alone cannot decide → needs_review (no silent exclude).
        c = classify_file("internal/httpapi/write.go")
        self.assertEqual(c.action, "needs_review")
        self.assertEqual(c.confidence, "low")

    def test_unknown_internal_pkg_should_document_not_exclude(self) -> None:
        """Anti-drop guardrail: a new package like `demowidgets` must NOT be
        silently excluded — it routes to should_document so the agent verifies."""
        c = classify_file("internal/demowidgets/handler.go")
        self.assertEqual(c.action, "should_document")
        self.assertNotEqual(c.action, "exclude")

    def test_unknown_internal_pkg_keyword_bumps(self) -> None:
        c = classify_file("internal/newfeature/x.go", diff_keywords={"outbox"})
        self.assertEqual(c.action, "must_document")

    def test_fe_route_should_document(self) -> None:
        c = classify_file("web/src/app/routes/sessions.tsx")
        self.assertEqual(c.action, "should_document")
        self.assertEqual(c.surface_hint, "fe_routes")

    def test_unknown_root_file_needs_review(self) -> None:
        """Files outside every known area must not be silently excluded."""
        c = classify_file("some_random_root_file.xyz")
        self.assertEqual(c.action, "needs_review")


class AggregateTests(unittest.TestCase):
    def test_must_document_dominates(self) -> None:
        files = [
            classify_file(".github/workflows/ci.yml"),  # exclude
            classify_file("internal/db/migrations/0001.sql"),  # must
            classify_file("internal/sessions/x.go"),  # should
        ]
        v = aggregate(files)
        self.assertEqual(v.action, "must_document")
        self.assertEqual(len(v.must_document), 1)
        self.assertEqual(len(v.should_document), 1)
        self.assertEqual(v.excluded, 1)

    def test_should_document_dominates_when_no_must(self) -> None:
        files = [
            classify_file(".github/workflows/ci.yml"),  # exclude
            classify_file("internal/sessions/x.go"),  # should
        ]
        v = aggregate(files)
        self.assertEqual(v.action, "must_document")  # should → must at PR level
        self.assertIn("verify", v.reason)

    def test_needs_review_when_only_review(self) -> None:
        files = [classify_file("internal/httpapi/write.go")]  # plumbing → review
        v = aggregate(files)
        self.assertEqual(v.action, "needs_review")

    def test_all_exclude(self) -> None:
        files = [
            classify_file(".github/workflows/ci.yml"),
            classify_file("internal/sessions/x_test.go"),
        ]
        v = aggregate(files)
        self.assertEqual(v.action, "exclude")
        self.assertEqual(v.excluded, 2)


class CLITests(unittest.TestCase):
    def test_cli_writes_json_and_exits_zero(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "r.json"
            code = main([
                "--files",
                "internal/db/migrations/0001.sql",
                ".github/workflows/ci.yml",
                "--output",
                str(out),
            ])
            self.assertEqual(code, 0)
            data = json.loads(out.read_text())
            self.assertEqual(data["verdict"]["action"], "must_document")
            self.assertEqual(len(data["files"]), 2)

    def test_fail_on_review_exits_one(self) -> None:
        code = main(["--files", "internal/httpapi/write.go", "--fail-on-review"])
        self.assertEqual(code, 1)


if __name__ == "__main__":
    unittest.main()

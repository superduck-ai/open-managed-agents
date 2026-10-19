import argparse
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import review


class ReviewTests(unittest.TestCase):
    def test_outside_herdr_does_not_control_session(self):
        with patch.dict(os.environ, {"HERDR_ENV": "0"}), patch.object(review, "api") as api:
            with self.assertRaisesRegex(review.WorkflowError, "Herdr"):
                review.run(argparse.Namespace())
            api.assert_not_called()

    def test_blocked_cckimi_is_not_prompted(self):
        result = {"agents": [{"pane_id": "w1:p2", "agent": "claude", "agent_status": "blocked"}]}
        with patch.object(review, "api", return_value=result):
            with self.assertRaisesRegex(review.WorkflowError, "审批"):
                review.wait_cckimi("w1:p2", 1000)

    def test_blocked_prompt_is_saved_but_not_completed(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "review.txt"
            with patch.object(review, "api", side_effect=[{}, {"agent": {"agent_status": "blocked"}}]), \
                    patch.object(review, "command", return_value="approval needed"):
                with self.assertRaisesRegex(review.WorkflowError, "blocked"):
                    review.prompt("reviewer", "review", 1000, output)
            self.assertEqual(output.read_text(), "approval needed")

    def workflow(self, directory, snapshots, fail_review=False):
        root = Path(directory)
        script = root / "herdr" / "review.py"
        script.parent.mkdir()
        started = []

        def start(name, kind, pane, timeout):
            started.append(kind)

        def prompt(name, text, timeout, output):
            if fail_review and "cckimi" in name:
                raise review.WorkflowError("review timeout")
            text = "REVIEW_START\nreview result\nREVIEW_END\n"
            output.write_text(text)
            if "codex" in name:
                (output.parent / "conclusion.md").write_text("merged decisions and validation")
            return text

        args = argparse.Namespace(pane="w1:p1", scope="current diff", timeout=1000, startup_timeout=1000)
        with patch.dict(os.environ, {"HERDR_ENV": "1"}), \
                patch.object(review, "__file__", str(script)), \
                patch.object(review.shutil, "which", return_value="installed"), \
                patch.object(review, "api", return_value={}), \
                patch.object(review, "split", side_effect=["w1:p2", "w1:p3", "w1:p4"]), \
                patch.object(review, "start", side_effect=start), \
                patch.object(review, "snapshot", side_effect=snapshots), \
                patch.object(review, "prompt", side_effect=prompt):
            try:
                review.run(args)
            except review.WorkflowError:
                pass
        state_file = next(root.glob("tmp/herdr-review/*/state.json"))
        return started, review.json.loads(state_file.read_text()), state_file.parent

    def test_review_failure_prevents_codex(self):
        with tempfile.TemporaryDirectory() as directory:
            started, state, _ = self.workflow(directory, ["before"], fail_review=True)
            self.assertEqual(started, ["grok", "cckimi"])
            self.assertEqual(state["phase"], "interrupted")
            self.assertIn("review timeout", state["error"])

    def test_worktree_change_prevents_codex(self):
        with tempfile.TemporaryDirectory() as directory:
            started, state, _ = self.workflow(directory, ["before", "after"])
            self.assertNotIn("codex", started)
            self.assertEqual(state["phase"], "interrupted")

    def test_complete_workflow_preserves_both_reviews_and_conclusion(self):
        with tempfile.TemporaryDirectory() as directory:
            started, state, output = self.workflow(directory, ["same", "same"])
            self.assertEqual(started, ["grok", "cckimi", "codex"])
            self.assertEqual(state["phase"], "finished")
            self.assertIn("grok", (output / "reviews.md").read_text())
            self.assertIn("cckimi", (output / "reviews.md").read_text())
            self.assertTrue((output / "conclusion.md").is_file())

    def test_cckimi_launch_accepts_plain_cli_output(self):
        with patch.object(review, "command", return_value="Command sent"), \
                patch.object(review, "wait_cckimi") as wait, \
                patch.object(review, "api") as api:
            review.start("reviewer", "cckimi", "w1:p2", 1000)
            wait.assert_called_once_with("w1:p2", 1000)
            api.assert_called_once_with("agent", "rename", "w1:p2", "reviewer")

    def test_truncated_report_without_recovery_prevents_completion(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(review, "prompt", return_value="only last lines") as prompt:
            with self.assertRaisesRegex(review.WorkflowError, "完整报告"):
                review.review_agent("reviewer", "diff", "grok", 1000, Path(directory))
            self.assertEqual(prompt.call_count, 2)

    def test_agent_start_retries_only_rejected_busy_shell(self):
        with patch.object(review, "api", side_effect=[review.WorkflowError("busy", "agent_pane_busy"), {}]) as api, \
                patch.object(review.time, "sleep"):
            review.start("reviewer", "codex", "w1:p2", 1000)
            self.assertEqual(api.call_count, 2)

    def test_agent_start_does_not_retry_uncertain_errors(self):
        with patch.object(review, "api", side_effect=review.WorkflowError("timeout", "timeout")) as api:
            with self.assertRaisesRegex(review.WorkflowError, "timeout"):
                review.start("reviewer", "codex", "w1:p2", 1000)
            api.assert_called_once()

    def test_grok_start_accepts_response_with_terminal_timestamp(self):
        with patch.object(review, "api", return_value={"agent": {"agent_status": "idle"}}), \
                patch.object(review.time, "sleep"), \
                patch.object(review, "command", return_value="     READY      5:23 PM\n"):
            review.start("reviewer", "grok", "w1:p2", 1000)

    def test_report_markers_accept_terminal_timestamp_but_not_prompt_text(self):
        self.assertIsNone(review.complete_report("最终报告以 REVIEW_START 开始，REVIEW_END 结束"))
        report = review.complete_report("REVIEW_START    5:23 PM\nfindings\nREVIEW_END\n")
        self.assertIn("findings", report)

    def test_stalled_grok_without_final_export_is_not_completed(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(review, "prompt", side_effect=review.WorkflowError("stalled", "agent_prompt_stalled")), \
                patch.object(review, "api", return_value={"agent": {"agent_status": "idle"}}), \
                patch.object(review, "command", return_value="still investigating"):
            with self.assertRaisesRegex(review.WorkflowError, "完整最终报告"):
                review.review_agent("reviewer", "diff", "grok", 1000, Path(directory), "session-id")

    def test_stalled_working_grok_is_not_exported_or_reprompted(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(review, "prompt", side_effect=review.WorkflowError("stalled", "agent_prompt_stalled")) as prompt, \
                patch.object(review, "api", return_value={"agent": {"agent_status": "working"}}), \
                patch.object(review, "command") as command:
            with self.assertRaises(review.WorkflowError):
                review.review_agent("reviewer", "diff", "grok", 1000, Path(directory), "session-id")
            command.assert_not_called()
            prompt.assert_called_once()

    def test_stalled_grok_with_complete_export_is_recovered_without_reprompt(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(review, "prompt", side_effect=review.WorkflowError("stalled", "agent_prompt_stalled")) as prompt, \
                patch.object(review, "api", return_value={"agent": {"agent_status": "done"}}), \
                patch.object(review, "command", return_value="REVIEW_START\nfindings\nREVIEW_END\n"):
            output = Path(directory)
            review.review_agent("reviewer", "diff", "grok", 1000, output, "session-id")
            self.assertIn("findings", (output / "grok-review.md").read_text())
            self.assertTrue((output / "grok-prompt-error.txt").is_file())
            prompt.assert_called_once()

    def test_recover_full_report_when_terminal_is_truncated(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)

            def prompt(name, text, timeout, transcript):
                if "recovery" in transcript.name:
                    (output / "grok-review.md").write_text("REVIEW_START\nfindings\nREVIEW_END\n")
                return "last screen"

            with patch.object(review, "prompt", side_effect=prompt):
                review.review_agent("reviewer", "diff", "grok", 1000, output)
            self.assertIn("findings", (output / "grok-review.md").read_text())


if __name__ == "__main__":
    unittest.main()

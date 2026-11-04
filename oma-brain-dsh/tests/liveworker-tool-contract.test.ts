import { expect,test } from "bun:test";
import { resolve } from "node:path";

const files = [
  "tests/liveworker/chat_brain_test.go",
  "tests/liveworker/chat_brain_lazy_test.go",
  "tests/liveworker/chat_brain_compaction_test.go",
  "tests/liveworker/chat_brain_observability_test.go",
  "tests/liveworker/chat_brain_trace_export_test.go",
  "tests/liveworker/chat_brain_trace_openobserve_test.go",
  ".agents/skills/verify-be/features/brain.md",
  ".agents/skills/verify-be/features/observability.md",
  "oma-brain-dsh/README.md",
  "docs/design/be/ccrv2/dsh-otel-export.md",
  "docs/design/be/agent-system-prompt.md",
  "docs/zh/multiagent-orchestration.mdx",
  "docs/en/multiagent-orchestration.mdx",
];

test.each(files)("delegation consumer %s uses the current tool contract", async file => {
  const content = await Bun.file(resolve(import.meta.dir, "../..", file)).text();
  expect(/send_to_agent|archive_agent/.test(content)).toBe(false);
});

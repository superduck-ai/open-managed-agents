import { expect, test } from "bun:test";
import { MessageId, type AssistantMessage } from "@deepseek-ai/dsh-llm";
import { assistantMessages } from "../src/ccrv2/assistant.ts";

test.each(["oma-messages-v1", "oma-chat-completions-v1"])("%s retains a complete reply beyond the proxy observation limit", format => {
  const text = "x".repeat(4 * 1024 * 1024 + 1);
  const message: AssistantMessage = {
    id: MessageId("large-reply"), role: "assistant", content: [{ type: "text", text }],
    source: { kind: "model", provider: "oma", model: "test-model", replayState: {
      response: { format, messageID: "msg_large", requestID: "sevt_request" }, blocks: [{ index: 0 }],
    } },
  };
  const output = assistantMessages("cse_large", "fallback", message);
  expect(output).toHaveLength(1);
  expect(output[0]!.content).toEqual([{ type: "text", text }]);
  expect(output[0]!.model_request_start_id).toBe("sevt_request");
  expect(assistantMessages("cse_large", "different-replay-fallback", message)).toEqual(output);
});

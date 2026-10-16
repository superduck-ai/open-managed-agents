import { isDeepStrictEqual } from "node:util";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { validateJsonSchemaValue, type JsonSchemaNode } from "@deepseek-ai/dsh-tools";
import { object, type Payload } from "../json.ts";
import { toolText } from "./tools.ts";
import type {
  RemoteTool,
  SandboxExecutor,
  ToolExecutionBridge,
  ToolExecutionContext,
} from "./contracts.ts";

/** Ordered tool execution. The bridge supplies policy and confirmed journal writes. */
export class ToolExecution {
  constructor(
    readonly sandbox: SandboxExecutor,
    readonly bridge: ToolExecutionBridge,
  ) {}

  async execute(tool: RemoteTool, original: Payload, exec: ToolExecutionContext): Promise<string> {
    if (this.bridge.blocked())
      throw new Error("Session requires tool outcome reconciliation");
    const validate = (args: Payload) => {
      const errors = validateJsonSchemaValue(tool.inputSchema as JsonSchemaNode, args);
      if (errors.length)
        throw new Error(`Invalid ${tool.name} arguments: ${errors.join("; ")}`);
    };
    validate(original);
    const decision = await this.bridge.approve({
      callId: exec.callId,
      toolName: tool.name,
      input: original,
    }, exec.signal);
    if (!decision.allowed)
      throw new Error("Tool use denied by OMA");
    const effective = decision.input === undefined ? original : object(decision.input);
    validate(effective);
    exec.signal.throwIfAborted();
    const call = { callId: exec.callId, name: tool.name, original, effective };
    await this.bridge.recordApproved(call);
    await this.sandbox.provision(exec.signal);
    exec.signal.throwIfAborted();
    await this.bridge.flushLog();
    await this.bridge.recordDispatch(exec.callId);
    let result: Payload;
    let text: string;
    let remoteOutcome: "unknown" | undefined;
    try {
      result = await this.sandbox.call(tool, effective, exec.signal);
      text = toolText(result, tool.name);
    } catch {
      if (!exec.signal.aborted || exec.signal.reason?.kind !== "user") {
        try {
          await this.bridge.recordUnknown(exec.callId);
        } finally {
          this.bridge.stop();
        }
        throw new Error("Remote tool outcome is unknown; turn stopped without replay");
      }
      text = "Tool call interrupted.";
      result = { isError: true };
      remoteOutcome = "unknown";
    }
    const changed = !isDeepStrictEqual(original, effective);
    await this.bridge.recordResult({ ...call, text, failed: !!result.isError, ...(remoteOutcome ? { remoteOutcome } : {}) });
    if (changed)
      exec.deferContext(createUserMessage({
        source: { kind: "user" },
        content: [{
          type: "text",
          text: `OMA approval changed ${tool.name} arguments to ${JSON.stringify(effective)}. The returned result is for these approved arguments.`,
        }],
      }));
    if (result.isError) throw new Error(text || "Remote tool failed");
    return text;
  }
}

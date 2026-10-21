export class SandboxRequestError extends Error {
  constructor(readonly phase: "initialization" | "tool_call", readonly status?: number) {
    const reason = status === undefined ? "Sandbox transport failed" : `Sandbox gateway returned HTTP ${status}`;
    super(phase === "initialization" ? `${reason} during initialization. The tool was not executed.` : reason);
  }
}

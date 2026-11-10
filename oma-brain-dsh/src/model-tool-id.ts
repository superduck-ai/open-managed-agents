import { createHash } from "node:crypto";

export function modelToolCallID(requestID: string, providerID: string): string {
  return "toolu_" + createHash("sha256").update("model-tool-call-v1\0" + requestID + "\0" + providerID).digest("hex").slice(0, 32);
}

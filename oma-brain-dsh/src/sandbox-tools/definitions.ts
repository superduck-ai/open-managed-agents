import type { RemoteTool } from "../runtime/contracts.ts";
import definitions from "../../../internal/sandboxtools/definitions.json";

export function sandboxToolDefinitions(): RemoteTool[] {
  return structuredClone(definitions);
}

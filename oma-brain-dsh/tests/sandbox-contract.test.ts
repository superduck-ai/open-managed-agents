import { expect, test } from "bun:test";
import { Client, StreamableHTTPClientTransport } from "@modelcontextprotocol/client";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";
import type { Payload } from "../src/json.ts";

const endpoint = process.env.OMA_RUST_MCP_URL;

function contract(schema: Payload): Payload {
  const properties = Object.fromEntries(Object.entries(schema.properties ?? {}).sort(([a], [b]) => a.localeCompare(b))
    .map(([name, property]) => [name, {
      type: (property as Payload).type,
      ...((property as Payload).enum ? { enum: [...(property as Payload).enum].sort() } : {}),
      ...((property as Payload).additionalProperties ? { additionalProperties: (property as Payload).additionalProperties.type } : {}),
    }]));
  return { type: schema.type, required: [...(schema.required ?? [])].sort(), properties };
}

test.skipIf(!endpoint)("built-in sandbox tool definitions match the Rust sandbox MCP server", async () => {
  const client = new Client({ name: "oma-brain-dsh-contract", version: "0.1.0" }, { versionNegotiation: { mode: "auto" } });
  await client.connect(new StreamableHTTPClientTransport(new URL(endpoint!)));
  try {
    const { tools } = await client.listTools();
    const remote = new Map(tools.map((tool) => [tool.name, tool.inputSchema as Payload]));
    for (const definition of sandboxToolDefinitions()) {
      const schema = remote.get(definition.remoteName);
      expect(schema, `${definition.remoteName} is missing from tools/list`).toBeDefined();
      expect(contract(definition.inputSchema), definition.remoteName).toEqual(contract(schema!));
    }
  } finally {
    await client.close();
  }
});

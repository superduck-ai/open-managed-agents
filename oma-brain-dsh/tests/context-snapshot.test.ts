import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { omaRuntimeConfig } from "../src/oma-session.ts";
import { assignment } from "./backend.ts";

test("predefined children require their own frozen configuration", async () => {
  const task = assignment();
  const config = omaRuntimeConfig(new OmaApi("https://api.example"), task);
  await expect(
    config.threadMcpContext!({
      id: "child",
      name: "reader",
      model: "test-model",
      systemPrompt: "",
      status: "idle",
    }),
  ).rejects.toThrow("Missing frozen subagent context");
});

test("child configuration is local and its transport keeps thread scope", async () => {
  const task = assignment();
  task.launch.tool_permissions = { read: "allow", bash: "allow" };
  task.external_mcp_config = {
    mcpServers: { parent: { url: "https://parent.example" } },
  };
  task.subagent_contexts = {
    reader: {
      mcp_config: { mcpServers: { child: { url: "https://child.example" } } },
      runtime_tool_permissions: { read: "ask", bash: "deny" },
    },
  };
  const calls: Request[] = [];
  const api = new OmaApi("https://api.example", (async (input, init) => {
    calls.push(new Request(input, init));
    return Response.json({});
  }) as typeof fetch);
  const config = omaRuntimeConfig(api, task);
  const child = await config.threadMcpContext!({
    id: "child-id",
    name: "reader",
    model: "test-model",
    systemPrompt: "",
    status: "idle",
  });
  expect(calls).toHaveLength(0);
  expect(child.config).toEqual(task.subagent_contexts.reader!.mcp_config);
  expect(child.toolPermissions).toEqual({ read: "ask", bash: "deny" });
  await child.fetcher("https://child.example");
  expect(calls[0]!.headers.get("X-OMA-Session-Thread-ID")).toBe("child-id");
  const inline = await config.threadMcpContext!({
    id: "inline",
    name: "inline",
    inline: true,
    model: "test-model",
    systemPrompt: "",
    status: "idle",
  });
  expect(inline.config).toEqual(task.external_mcp_config);
  expect(inline.toolPermissions).toEqual(task.launch.tool_permissions);
});

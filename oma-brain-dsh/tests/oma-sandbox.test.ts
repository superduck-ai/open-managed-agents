import { runSubagent, subagentScripts } from "./subagent-tools.ts";
import { expect, test } from "bun:test";
import { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "../src/api.ts";
import { createOmaSession } from "../src/oma-session.ts";
import { mountOma } from "../src/plugin.ts";
import * as OmaSandboxProvider from "../src/oma-sandbox/index.ts";
import { SandboxCalls, sandboxCallTimeoutMs } from "../src/oma-sandbox/client.ts";
import * as SandboxTools from "../src/sandbox-tools/index.ts";
import { sandboxToolDefinitions } from "../src/sandbox-tools/definitions.ts";
import { assignment, Backend, textEvents, toolEvents } from "./backend.ts";
import type { Payload } from "../src/json.ts";

async function fixture(tools?: string[]) {
  const b = new Backend();
  if (tools) b.a.launch.tools = tools;
  await b.ready;
  const ctx = new Context();
  const api = new OmaApi(b.url);
  await mountOma(ctx, api);
  const session = createOmaSession(ctx, api, b.a);
  b.onApproval = async (payload) => {
    await session.control({ type: "control_response", response: {
      subtype: "success", request_id: payload.request_id, response: { behavior: "allow" },
    } });
  };
  return {
    b, ctx, session,
    providers: () => [...(ctx.registry.get(OmaSandboxProvider)?.fibers ?? [])],
    consumers: () => [...(ctx.registry.get(SandboxTools)?.fibers ?? [])],
    async close() {
      try { await session.close(); }
      finally { await ctx.fiber.dispose(); b.stop(); }
    },
  };
}

test("DSH child disposal preserves the root executor", async () => {
  const f = await fixture();
  try {
    await f.session.open();
    expect(f.providers()).toHaveLength(1);
    expect(f.b.mcpMethods).toEqual([]);
    const provider = f.providers()[0]!;
    const executor = provider.ctx.get("sandboxExecutor");
    subagentScripts(f.b, [textEvents("received child report")], [toolEvents("child", "child_call"), textEvents("child report")]);
    const report = await runSubagent(f.session, "subagent", { description: "child", prompt: ["Inspect files", "Inspect the workspace"].filter(Boolean).join("\n") });
    expect(report.report).toBe("child report");
    await f.session.subagents.wait();
    await f.session.agent.whenIdle();
    expect(f.providers()).toEqual([provider]);
    expect(f.consumers()).toHaveLength(1);
    expect(provider.ctx.get("sandboxExecutor")).toBe(executor);
    f.b.onModel = undefined;
    f.b.modelScript = [toolEvents("parent", "parent_call"), textEvents("done")];
    await f.session.receive("parent_input", { content: "Continue" });
    expect(f.b.executions.map((call) => call.arguments.command)).toEqual(["child", "parent"]);
    expect(f.b.executions).toHaveLength(2);
    await f.session.close();
    expect(f.providers()).toHaveLength(0);
    expect(f.consumers()).toHaveLength(0);
    expect(provider.ctx.get("sandboxExecutor")).toBeUndefined();
  } finally { await f.close(); }
});

test("failed root setup rolls back the provider without allocating a sandbox", async () => {
  const f = await fixture();
  const setup = f.session.config.setup!;
  f.session.config.setup = async (scope) => {
    await setup(scope);
    expect(f.providers()).toHaveLength(1);
    throw new Error("setup failed after provider mounted");
  };
  try {
    await expect(f.session.open()).rejects.toThrow("setup failed after provider mounted");
    expect(f.providers()).toHaveLength(0);
    expect(f.consumers()).toHaveLength(0);
    expect(f.b.sandboxCount).toBe(0);
    expect(f.b.mcpMethods).toEqual([]);
  } finally { await f.close(); }
});

test("root close keeps the provider alive until a child's tool result checkpoint drains", async () => {
  const f = await fixture();
  const writing = Promise.withResolvers<void>();
  const confirmed = Promise.withResolvers<void>();
  let settled: Promise<unknown> | undefined;
  try {
    await f.session.open();
    const provider = f.providers()[0]!;
    const persistence = f.ctx.ccrv2.config.backend;
    const persist = persistence.persist.bind(persistence);
    persistence.persist = async (id, ...args) => {
      if ("type" in args[1] && args[1].type === "tool/result") {
        writing.resolve();
        await confirmed.promise;
      }
      await persist(id, ...args);
    };
    f.b.modelScript = [toolEvents("child", "draining_call"), textEvents("done")];
    settled = runSubagent(f.session, "subagent", { description: "child", prompt: ["Test", "Run"].filter(Boolean).join("\n") }).catch(() => {});
    await writing.promise;
    let closed = false;
    const closing = f.session.close().then(() => { closed = true; });
    await new Promise<void>((resolve) => setImmediate(resolve));
    expect(closed).toBe(false);
    expect(provider.ctx.get("sandboxExecutor")).toBeDefined();
    confirmed.resolve();
    await Promise.all([settled, closing]);
    expect(f.providers()).toHaveLength(0);
    expect(f.b.executions).toHaveLength(1);
  } finally {
    confirmed.resolve();
    await settled;
    await f.close();
  }
});

test.each(["user.interrupt", "control_request"])("confirmed %s ends as end_turn and permits another turn in the same session", async (type) => {
  const f = await fixture();
  const dispatched = Promise.withResolvers<void>();
  const cancelled = Promise.withResolvers<Record<string, unknown>>();
  try {
    await f.session.open();
    f.b.modelScript = [toolEvents("sleep 120", "cancel_call")];
    f.b.onTool = async () => { dispatched.resolve(); return cancelled.promise; };
    f.b.onCancel = async () => { cancelled.resolve({ isError: true, content: [{ type: "text", text: "cancelled" }], structuredContent: { status: "cancelled", cancelled: true } }); };
    const turn = f.session.receive("interrupt_input", { content: "Run until interrupted" });
    await dispatched.promise;
    await f.session.receive("interrupt_control", type === "control_request"
      ? { type, request_id: "interrupt_request", request: { subtype: "interrupt" } }
      : { type });
    await turn;
    expect(f.session.abort.signal.aborted).toBe(false);
    expect([...f.b.publicLog.values()].some((event) => event.type === "result" && event.is_error === true)).toBe(false);
    expect([...f.b.publicLog.values()].filter((event) => event.type === "result").map((event) => event.is_error)).toEqual([false]);
    expect([...f.b.publicLog.values()].some((event) => event.type === "agent.message")).toBe(false);
    f.b.modelScript = [textEvents("RESUMED")];
    await f.session.receive("resume_input", { content: "Reply RESUMED without tools" });
    expect([...f.b.publicLog.values()].filter((event) => event.type === "result").at(-1)!.is_error).toBe(false);
    expect(f.b.executions).toHaveLength(1);
  } finally { cancelled.resolve({ isError: true, content: [{ type: "text", text: "cancelled" }] }); await f.close(); }
});


test("root model requests expose only the session's enabled sandbox tools", async () => {
  for (const tools of [[], ["Read"]]) {
    const f = await fixture(tools);
    try {
      await f.session.open();
      f.b.modelScript = [textEvents("done")];
      await f.session.receive("allowlist_input", { content: "Inspect available tools" });
      expect(f.b.requests).toHaveLength(1);
      expect((f.b.requests[0]!.tools ?? []).map((tool: { name: string }) => tool.name))
        .toEqual([...tools, "interrupt_agent", "list_agents", "send_message", "skill", "subagent"].sort());
      expect(f.b.sandboxCount).toBe(0);
      expect(f.b.mcpMethods).toEqual([]);
    } finally { await f.close(); }
  }
});

test("sandbox Bash projection preserves model and public tool output", async () => {
  const f = await fixture(["Bash"]);
  const stdout = "exact stdout\n";
  const metadata = { stdout, stderr: "", exit_code: 0, status: "completed", timed_out: false,
    cancelled: false, stdout_truncated: false, stderr_truncated: false };
  f.b.onTool = async () => ({ structuredContent: metadata, content: [
    { type: "text", text: stdout }, { type: "text", text: JSON.stringify(metadata) },
  ] });
  try {
    await f.session.open();
    f.b.modelScript = [toolEvents("printf exact", "projected-bash"), textEvents("done")];
    await f.session.receive("projection", { content: "Run Bash" });
    const request = JSON.stringify(f.b.requests[1]!.messages);
    expect(request).toContain("exact stdout");
    expect(request).not.toContain("exit_code");
    const results = [...f.b.publicLog.values()].flatMap(event => event.message?.content ?? [])
      .filter(block => block.type === "tool_result" && block.tool_use_id === "projected-bash");
    expect(results).toHaveLength(1);
    expect(results[0]!.content).toEqual([{ type: "text", text: stdout }]);
  } finally { await f.close(); }
});

test("a lost sandbox call becomes a model-visible tool error and the turn continues", async () => {
  const f = await fixture(["Bash"]);
  let executions = 0;
  f.b.onTool = async () => { executions++; throw new Error("sandbox retired"); };
  try {
    await f.session.open();
    f.b.modelScript = [toolEvents("touch marker", "lost-bash"), textEvents("checked state")];
    await f.session.receive("lost", { content: "Run Bash" });
    expect(executions).toBe(1);
    expect(f.b.requests).toHaveLength(2);
    const request = JSON.stringify(f.b.requests[1]!.messages);
    expect(request).toContain("Remote tool outcome is unknown");
    const results = [...f.b.publicLog.values()].flatMap(event => event.message?.content ?? [])
      .filter(block => block.type === "tool_result" && block.tool_use_id === "lost-bash");
    expect(results).toHaveLength(1);
    expect(results[0]!.is_error).toBe(true);
    expect([...f.b.publicLog.values()].filter(event => event.type === "session.error")).toHaveLength(0);
  } finally { await f.close(); }
});

test("submission response loss reuses the native call identity", async () => {
 const bodies:Payload[]=[];
 let polls=0;
 const api=new OmaApi("http://oma.test",Object.assign(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const request=new Request(input,init);
  if(request.method==="POST"){
   bodies.push(await request.json());
   if(bodies.length===1)throw new Error("response lost after commit");
   return Response.json({state:"running"});
  }
  polls++;
  return Response.json(polls===1?{state:"running"}:{state:"succeeded",result:{content:[{type:"text",text:"saved"}]}});
 },{preconnect:fetch.preconnect}));
 const a=assignment();api.assignments.set(a.session_id,a);
 const executor=new SandboxCalls(api,a.session_id);
 const tool=sandboxToolDefinitions().find(tool=>tool.name==="Write")!;
 expect(await executor.call(tool,{file_path:"/tmp/a",content:"x"},new AbortController().signal,"child","native-call")).toMatchObject({content:[{text:"saved"}]});
 expect(bodies.map(body=>body.call_id)).toEqual(["native-call","native-call"]);
 expect(polls).toBe(2);
});

test("user cancellation retains output confirmed by the sandbox",async()=>{
 const abort=new AbortController();let cancelled=false;
 const api=new OmaApi("http://oma.test",Object.assign(async(input:RequestInfo|URL,init?:RequestInit)=>{
  const request=new Request(input,init);
  if(request.url.endsWith("/cancel")){cancelled=true;return Response.json({state:"cancel_requested"});}
  if(request.method==="POST")return Response.json({state:"running"});
  if(!cancelled){abort.abort({kind:"user"});throw abort.signal.reason;}
  return Response.json({state:"cancelled",result:{isError:true,content:[{type:"text",text:"partial output"}]}});
 },{preconnect:fetch.preconnect}));
 const a=assignment();api.assignments.set(a.session_id,a);
 const executor=new SandboxCalls(api,a.session_id);
 const tool=sandboxToolDefinitions().find(tool=>tool.name==="Bash")!;
 expect(await executor.call(tool,{command:"long"},abort.signal,undefined,"native-cancel")).toMatchObject({isError:true,content:[{text:"partial output"}]});
 expect(cancelled).toBe(true);
});


test("call deadlines leave time for Bash background handoff and process exit", () => {
  const tools = sandboxToolDefinitions();
  const bash = tools.find(tool => tool.name === "Bash")!;
  expect(sandboxCallTimeoutMs(bash, { timeout: 1, on_timeout: "background" })).toBe(31_000);
  expect(sandboxCallTimeoutMs(bash, { timeout: 900 })).toBe(630_000);
  expect(sandboxCallTimeoutMs(bash, {})).toBe(150_000);
  expect(sandboxCallTimeoutMs(tools.find(tool => tool.name === "Read")!, {})).toBe(180_000);
});

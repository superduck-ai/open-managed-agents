import { expect, test } from "bun:test";
import { OmaApi, HttpError } from "../src/api.ts";
import { registerWorkerTask, validateBrainTask } from "../src/worker-registration.ts";
import { assignment } from "./backend.ts";

test("registration rejects conflicts and stale epochs", async () => {
 const task = { session_id: "cse", worker_epoch: 1, ingress_token: "ingress", model_token: "model" };
 const conflict = new OmaApi("http://oma.test", (async (_url, _init) => new Response(null, {status:409})) as typeof fetch);
 await expect(registerWorkerTask(conflict, task)).rejects.toBeInstanceOf(HttpError);
 const stale = new OmaApi("http://oma.test", (async (_url, _init) => Response.json({worker_epoch:"2"})) as typeof fetch);
 await expect(registerWorkerTask(stale, task)).rejects.toThrow("does not match");
 for (const value of [null, [], {}, {...task, worker_epoch:0}, {...task, ingress_token:""}]) expect(() => validateBrainTask(value)).toThrow();
});

test("registration rejects missing and malformed session context envelopes", async () => {
 const a = assignment();
 const task = {session_id:a.session_id, worker_epoch:a.worker_epoch, ingress_token:a.ingress_token, model_token:a.model_token};
 for (const response of [{model:a.launch.model, cwd:a.launch.work_dir}, {session_context:null}, {session_context:[]}, {session_context:"invalid"}]) {
  const api = new OmaApi("http://oma.test", (async (url, init) => Response.json(
   new Request(url, init).method === "POST" ? {worker_epoch:String(a.worker_epoch)} : response,
  )) as typeof fetch);
  await expect(registerWorkerTask(api, task)).rejects.toThrow("Invalid session_context");
 }
});

test("NATS admission reuses CCR registration and session context", async () => {
 const a = assignment();
 a.launch.environment_prompt = "# Managed-agent environment";
 a.launch.memory_catalog_prompt = "<attached_memory_stores/>";
 const task = {session_id:a.session_id, worker_epoch:a.worker_epoch, ingress_token:a.ingress_token, model_token:a.model_token};
 const calls: Request[] = [];
 const api = new OmaApi("http://oma.test", (async (url, init) => {
  const request = new Request(url, init); calls.push(request);
  return Response.json(request.method === "POST" ? {worker_epoch:String(a.worker_epoch)} : {session_context: {
   cwd:a.launch.work_dir, model:a.launch.model, system_prompt:a.launch.system,
   append_system_prompt:a.launch.environment_prompt, memory_catalog_prompt:a.launch.memory_catalog_prompt, runtime_skills:a.launch.skills, subagents:a.launch.subagents,
  }});
 }) as typeof fetch);
 const result = await registerWorkerTask(api, task);
 expect(result.ingress_token).toBe(task.ingress_token);
 expect(result.model_token).toBe(task.model_token);
 expect(result.launch.system).toBe(a.launch.system);
 expect(result.launch.model).toBe(a.launch.model);
 expect(result.launch.work_dir).toBe(a.launch.work_dir);
 expect(result.launch.environment_prompt).toBe("# Managed-agent environment");
 expect(result.launch.memory_catalog_prompt).toBe("<attached_memory_stores/>");
 expect(calls).toHaveLength(2);
 expect(calls[0]!.headers.has("X-Worker-ID")).toBe(false);
 expect(calls[0]!.headers.has("X-Worker-Terminal-Events")).toBe(false);
 expect(await calls[0]!.json()).toEqual({});
 expect(new URL(calls[1]!.url).pathname).toBe(`/v2/sessions/${a.session_id}`);
});

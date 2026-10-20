import { expect, test } from "bun:test";
import { persistExternalMcpOutput } from "../src/external-mcp/output.ts";
import type { Payload } from "../src/json.ts";
import type { SandboxExecutor } from "../src/runtime/contracts.ts";

function storage(fail = false) {
 const files = new Map<string, string>();
 const executor: SandboxExecutor = {
  async provision() {},
  async call(tool, args) {
   expect(tool.remoteName).toBe("write");
   if (fail) return {isError:true,content:[{type:"text",text:"write refused"}]};
   files.set(args.file_path,args.content);
   return {content:[{type:"text",text:"written"}]};
  },
 };
 return {files,executor};
}
for (const text of ["x".repeat(100000), "😀".repeat(100000)]) {
 test(`exact character boundary is inline without allocating a sandbox (${text.length} UTF16 units)`,async()=>{
  const {files,executor}=storage();const result={content:[{type:"text",text}]};
  expect(await persistExternalMcpOutput(result,executor,new AbortController().signal)).toBe(result);
  expect(files.size).toBe(0);
 });
}
test("Unicode large output saves exact content, preserves error and hides rawOutput from the preview",async()=>{
 const {files,executor}=storage();const text="😀".repeat(50001)+"private-middle"+"中".repeat(50000);
 const result:Payload={isError:true,content:[{type:"text",text}],structuredContent:{rawOutput:text}};
 const a=await persistExternalMcpOutput(result,executor,new AbortController().signal);
 const b=await persistExternalMcpOutput(result,executor,new AbortController().signal);
 expect(files.size).toBe(2);expect([...files.values()]).toEqual([text,text]);
 expect(a.isError).toBe(true);expect(a.structuredContent).toBeUndefined();expect(a.content[0].text).not.toContain("private-middle");
 expect(a.content[0].text).not.toBe(b.content[0].text);
});
test("failed persistence reports a known failure without full output or a false saved path",async()=>{
 const {files,executor}=storage(true);const text="private-".repeat(15000);
 const result=await persistExternalMcpOutput({content:[{type:"text",text}]},executor,new AbortController().signal);
 expect(result.isError).toBe(true);expect(JSON.stringify(result)).not.toContain("/tmp/tool-outputs/");expect(JSON.stringify(result)).not.toContain(text);expect(files.size).toBe(0);
});
test("cancelled persistence does not publish a successful file preview",async()=>{
 const {executor}=storage();const abort=new AbortController();abort.abort();
 await expect(persistExternalMcpOutput({content:[{type:"text",text:"x".repeat(100001)}]},executor,abort.signal)).rejects.toThrow();
});

for (const type of ["image","audio","resource","resource_link"]) {
 test(`completed external ${type} content returns a known unsupported result without persistence`,async()=>{
  const {files,executor}=storage();
  const result=await persistExternalMcpOutput({content:[{type:"text",text:"known"},{type}]},executor,new AbortController().signal);
  expect(result.isError).toBe(true);expect(result.content[0].text).toContain("unsupported non-text content");expect(files.size).toBe(0);
 });
}

test("malformed text is not mislabeled as a completed unsupported-content outcome",async()=>{
 const {executor}=storage();
 await expect(persistExternalMcpOutput({content:[{type:"text",text:7}]},executor,new AbortController().signal)).rejects.toThrow("Only text");
});

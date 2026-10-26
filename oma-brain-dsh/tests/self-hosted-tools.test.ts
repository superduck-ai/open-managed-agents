import { expect, test } from "bun:test";
import { ToolCallId } from "@deepseek-ai/dsh-llm";
import { ToolExecution } from "../src/runtime/execution.ts";
import type { ApprovalDecision, ToolExecutionBridge } from "../src/runtime/contracts.ts";

const tool = {name:"Write",remoteName:"write",description:"write",inputSchema:{type:"object",properties:{content:{type:"string"}},required:["content"]}};
function fixture(decision: ApprovalDecision) {
 const steps:string[]=[];
 const bridge:ToolExecutionBridge={
  approve:async()=>decision,
  runTool:async(_id,execute)=>{steps.push("dispatch");return execute();},
  approvalChanged(){},
 };
 const sandbox={provision:async()=>{steps.push("provision");},call:async()=>{steps.push("local-call");return {content:[{type:"text",text:"cloud"}],isError:false};}};
 return {execution:new ToolExecution(sandbox,bridge),steps};
}
const context=()=>({callId:ToolCallId(crypto.randomUUID()),signal:new AbortController().signal,deferContext:()=>{}});
test("self-hosted successful real-client result bypasses provision/call and retains checkpoint ordering",async()=>{
 const f=fixture({allowed:true,externalResult:{content:[{type:"text",text:" exact\nbytes "}],isError:false}});
 expect(await f.execution.execute(tool,{content:"original"},context())).toBe(" exact\nbytes ");
 expect(f.steps).toEqual(["dispatch"]);

});
test("self-hosted failed result remains a known tool failure and never provisions/replays",async()=>{
 const f=fixture({allowed:true,externalResult:{content:[{type:"text",text:"readonly failure"}],isError:true}});
 await expect(f.execution.execute(tool,{content:"original"},context())).rejects.toThrow("readonly failure");
 expect(f.steps).toEqual(["dispatch"]);

});
test("denied self-hosted call cannot execute or record a supplied result",async()=>{
 const f=fixture({allowed:false,externalResult:{content:[{type:"text",text:"forbidden"}],isError:false}});
 await expect(f.execution.execute(tool,{content:"original"},context())).rejects.toThrow("denied");expect(f.steps).toEqual([]);
});
test("cloud executor continues to provision and call the actual sandbox",async()=>{
 const f=fixture({allowed:true});expect(await f.execution.execute(tool,{content:"original"},context())).toBe("cloud");
 expect(f.steps).toEqual(["provision","dispatch","local-call"]);
});

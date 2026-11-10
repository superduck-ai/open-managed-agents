import { expect, test } from "bun:test";
import { modelToolCallID } from "../src/model-tool-id.ts";
import { AiSdkStream } from "../src/llm-ai-sdk/stream.ts";

test("provider IDs are stable within a request and independent across requests", () => {
  expect(modelToolCallID("request-a", "functions.Read:2")).toBe("toolu_ce3031e9a6dade42a6fe4a85f105445a");
  const ids: string[] = [];
  for (const request of ["request-a", "request-b"]) {
    const stream = new AiSdkStream("oma-messages-v1", request, id => modelToolCallID(request,id));
    stream.accept({type:"tool-input-start",id:"functions.Read:2",toolName:"Read"});
    const deltas = stream.accept({type:"tool-input-delta",id:"functions.Read:2",delta:'{"offset":0}'});
    const ends = stream.accept({type:"tool-call",toolCallId:"functions.Read:2",toolName:"Read",input:'{"offset":0}'});
    const block = ends.find(chunk => chunk.type === "block-end");
    if (block?.type !== "block-end" || block.block.type !== "tool-call") throw new Error("missing tool block");
    expect(deltas).toContainEqual({type:"tool-call-delta",index:0,id:block.block.id,name:"Read",argumentsDelta:'{"offset":0}'});
    expect(String(block.block.id)).toBe(modelToolCallID(request,"functions.Read:2"));
    ids.push(block.block.id);
  }
  expect(ids[0]).not.toBe(ids[1]);
});

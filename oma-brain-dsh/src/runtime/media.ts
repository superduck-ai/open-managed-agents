import type { ContentBlock } from "@deepseek-ai/dsh-llm";
import type { Payload } from "../json.ts";

export interface MessageMediaBlock {
  type: "message-media";
  block: Payload;
}

declare module "@deepseek-ai/dsh-llm/types" {
  interface ContentBlockMap {
    "message-media": MessageMediaBlock;
  }
}

export type UserContent = string | ContentBlock[];

export function userContentBlocks(input: UserContent): ContentBlock[] {
  return typeof input === "string" ? [{ type: "text", text: input }] : input;
}

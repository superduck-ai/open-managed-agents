import type { Context } from "@deepseek-ai/cordis";
import { admitEncodedImages, type EncodedImageAttachment } from "@deepseek-ai/dsh-attachment";
import type { ContentBlock } from "@deepseek-ai/dsh-llm";
import type { ToolExecution } from "@deepseek-ai/dsh-tools";
import type { Payload } from "../json.ts";
import { withAttachmentSession } from "../attachments/remote.ts";
import { toolText } from "../runtime/tools.ts";

export async function readContent(ctx: Context, result: Payload, exec: ToolExecution): Promise<ContentBlock[]> {
  if (!Array.isArray(result.content) || !result.content.some((block: Payload) => block?.type === "image"))
    return [{ type: "text", text: toolText(result) }];
  const images: EncodedImageAttachment[] = [];
  for (const block of result.content) {
    if (block?.type === "text" && typeof block.text === "string") continue;
    if (block?.type !== "image" || typeof block.data !== "string" || !["image/png", "image/jpeg", "image/gif", "image/webp"].includes(block.mimeType))
      throw new Error("Read returned unsupported image content");
    images.push({ data: block.data, mediaType: block.mimeType });
  }
  const agent = exec.agent;
  if (!agent) throw new Error("Read image requires a session");
  const routed = agent.session.requestHeader()?.config;
  const provider = routed?.provider ?? agent.options.provider;
  const model = routed?.model ?? agent.options.model;
  if (!provider || !model) throw new Error("Read image requires a model route");
  const info = await ctx.llm.resolveModelInfo(provider, model, exec.signal);
  if (!info.inputModalities?.includes("image")) throw new Error("Read returned an image but the current model does not support image input");
  const attachments = ctx.get("attachments");
  if (!attachments) throw new Error("Read image attachment storage is unavailable");
  exec.signal.throwIfAborted();
  const refs = await withAttachmentSession(agent.session.id, () => admitEncodedImages(attachments, images));
  exec.signal.throwIfAborted();
  let index = 0;
  return result.content.map((block: Payload): ContentBlock => block.type === "text"
    ? { type: "text", text: block.text } : { type: "image", attachment: refs[index++]! });
}

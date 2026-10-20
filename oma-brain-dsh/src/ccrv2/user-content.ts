import { admitEncodedImages, type AttachmentStore, type EncodedImageAttachment } from "@deepseek-ai/dsh-attachment";
import type { ContentBlock } from "@deepseek-ai/dsh-llm";
import { object, type Payload } from "../json.ts";

export interface OmaDocument {
  type: "document";
  title?: string;
  context?: string;
  citations?: { enabled: boolean };
  source: { type: "text"; media_type: "text/plain"; data: string }
    | { type: "base64"; media_type: "application/pdf"; data: string }
    | { type: "url"; url: string }
    | { type: "file"; file_id: string };
}

export type OmaImageSource = { type: "file"; file_id: string } | { type: "url"; url: string };

declare module "@deepseek-ai/dsh-llm" {
  interface ContentBlockMap {
    "oma-document": { type: "oma-document"; document: OmaDocument };
    "oma-image": { type: "oma-image"; source: OmaImageSource };
  }
}

export async function userInputContent(payload: Payload, attachments: AttachmentStore): Promise<ContentBlock[]> {
  const content = payload.message?.content ?? payload.content;
  if (typeof content === "string") return [{ type: "text", text: content }];
  if (!Array.isArray(content) || content.length === 0) throw new Error("User input requires content blocks");
  if (content.every((block) => block?.type === "text" && typeof block.text === "string"))
    return [{ type: "text", text: content.map((block) => block.text).join("\n") }];
  const images: EncodedImageAttachment[] = [];
  const blocks: ContentBlock[] = [];
  for (const value of content) {
    const block = object(value);
    const reference = block.type === "image" ? referencedImage(block) : undefined;
    if (block.type === "text" && typeof block.text === "string") blocks.push({ type: "text", text: block.text });
    else if (reference) blocks.push({ type: "oma-image", source: reference });
    else if (block.type === "image") {
      const source = object(block.source);
      if (source.type !== "base64" || typeof source.data !== "string" ||
        !["image/png", "image/jpeg", "image/webp", "image/gif"].includes(source.media_type))
        throw new Error("Image input requires a supported base64 raster source");
      images.push({ mediaType: source.media_type, data: source.data });
    } else if (block.type === "document") blocks.push({ type: "oma-document", document: documentContent(block) });
    else throw new Error("Unsupported user content block");
  }
  const references = await admitEncodedImages(attachments, images);
  let imageIndex = 0;
  let blockIndex = 0;
  return content.map((block) => block.type === "image" && !referencedImage(block)
    ? { type: "image", attachment: references[imageIndex++]! }
    : blocks[blockIndex++]!);
}

function referencedImage(block: Payload): OmaImageSource | undefined {
  const source = object(block.source);
  if (source.type === "file" && typeof source.file_id === "string" && source.file_id) return { type: "file", file_id: source.file_id };
  if (source.type === "url" && typeof source.url === "string" && source.url) return { type: "url", url: source.url };
  return undefined;
}

function documentContent(block: Payload): OmaDocument {
  const source = object(block.source);
  const valid = source.type === "text" && source.media_type === "text/plain" && typeof source.data === "string"
    || source.type === "base64" && source.media_type === "application/pdf" && typeof source.data === "string"
    || source.type === "url" && typeof source.url === "string"
    || source.type === "file" && typeof source.file_id === "string";
  if (!valid) throw new Error("Unsupported document source");
  if (block.title !== undefined && typeof block.title !== "string") throw new Error("Invalid document title");
  if (block.context !== undefined && typeof block.context !== "string") throw new Error("Invalid document context");
  return structuredClone(block) as OmaDocument;
}

import type { Assignment } from "../assignment.ts";
import { RetryableInputError } from "../runtime/contracts.ts";
import { AsyncLocalStorage } from "node:async_hooks";
import { join } from "node:path";
import { Context } from "@deepseek-ai/cordis";
import { AttachmentError, AttachmentStore, isImageAdmissionError, type ImageAttachmentRef, type ImageRequestTarget, type SaveImageAttachment, type StoredImageAttachment } from "@deepseek-ai/dsh-attachment";
import { prepareImageFile, commitPreparedImageFile, readImageFile, readRequestImageFile, DEFAULT_MAX_IMAGE_BYTES, DEFAULT_MAX_IMAGE_PIXELS, DEFAULT_MAX_IMAGE_DIMENSION, DEFAULT_MAX_IMAGES_PER_MESSAGE, DEFAULT_MAX_MESSAGE_IMAGE_BYTES, DEFAULT_NORMALIZED_IMAGE_MAX_PIXELS, DEFAULT_NORMALIZED_IMAGE_MAX_DIMENSION, DEFAULT_NORMALIZED_IMAGE_MAX_BYTES } from "@deepseek-ai/dsh-attachment-local";
import { HttpError, type OmaApi } from "../api.ts";
import { resolveBrainHome } from "../storage.ts";

const sessionScope = new AsyncLocalStorage<string>();

export function withAttachmentSession<T>(id: string, action: () => Promise<T>): Promise<T> {
  return sessionScope.run(id, action);
}

export class AttachmentTransportError extends RetryableInputError {
  readonly recovery: "retry" | "reclaim";
  constructor(readonly cause: unknown) {
    const recovery = cause instanceof HttpError && [401, 403, 409, 410].includes(cause.status) ? "reclaim" : "retry";
    super(recovery === "reclaim" ? "Image attachment worker credentials or lease rejected; retry after worker reassignment"
      : "Image attachment storage temporarily unavailable; retry the operation", { cause });
    this.name = "AttachmentTransportError";
    this.recovery = recovery;
  }
}

export default class RemoteAttachmentStore extends AttachmentStore {
  readonly imageLimits = { maxImageBytes: DEFAULT_MAX_IMAGE_BYTES, maxImagesPerMessage: DEFAULT_MAX_IMAGES_PER_MESSAGE,
    maxMessageImageBytes: DEFAULT_MAX_MESSAGE_IMAGE_BYTES, maxImagePixels: DEFAULT_MAX_IMAGE_PIXELS,
    maxImageDimension: DEFAULT_MAX_IMAGE_DIMENSION, mediaTypes: ["image/png", "image/jpeg", "image/webp", "image/gif"] as const };
  private readonly root: string;
  private readonly policy = { maxPixels: DEFAULT_NORMALIZED_IMAGE_MAX_PIXELS, maxDimension: DEFAULT_NORMALIZED_IMAGE_MAX_DIMENSION, maxBytes: DEFAULT_NORMALIZED_IMAGE_MAX_BYTES };
  private readonly authorized = new WeakMap<Assignment, { epoch: number; hashes: Set<string> }>();
  private active = 0;
  private readonly waiting: (() => void)[] = [];

  constructor(ctx: Context, private readonly options: { api: OmaApi; cacheRoot?: string }) { super(ctx); this.root = options.cacheRoot ?? join(resolveBrainHome(), "attachments", "v1"); }

  async validateImage(input: SaveImageAttachment): Promise<void> { await this.compress(() => prepareImageFile(input, this.imageLimits, this.policy)); }

  private async compress<T>(action: () => Promise<T>): Promise<T> {
    if (this.active >= 2) await new Promise<void>(resolve => this.waiting.push(resolve));
    else this.active++;
    try { return await action(); }
    finally { const next = this.waiting.shift(); if (next) next(); else this.active--; }
  }

  async saveImage(input: SaveImageAttachment): Promise<ImageAttachmentRef> {
    const prepared = await this.compress(() => prepareImageFile(input, this.imageLimits, this.policy));
    const digest = prepared.ref.attachmentId.slice("sha256:".length);
    const authorization = this.authorization();
    const response = await this.remote(`image-attachments/${digest}`, "PUT", prepared.data, prepared.ref.mediaType);
    await response.body?.cancel();
    if (!response.ok) throw new AttachmentError("Image attachment unavailable", response.status === 413 ? "IMAGE_TOO_LARGE" : "INVALID_IMAGE");
    authorization.add(digest);
    await commitPreparedImageFile(this.root, prepared);
    return prepared.ref;
  }

  async readImage(ref: ImageAttachmentRef, signal?: AbortSignal): Promise<StoredImageAttachment> {
    if (!/^sha256:[0-9a-f]{64}$/.test(ref.attachmentId)) throw new AttachmentError("Invalid attachment reference", "INVALID_ATTACHMENT_REF");
    const path = `image-attachments/${ref.attachmentId.slice("sha256:".length)}`;
    const authorization = this.authorization();
    const digest = ref.attachmentId.slice("sha256:".length);
    if (!authorization.has(digest)) {
      const response = await this.remote(path, "HEAD", undefined, undefined, signal);
      if (!response.ok) throw new AttachmentError("Stored image attachment is unavailable", response.status === 404 ? "ATTACHMENT_NOT_FOUND" : "ATTACHMENT_CORRUPT");
      authorization.add(digest);
    }
    try { return await readImageFile(this.root, ref, signal); }
    catch (error) {
      signal?.throwIfAborted();
      const response = await this.remote(path, "GET", undefined, undefined, signal);
      if (!response.ok) throw new AttachmentError("Stored image attachment is unavailable", response.status === 404 ? "ATTACHMENT_NOT_FOUND" : "ATTACHMENT_CORRUPT");
      const data = await boundedBytes(response, ref.bytes, signal);
      if (data.byteLength !== ref.bytes || `sha256:${new Bun.CryptoHasher("sha256").update(data).digest("hex")}` !== ref.attachmentId)
        throw new AttachmentError("Image attachment integrity check failed", "ATTACHMENT_CORRUPT");
      await commitPreparedImageFile(this.root, { data, ref });
      return readImageFile(this.root, ref, signal);
    }
  }

  async readImageRequest(ref: ImageAttachmentRef, target: ImageRequestTarget, signal?: AbortSignal) {
    const stored = await this.readImage(ref, signal);
    return this.compress(() => readRequestImageFile(this.root, stored, target, signal));
  }

  async source(fileId: string, signal?: AbortSignal): Promise<SaveImageAttachment | undefined> {
    const response = await this.remote(`message-attachments/${encodeURIComponent(fileId)}/content`, "GET", undefined, undefined, signal);
    if (!response.ok) return undefined;
    const mediaType = response.headers.get("content-type");
    if (!this.imageLimits.mediaTypes.includes(mediaType as never)) { await response.body?.cancel(); return undefined; }
    try {
      const data = await boundedBytes(response, this.imageLimits.maxImageBytes, signal);
      return { data, mediaType: mediaType as SaveImageAttachment["mediaType"] };
    } catch (error) {
      if (isImageAdmissionError(error)) return undefined;
      throw error;
    }
  }

  private assignment(): Assignment {
    const id = sessionScope.getStore();
    if (!id) throw new Error("Image attachment access requires a session");
    return this.options.api.assignment(this.options.api.modelSessions.get(id) ?? id);
  }

  private authorization(): Set<string> {
    const assignment = this.assignment();
    let authorization = this.authorized.get(assignment);
    if (!authorization || authorization.epoch !== assignment.worker_epoch) {
      authorization = { epoch: assignment.worker_epoch, hashes: new Set() };
      this.authorized.set(assignment, authorization);
    }
    return authorization.hashes;
  }

  private async remote(path: string, method: string, data?: Uint8Array, mediaType?: string, signal?: AbortSignal): Promise<Response> {
    const api = this.options.api;
    const assignment = this.assignment();
    let response: Response;
    try {
      response = await api.fetcher(`${api.base.replace(/\/$/, "")}/v1/code/sessions/${encodeURIComponent(assignment.session_id)}/worker/${path}`, {
        method, headers: { Authorization: `Bearer ${assignment.ingress_token}`, "X-Worker-Epoch": String(assignment.worker_epoch), ...(mediaType ? { "Content-Type": mediaType } : {}) },
        body: data ? new Uint8Array(data).buffer : undefined, redirect: "error", signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(30_000)]) : AbortSignal.timeout(30_000),
      });
    } catch (error) { signal?.throwIfAborted(); throw new AttachmentTransportError(error); }
    if (response.ok) return response;
    await response.body?.cancel();
    if ([401, 403, 408, 409, 410, 429].includes(response.status) || response.status >= 500)
      throw new AttachmentTransportError(new HttpError(response.status));
    if ([400, 404, 413, 415, 422].includes(response.status)) return response;
    throw new HttpError(response.status);
  }
}

async function boundedBytes(response: Response, limit: number, signal?: AbortSignal): Promise<Uint8Array> {
  if (!Number.isSafeInteger(limit) || limit <= 0 || limit > DEFAULT_MAX_IMAGE_BYTES) throw new AttachmentError("Invalid image size", "INVALID_ATTACHMENT_REF");
  const reader = response.body?.getReader();
  if (!reader) throw new AttachmentTransportError(new Error("Missing image body"));
  const chunks: Uint8Array[] = [];
  let bytes = 0;
  try {
    for (;;) {
      signal?.throwIfAborted();
      const next = await reader.read();
      if (next.done) break;
      bytes += next.value.byteLength;
      if (bytes > limit) throw new AttachmentError("Image exceeds source limit", "IMAGE_TOO_LARGE");
      chunks.push(next.value);
    }
    return new Uint8Array(Buffer.concat(chunks, bytes));
  } catch (error) {
    if (isImageAdmissionError(error) || error instanceof AttachmentError) throw error;
    throw new AttachmentTransportError(error);
  } finally { await reader.cancel().catch(() => undefined); }
}

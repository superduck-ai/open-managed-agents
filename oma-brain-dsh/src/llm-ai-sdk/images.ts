import { requestImageDimensions, longEdgeDimensions, type ImageAttachmentRef, type ImageRequestTarget } from "@deepseek-ai/dsh-attachment";
import { textOnlyImageText, type ImageBlock, type LlmImageRequestPricing } from "@deepseek-ai/dsh-llm";

export interface ImageInput {
  max_images?: number;
  max_request_image_bytes?: number;
  max_image_bytes?: number;
  max_long_edge?: number;
  max_pixels?: number;
  tokens_per_megapixel?: number;
}

export function imagePolicy(config?: ImageInput) {
  return { maxImages: config?.max_images || 20, maxBytes: config?.max_request_image_bytes || 12 * 1024 * 1024,
    maxImageBytes: config?.max_image_bytes || 1024 * 1024, maxLongEdge: config?.max_long_edge || 2048,
    maxPixels: config?.max_pixels || 1024 * 1024, tokensPerMegapixel: config?.tokens_per_megapixel || 1600 };
}

export function imageTarget(ref: ImageAttachmentRef, config?: ImageInput): ImageRequestTarget {
  const policy = imagePolicy(config);
  const pixels = requestImageDimensions(ref.width, ref.height, policy.maxPixels);
  const dimensions = longEdgeDimensions(pixels.width, pixels.height, policy.maxLongEdge);
  return { ...dimensions, maxBytes: policy.maxImageBytes };
}

export function imagePlaceholder(ref: ImageAttachmentRef): string {
  return `[Image ${ref.attachmentId}, ${ref.width}x${ref.height}: omitted to meet image request limits; the image remains saved in this session.]`;
}

export function imagePricing(config?: ImageInput, acceptsImages = true): LlmImageRequestPricing {
  return { priceImages(images: readonly ImageBlock[]) {
    return images.map(block => {
      if (!acceptsImages) return { visualTokens: 0, text: textOnlyImageText(block.attachment) };
      if (block.offloaded) return { visualTokens: 0, text: imagePlaceholder(block.attachment) };
      const target = imageTarget(block.attachment, config);
      return { visualTokens: Math.ceil(target.width * target.height / 1_000_000 * imagePolicy(config).tokensPerMegapixel), text: "" };
    });
  } };
}

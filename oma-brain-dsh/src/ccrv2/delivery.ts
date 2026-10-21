import { CcrBatchUploader } from "./batch-uploader.ts";
import type { DeliveryUpdate } from "./wire.ts";

/** Delivery keeps its 64-update batch/queue limits and caller-supplied retries. */
export class CcrDeliveryUploader extends CcrBatchUploader<DeliveryUpdate> {}

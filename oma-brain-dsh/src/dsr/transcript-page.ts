import type { TranscriptPage } from "./wire.ts";
import { DsrError, decimal, object } from "./http.ts";

export async function readTranscriptPage(
  response: Response,
  from: string,
  id: string,
): Promise<TranscriptPage> {
  if (
    response.headers.get("Content-Type")?.split(";")[0] !==
    "application/x-ndjson"
  )
    throw new DsrError(0, "invalid_response");
  const nextOffset = decimal(response.headers.get("DSR-Next-Offset"), true);
  const nextFrom = response.headers.get("DSR-Next-From-Offset");
  const text = await response.text();
  if (text && !text.endsWith("\n")) throw new DsrError(0, "invalid_response");
  const lines = text ? text.slice(0, -1).split("\n") : [];
  const records = lines.map((line, index) => {
    let value: Record<string, unknown>;
    try {
      value = object(JSON.parse(line));
    } catch {
      throw new DsrError(0, "invalid_response");
    }
    const offset = BigInt(from) + BigInt(index);
    if (offset === 0n) {
      if ("seq" in value || value.id !== id)
        throw new DsrError(0, "invalid_response");
    } else if (
      !Number.isSafeInteger(value.seq) ||
      (value.seq as number) < 0 ||
      BigInt(value.seq as number) + 1n !== offset
    ) {
      throw new DsrError(0, "invalid_response");
    }
    return value;
  });
  const end = BigInt(from) + BigInt(records.length);
  if (
    end > BigInt(nextOffset) ||
    (nextFrom === null
      ? end !== BigInt(nextOffset)
      : records.length === 0 ||
        BigInt(decimal(nextFrom)) !== end ||
        end >= BigInt(nextOffset))
  )
    throw new DsrError(0, "invalid_response");
  return {
    records,
    next_offset: nextOffset,
    next_from_offset: nextFrom,
  } as unknown as TranscriptPage;
}

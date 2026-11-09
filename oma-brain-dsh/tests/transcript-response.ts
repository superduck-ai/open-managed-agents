export function transcriptResponse(
  records: readonly unknown[],
  nextOffset: string,
  nextFrom: string | null = null,
): Response {
  return new Response(
    records.map((record) => JSON.stringify(record) + "\n").join(""),
    {
      headers: {
        "Content-Type": "application/x-ndjson",
        "DSR-Next-Offset": nextOffset,
        ...(nextFrom === null ? {} : { "DSR-Next-From-Offset": nextFrom }),
      },
    },
  );
}

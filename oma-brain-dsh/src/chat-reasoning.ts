const REASONING_FIELDS = ["reasoning_content", "reasoning", "reasoning_text"] as const;

export function normalizeChatReasoning(response: Response): Response {
  if (!response.ok || !response.body) return response;
  const body = response.body.pipeThrough(new TextDecoderStream()).pipeThrough(reasoningLines()).pipeThrough(new TextEncoderStream());
  const headers = new Headers(response.headers);
  headers.delete("content-length");
  return new Response(body, { status: response.status, statusText: response.statusText, headers });
}

function reasoningLines(): TransformStream<string, string> {
  let pending = "";
  return new TransformStream({
    transform(chunk, controller) {
      if (!chunk.includes("\n")) {
        pending += chunk;
        return;
      }
      const lines = (pending + chunk).split("\n");
      pending = lines.pop() ?? "";
      controller.enqueue(`${lines.map(normalizeLine).join("\n")}\n`);
    },
    flush(controller) {
      if (pending) controller.enqueue(normalizeLine(pending));
    },
  });
}

function normalizeLine(line: string): string {
  if (!line.startsWith("data:") || !line.includes('"reasoning')) return line;
  const lineEnd = line.endsWith("\r") ? "\r" : "";
  let chunk: unknown;
  try {
    chunk = JSON.parse(line.slice(5, line.length - lineEnd.length));
  } catch {
    return line;
  }
  const choices = (chunk as { choices?: unknown } | null)?.choices;
  if (!Array.isArray(choices)) return line;
  for (const choice of choices) {
    const delta = (choice as { delta?: unknown } | null)?.delta;
    if (delta && typeof delta === "object") normalizeDelta(delta as Record<string, unknown>);
  }
  return `data: ${JSON.stringify(chunk)}${lineEnd}`;
}

function normalizeDelta(delta: Record<string, unknown>): void {
  const reasoning = REASONING_FIELDS.map((field) => delta[field]).find((value) => typeof value === "string" && value !== "");
  for (const field of REASONING_FIELDS) delete delta[field];
  if (reasoning !== undefined) delta.reasoning_content = reasoning;
}

const REASONING_FIELDS = ["reasoning_content", "reasoning", "reasoning_text"] as const;

export function normalizeChatReasoning(response: Response): Response {
  if (!response.ok || !response.body) return response;
  const body = response.body.pipeThrough(new TextDecoderStream()).pipeThrough(reasoningEvents()).pipeThrough(new TextEncoderStream());
  const headers = new Headers(response.headers);
  headers.delete("content-length");
  return new Response(body, { status: response.status, statusText: response.statusText, headers });
}

function reasoningEvents(): TransformStream<string, string> {
  let pending = "";
  let event: string[] = [];
  return new TransformStream({
    transform(chunk, controller) {
      if (!chunk.includes("\n")) {
        pending += chunk;
        return;
      }
      const lines = (pending + chunk).split("\n");
      pending = lines.pop() ?? "";
      let output = "";
      for (const line of lines) {
        event.push(line);
        if (line !== "" && line !== "\r") continue;
        output += normalizeEvent(event).map((value) => `${value}\n`).join("");
        event = [];
      }
      if (output) controller.enqueue(output);
    },
    flush(controller) {
      const rest = pending ? [...event, pending] : event;
      if (rest.length) controller.enqueue(normalizeEvent(rest).join("\n"));
    },
  });
}

function normalizeEvent(lines: string[]): string[] {
  const data = lines.filter(isData);
  if (!data.length) return lines;
  const payload = data.map(dataValue).join("\n");
  if (!payload.includes('"reasoning')) return lines;
  const normalized = normalizePayload(payload);
  if (normalized === undefined) return lines;
  const lineEnd = data[0]!.endsWith("\r") ? "\r" : "";
  let placed = false;
  return lines.flatMap((line) => {
    if (!isData(line)) return [line];
    if (placed) return [];
    placed = true;
    return [`data: ${normalized}${lineEnd}`];
  });
}

function isData(line: string): boolean {
  return line.startsWith("data:") || line === "data" || line === "data\r";
}

function dataValue(line: string): string {
  const value = line.replace(/\r$/, "").slice(5);
  return value.startsWith(" ") ? value.slice(1) : value;
}

function normalizePayload(payload: string): string | undefined {
  let chunk: unknown;
  try {
    chunk = JSON.parse(payload);
  } catch {
    return undefined;
  }
  const choices = (chunk as { choices?: unknown } | null)?.choices;
  if (!Array.isArray(choices)) return undefined;
  for (const choice of choices) {
    const delta = (choice as { delta?: unknown } | null)?.delta;
    if (delta && typeof delta === "object") normalizeDelta(delta as Record<string, unknown>);
  }
  return JSON.stringify(chunk);
}

function normalizeDelta(delta: Record<string, unknown>): void {
  const reasoning = REASONING_FIELDS.map((field) => delta[field]).find((value) => typeof value === "string" && value !== "");
  for (const field of REASONING_FIELDS) delete delta[field];
  if (reasoning !== undefined) delta.reasoning_content = reasoning;
}

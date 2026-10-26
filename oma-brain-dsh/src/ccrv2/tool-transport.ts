const CONNECT_FAILURE_CODES = new Set(["ConnectionRefused", "ECONNREFUSED", "FailedToOpenSocket", "ENOTFOUND", "EAI_AGAIN"]);

export function isConnectFailure(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error
    && CONNECT_FAILURE_CODES.has(String(error.code));
}

export async function notForwardedDetail(response: Response): Promise<string> {
  let detail = `HTTP ${response.status}`;
  try {
    const message = (await response.json())?.error?.message;
    if (typeof message === "string" && message) detail = message.slice(0, 500);
  } catch {
    return detail;
  }
  return detail;
}

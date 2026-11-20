import { HttpError, type OmaApi } from "./api.ts";
import type { Assignment } from "./assignment.ts";
import { retryDelay, sleep } from "./ccrv2/retry.ts";

const BUDGET_HEADER = "x-oma-session-budget";

export function isBudgetRejection(response: Response): boolean {
  return response.status === 400 && response.headers.get(BUDGET_HEADER) === "reached";
}

export async function waitForModelBudget(api: OmaApi, assignment: Assignment, signal?: AbortSignal): Promise<void> {
  const path = `/v2/sessions/${encodeURIComponent(assignment.session_id)}/budget`;
  for (let attempt = 0; ; attempt++) {
    if (attempt > 0) await sleep(retryDelay(attempt), signal);
    signal?.throwIfAborted();
    let state: Response;
    try {
      state = await api.requestResponse(path, assignment.ingress_token, "HEAD", undefined, undefined, signal);
    } catch (error) {
      signal?.throwIfAborted();
      if (error instanceof TypeError || error instanceof DOMException && error.name === "TimeoutError") continue;
      throw error;
    }
    if (state.status === 408 || state.status === 429 || state.status >= 500) continue;
    if (!state.ok) throw new HttpError(state.status);
    const budget = state.headers.get(BUDGET_HEADER);
    if (budget === "available") return;
    if (budget !== "reached") throw new Error("Invalid session budget state");
  }
}

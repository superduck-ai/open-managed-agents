import {DsrError, object, parseClaim, request, type Transport} from "./http.ts";
import type {Claim} from "./wire.ts";

export class DsrClaims {
    constructor(readonly transport: Transport, readonly token: string, readonly workerId: string) {
    }

    async claim(blockMs: number, signal?: AbortSignal): Promise<Claim | undefined> {
        if (!Number.isSafeInteger(blockMs) || blockMs < 0 || blockMs > 30000) throw new DsrError(0, "invalid_request");
        const response = await request(this.transport, {
            path: `/v1/dsr/claims?block_ms=${blockMs}`, token: this.token, method: "POST", body: {},
            headers: {"X-Worker-ID": this.workerId}, timeout: blockMs + 15000, signal
        }, 1);
        return response.status === 204 ? undefined : parseClaim(await response.json());
    }

    async release(claim: Claim, reason: "idle" | "shutdown" | "admission_failed" | "permanent", signal?: AbortSignal): Promise<void> {
        const response = await request(this.transport, {
            path: `/v1/dsr/claims/${encodeURIComponent(claim.code_session_id)}/release`,
            token: this.token,
            method: "POST",
            body: {epoch: claim.worker_epoch, reason},
            headers: {"X-Worker-ID": this.workerId},
            timeout: 5000,
            signal
        });
        if (object(await response.json()).ok !== true) throw new DsrError(0, "invalid_response");
    }
}

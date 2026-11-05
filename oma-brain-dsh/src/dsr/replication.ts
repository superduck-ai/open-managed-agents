// ============================================================================
// Transcript 复制端口：把本地 DSH 会话日志同步到 DSR 服务器
// ============================================================================
//
// DSH 运行时会产生两类记录：
//   - SessionHeader：会话头（没有 seq 字段），描述会话 id、父会话、模型等；
//   - SessionEvent ：带 seq 的事件（user/message、assistant/message、tool/call...）。
//
// SessionReplication 只依赖一个抽象端口 RemoteTranscriptStore：
//
//   write(rootSessionId, sessionId, records)  -> 把本地记录写到远端
//   fenced(error)             -> 判断错误是否表示“本 owner 已无权写入”
//
// 本文件用 DSR HTTP API 实现这个端口。典型场景：
//   - 会话被新 worker 接管时，新 owner 用 read 恢复历史；
//   - 会话运行过程中，本地新增事件用 write 增量上传；
//   - 旧 owner 的写入会收到 epoch_superseded 等错误，被 fenced 识别后停止重试。
//
// 例子：
//   const port = createRemoteTranscriptStore((rootSessionId) => clients.get(rootSessionId)!);
//   await port.write("cse_1", "ses_root", [
//     {id: "ses_root", version: 1, ...},              // header
//     {seq: 0, type: "user/message", data: {...}},    // event
//     {seq: 1, type: "assistant/message", data: {...}},
//   ]);
// ============================================================================

import type {SessionEvent, SessionHeader} from "@deepseek-ai/dsh-session";
import type {RemoteTranscriptStore} from "../runtime/replication.ts";
import {DsrError} from "./http.ts";
import type {DsrClient} from "./client.ts";

/**
 * 构造 DSR 版本的 transcript 端口。
 *
 * @param client 按“根会话 ID”查找 DsrClient。查找失败（owner 已不在本进程）
 *               应抛错，让复制层把写入视为 fenced，而不是写到错误的会话。
 */
export function createRemoteTranscriptStore(
    client: (rootSessionId: string) => DsrClient,
): RemoteTranscriptStore {
    return {
        read: async function* (rootSessionId, sessionId) {
            const api = client(rootSessionId);
            const directory = await api.listTranscripts();
            if (!directory.some((item) => item.dsh_session_id === sessionId))
                return;
            let cursor: string | null = "0";
            do {
                const page = await api.readTranscript(sessionId, cursor);
                yield page.header ? [page.header, ...page.events] : page.events;
                cursor = page.next_from_seq;
            } while (cursor !== null);
        },

        // 写入：先写 header，再按顺序批量追加事件。
        // 顺序必须保持，否则服务器无法保证 seq 连续。
        write: async (rootSessionId, sessionId, records) => {
            const api = client(rootSessionId);
            // header 没有 seq 字段，用它和事件区分开。
            const headers = records.filter(
                (record): record is SessionHeader => !("seq" in record),
            );
            for (const header of headers)
                await api.putHeader(sessionId, header, header.parentSession ?? null);
            const events = records.filter(
                (record): record is SessionEvent => "seq" in record,
            );
            // 每批 200 条，避免单次请求体过大；appendBounded 还会处理“按字节超限”的情况。
            for (let i = 0; i < events.length; i += 200)
                await appendBounded(api, sessionId, events.slice(i, i + 200));
        },

        // 这些错误表示“本次写入被 fence 掉了”，重试没有意义：
        //   epoch_superseded     —— 已有更新的 worker_epoch 接管；
        //   unauthorized         —— token 已失效；
        //   transcript_conflict  —— 远端 seq 与本地不一致；
        //   transcript_purged    —— 会话记录已被清理。
        fenced: (error) =>
            error instanceof DsrError &&
            [
                "epoch_superseded",
                "unauthorized",
                "transcript_conflict",
                "transcript_purged",
            ].includes(error.code),
    };
}

/**
 * 追加事件；遇到 413（请求体过大）时二分重试。
 *
 * 例：一批 200 条事件因单条内容很长而 413：
 *   1. 拆成 100 + 100 分别提交；
 *   2. 若其中一半仍 413，继续对半拆；
 *   3. 直到单条事件仍 413 —— 这时无法再拆，原样抛出。
 *
 * 条件 `events.length < 2` 就是“只剩一条还超限”的终止条件。
 * 拆分后按前后顺序依次 await，保证 seq 顺序不变。
 */
async function appendBounded(
    client: DsrClient,
    sessionId: string,
    events: readonly SessionEvent[],
): Promise<void> {
    try {
        await client.appendEvents(sessionId, events);
    } catch (error) {
        if (
            !(error instanceof DsrError) ||
            error.status !== 413 ||
            events.length < 2
        )
            throw error;
        const middle = Math.floor(events.length / 2);
        await appendBounded(client, sessionId, events.slice(0, middle));
        await appendBounded(client, sessionId, events.slice(middle));
    }
}

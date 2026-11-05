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
//   read(root, id)            -> 从远端读回某个会话的全部记录
//   write(root, id, records)  -> 把本地记录写到远端
//   fenced(error)             -> 判断错误是否表示“本 owner 已无权写入”
//
// 本文件用 DSR HTTP API 实现这个端口。典型场景：
//   - 会话被新 worker 接管时，新 owner 用 read 恢复历史；
//   - 会话运行过程中，本地新增事件用 write 增量上传；
//   - 旧 owner 的写入会收到 epoch_superseded 等错误，被 fenced 识别后停止重试。
//
// 例子：
//   const port = createRemoteTranscriptStore(id => clients.get(id)!);
//   await port.write("cse_1", "ses_root", [
//     {id: "ses_root", version: 1, ...},              // header
//     {seq: 0, type: "user/message", data: {...}},    // event
//     {seq: 1, type: "assistant/message", data: {...}},
//   ]);
// ============================================================================

import type {SessionEvent, SessionHeader} from "@deepseek-ai/dsh-session";
import type {DshLogData} from "../runtime/transcript-types.ts";
import type {RemoteTranscriptStore} from "../runtime/replication.ts";
import {DsrError} from "./http.ts";
import type {DsrClient} from "./client.ts";

/**
 * 构造 DSR 版本的 transcript 端口。
 *
 * @param client 按“根会话 ID”查找 DsrClient。查找失败（owner 已不在本进程）
 *               应抛错，让复制层把写入视为 fenced，而不是写到错误的会话。
 */
export function createRemoteTranscriptStore(client: (root: string) => DsrClient): RemoteTranscriptStore {
    return {
        // 读取：先列目录确认会话存在，再从头分页拉取事件。
        // 返回 [] 表示远端还没有这个会话的任何记录（例如全新会话），不是错误。
        read: async (root, id) => {
            const api = client(root);
            const directory = await api.listTranscripts();
            // 目录里没有该 id：远端尚无记录，直接返回空数组。
            if (!directory.some(item => item.dsh_session_id === id)) return [];
            const records: DshLogData[] = [];
            let cursor: string | null = "0";
            // 分页读取，直到 next_from_seq 为 null。
            // 例：第一页返回 events 0..199 且 next_from_seq="200"，
            //     下一页从 200 开始，直到读完。
            do {
                const page = await api.readTranscript(id, cursor);
                // header 只在第一页（from_seq=0）出现。
                if (page.header) records.push(page.header);
                records.push(...page.events);
                cursor = page.next_from_seq;
            } while (cursor !== null);
            return records;
        },

        // 写入：先写 header，再按顺序批量追加事件。
        // 顺序必须保持，否则服务器无法保证 seq 连续。
        write: async (root, id, records) => {
            const api = client(root);
            // header 没有 seq 字段，用它和事件区分开。
            const headers = records.filter((record): record is SessionHeader => !("seq" in record));
            for (const header of headers) await api.putHeader(id, header, header.parentSession ?? null);
            const events = records.filter((record): record is SessionEvent => "seq" in record);
            // 每批 200 条，避免单次请求体过大；appendBounded 还会处理“按字节超限”的情况。
            for (let i = 0; i < events.length; i += 200) await appendBounded(api, id, events.slice(i, i + 200));
        },

        // 这些错误表示“本次写入被 fence 掉了”，重试没有意义：
        //   epoch_superseded     —— 已有更新的 worker_epoch 接管；
        //   unauthorized         —— token 已失效；
        //   transcript_conflict  —— 远端 seq 与本地不一致；
        //   transcript_purged    —— 会话记录已被清理。
        fenced: error => error instanceof DsrError && ["epoch_superseded", "unauthorized", "transcript_conflict", "transcript_purged"].includes(error.code),
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
async function appendBounded(client: DsrClient, id: string, events: readonly SessionEvent[]): Promise<void> {
    try {
        await client.appendEvents(id, events);
    } catch (error) {
        if (!(error instanceof DsrError) || error.status !== 413 || events.length < 2) throw error;
        const middle = Math.floor(events.length / 2);
        await appendBounded(client, id, events.slice(0, middle));
        await appendBounded(client, id, events.slice(middle));
    }
}

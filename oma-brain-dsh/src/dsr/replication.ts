// ============================================================================
// Transcript 复制端口：把本地 DSH 会话日志同步到 DSR 服务器
// ============================================================================
//
// DSH 运行时会产生两类记录：
//   - SessionHeader：会话头（没有 seq 字段），描述会话 id、父会话、模型等；
//   - SessionEvent ：带 seq 的事件（user/message、assistant/message、tool/call...）。
//
// SessionReplication 只依赖一个抽象端口 RemoteTranscriptStore，本文件用 DSR
// HTTP API 实现这个端口。每个方法都带两个会话标识，职责不同：
//   - rootSessionId：权限域。整棵会话树共享一份 claim/lease，用它选择本进程
//     持有的 DsrClient；查不到说明本进程已失去该会话树的所有权；
//   - sessionId   ：数据域。决定读写远端哪一份 transcript（根会话自己或某个
//     子线程，对应 DSR 目录里的 dsh_session_id）。
//
// 典型场景：
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

import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import type { RemoteTranscriptStore } from "../runtime/replication.ts";
import {
  transcriptBatch,
  transcriptEventBytes,
  TranscriptUploadError,
} from "../runtime/transcript-batch.ts";
import { DsrError, decimal } from "./http.ts";
import type { DsrClient } from "./client.ts";

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
    read: async function* (rootSessionId, sessionId, signal, snapshot) {
      const api = client(rootSessionId);
      if (snapshot === null) return;
      let expected = snapshot?.nextSeq;
      if (expected === undefined) {
        const directory = await api.listTranscripts(signal);
        const transcript = directory.find(
          (item) => item.dsh_session_id === sessionId,
        );
        if (!transcript) return;
        expected = transcript.next_seq;
      }
      const end = BigInt(decimal(expected));
      let count = 0n;
      let cursor: string | null = "0";
      do {
        const page = await api.readTranscript(sessionId, cursor, signal);
        count += BigInt(page.events.length);
        if (
          count > end ||
          (page.next_from_seq === null ? count !== end : count >= end)
        )
          throw new DsrError(0, "invalid_response");
        yield page.header ? [page.header, ...page.events] : page.events;
        cursor = page.next_from_seq;
      } while (cursor !== null);
    },

    // 写入：先写 header，再按顺序批量追加事件。
    // 顺序必须保持，否则服务器无法保证 seq 连续。
    write: async (rootSessionId, sessionId, records, signal) => {
      const api = client(rootSessionId);
      // header 没有 seq 字段，用它和事件区分开。
      const headers = records.filter(
        (record): record is SessionHeader => !("seq" in record),
      );
      for (const header of headers) {
        // 单条记录超过 15 MiB 直接拒绝，重试无意义。
        transcriptEventBytes(header);
        // header 上传返回下一个可用 seq；header 不算事件，
        // 全新 transcript 的 watermark 必须是 "0"。
        const next = await upload(() =>
          api.putHeader(
            sessionId,
            header,
            header.parentSession ?? null,
            signal,
          ),
        );
        if (next !== "0")
          throw new TranscriptUploadError(
            "Unexpected transcript header watermark",
          );
      }
      const events = records.filter(
        (record): record is SessionEvent => "seq" in record,
      );
      // transcriptBatch 按条数（200）和字节数（4 MiB）双上限切批，
      // 超限的单条事件已在 transcriptEventBytes 里被拦下。
      for (let index = 0; index < events.length;) {
        const batch = transcriptBatch(events, index);
        const next = await upload(() =>
          api.appendEvents(sessionId, batch, signal),
        );
        if (BigInt(decimal(next)) < BigInt(batch.at(-1)!.seq) + 1n)
          throw new TranscriptUploadError(
            "Unexpected transcript append watermark",
          );
        index += batch.length;
      }
    },

    // 这些错误表示“本次写入没有重试价值”，复制层应释放租约、停止重试：
    //   TranscriptUploadError —— 单条记录过大或 watermark 校验失败，
    //                           同样内容再传多少次结果都一样；
    //   epoch_superseded      —— 已有更新的 worker_epoch 接管；
    //   unauthorized          —— token 已失效；
    //   transcript_conflict   —— 远端 seq 与本地不一致；
    //   transcript_purged     —— 会话记录已被清理；
    //   invalid_response      —— 服务器响应与请求对不上；
    //   4xx/410 状态码        —— 请求本身或资源状态有问题，重试不会改变结果。
    fenced: (error) =>
      error instanceof TranscriptUploadError ||
      (error instanceof DsrError &&
        ([
          "epoch_superseded",
          "unauthorized",
          "transcript_conflict",
          "transcript_gap",
          "transcript_purged",
          "invalid_response",
        ].includes(error.code) ||
          [400, 401, 403, 404, 410, 413, 422].includes(error.status))),
  };
}

// 执行一次上传请求，把“再试也没用”的客户端/服务器错误统一翻译成
// TranscriptUploadError（fenced），其余错误（网络抖动、5xx 等）原样抛出，
// 由复制层的退避重试处理。
async function upload<T>(send: () => Promise<T>): Promise<T> {
  try {
    return await send();
  } catch (error) {
    if (error instanceof DsrError && [400, 413, 422].includes(error.status))
      throw new TranscriptUploadError("Transcript batch cannot be uploaded", {
        cause: error,
      });
    throw error;
  }
}

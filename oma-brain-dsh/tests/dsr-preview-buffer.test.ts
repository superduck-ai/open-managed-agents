import { expect, jest, test } from "bun:test";
import { DsrPreviewBuffer } from "../src/dsr/preview-buffer.ts";
import type { PublicEvent } from "../src/dsr/wire.ts";

const delta = (id: string, text: string, thread?: string): PublicEvent => ({
  id,
  type: "preview.delta",
  event_id: "message",
  delta: { type: "content_delta", content: { type: "text", text } },
  ...(thread ? { session_thread_id: thread } : {}),
});

test("closing a preview buffer cancels the timer and discards unsent previews", () => {
  jest.useFakeTimers();
  try {
    const batches: PublicEvent[][] = [];
    const buffer = new DsrPreviewBuffer((events) => {
      batches.push(events);
    });
    buffer.enqueue(delta("a", "a"));
    buffer.close();
    buffer.enqueue(delta("b", "b"));
    jest.advanceTimersByTime(1000);
    buffer.flush();
    expect(batches).toEqual([]);
  } finally {
    jest.useRealTimers();
  }
});

test("100ms batching preserves original append deltas, identities and thread order", () => {
  jest.useFakeTimers();
  try {
    const batches: PublicEvent[][] = [];
    const buffer = new DsrPreviewBuffer((events) => {
      batches.push(events);
    });
    const start = {
      id: "start",
      type: "preview.start",
      event: { id: "message", type: "agent.message" },
    } as PublicEvent;
    const first = [start, delta("a", "hello"), delta("b", " world", "child")];
    first.forEach((event) => buffer.enqueue(event));
    jest.advanceTimersByTime(99);
    expect(batches).toEqual([]);
    jest.advanceTimersByTime(1);
    expect(batches).toEqual([first]);
    buffer.enqueue(delta("c", "!"));
    jest.advanceTimersByTime(100);
    expect(batches[1]).toEqual([delta("c", "!")]);
    buffer.close();
  } finally {
    jest.useRealTimers();
  }
});

test("explicit flush publishes pending previews immediately and cancels the old timer", () => {
  jest.useFakeTimers();
  try {
    const batches: PublicEvent[][] = [];
    const buffer = new DsrPreviewBuffer((events) => {
      batches.push(events);
    });
    buffer.enqueue(delta("a", "a"));
    buffer.flush();
    jest.advanceTimersByTime(100);
    expect(batches).toEqual([[delta("a", "a")]]);
    buffer.close();
  } finally {
    jest.useRealTimers();
  }
});

test("preview batches flush at the count and byte limits without modifying content", () => {
  const batches: PublicEvent[][] = [];
  const buffer = new DsrPreviewBuffer((events) => {
    batches.push(events);
  });
  for (let i = 0; i < 201; i++) buffer.enqueue(delta(String(i), "x"));
  expect(batches[0]).toHaveLength(200);
  buffer.flush();
  expect(batches[1]).toHaveLength(1);
  const big = delta("big", "x".repeat(3 * 1024 * 1024));
  buffer.enqueue(big);
  buffer.enqueue(delta("next", "y".repeat(2 * 1024 * 1024)));
  expect(batches[2]).toEqual([big]);
  buffer.flush();
  expect(batches[3]).toHaveLength(1);
  buffer.close();
});

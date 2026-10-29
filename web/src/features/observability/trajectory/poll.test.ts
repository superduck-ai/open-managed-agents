import { afterEach, expect, spyOn, test } from 'bun:test';
import { pollTrajectory } from './poll';
import type { TrajectoryPage } from '../api';

const originalFetch = globalThis.fetch;
const scope = { orgUuid: 'org', workspaceId: 'ws' };
let timerSpy: ReturnType<typeof spyOn> | undefined;
let clearSpy: ReturnType<typeof spyOn> | undefined;
const cleanups: (() => void)[] = [];

afterEach(() => {
  for (const cleanup of cleanups.splice(0)) cleanup();
  timerSpy?.mockRestore();
  clearSpy?.mockRestore();
  globalThis.fetch = originalFetch;
});

function clock() {
  let nextId = 0;
  const pending = new Map<number, { run: () => void; delay: number }>();
  timerSpy = spyOn(globalThis, 'setTimeout').mockImplementation((run, delay) => {
    const id = ++nextId;
    pending.set(id, { run: run as () => void, delay: delay ?? 0 });
    return id as unknown as ReturnType<typeof setTimeout>;
  });
  clearSpy = spyOn(globalThis, 'clearTimeout').mockImplementation((id) => {
    pending.delete(Number(id));
  });
  return {
    pending,
    async next(delay: number) {
      expect(pending.size).toBe(1);
      const [id, timer] = pending.entries().next().value!;
      expect(timer.delay).toBe(delay);
      pending.delete(id);
      timer.run();
      await Bun.sleep(0);
    },
  };
}

function start(onPage = (_page: TrajectoryPage) => {}, onFailure = (_stopped: boolean) => {}) {
  const abort = new AbortController();
  const stop = pollTrajectory(scope, { sessionId: 'sesn_1' }, abort.signal, onPage, onFailure);
  const dispose = () => {
    abort.abort();
    stop();
  };
  cleanups.push(dispose);
  return dispose;
}

function page(seq: number, more = false, segment = 'cse_1'): TrajectoryPage {
  return {
    header: null,
    events: [{ seq, time: 1, type: 'turn/start', data: { turn: seq } }],
    next_seq: seq,
    has_more: more,
    segment,
    segments: [{ id: segment, started_at: '2026-10-20T00:00:00Z' }],
  };
}

test('lane targets keep thread scope and pin the first resolved segment', async () => {
  const timer = clock();
  const responses = [
    { ...page(-1), events: [], segment: '', segments: [] },
    page(0, true),
    page(1),
    page(2, false, 'cse_newer'),
  ];
  const requests: URL[] = [];
  globalThis.fetch = (async (input) => {
    requests.push(new URL(String(input), 'http://localhost'));
    return Response.json(responses.shift());
  }) as typeof fetch;
  const abort = new AbortController();
  const stop = pollTrajectory(
    scope,
    { sessionId: 'sesn 1', threadId: 'sthr_1' },
    abort.signal,
    () => {},
    () => {},
  );
  cleanups.push(() => {
    abort.abort();
    stop();
  });
  await Bun.sleep(0);
  await timer.next(1000);
  await timer.next(1000);
  expect(requests.map((url) => url.pathname)).toEqual(
    Array(4).fill('/api/organizations/org/observability/trajectories/sesn%201'),
  );
  expect(requests.map((url) => url.searchParams.get('thread_id'))).toEqual(['sthr_1', 'sthr_1', 'sthr_1', 'sthr_1']);
  expect(requests.map((url) => url.searchParams.get('segment'))).toEqual([null, null, 'cse_1', 'cse_1']);
  expect(requests.map((url) => url.searchParams.get('after_seq'))).toEqual(['-1', '-1', '0', '1']);
});

test('permanent history gaps stop after three failures with bounded backoff', async () => {
  const timer = clock();
  const fetcher = spyOn(globalThis, 'fetch').mockImplementation(async () =>
    Response.json({ error: { message: 'trajectory history has a sequence gap' } }, { status: 503 }),
  );
  const failures: boolean[] = [];
  start(undefined, (stopped) => failures.push(stopped));
  await Bun.sleep(0);
  await timer.next(2000);
  await timer.next(4000);
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(failures).toEqual([false, false, true]);
  expect(timer.pending.size).toBe(0);
  fetcher.mockRestore();
});

test('a recovered gap resumes the cursor and resets the consecutive failure budget', async () => {
  const timer = clock();
  const responses = [page(0), null, null, page(1, true), page(2), null, null, null];
  const cursors: string[] = [];
  globalThis.fetch = (async (input) => {
    cursors.push(new URL(String(input), 'http://localhost').searchParams.get('after_seq')!);
    const response = responses.shift();
    return response ? Response.json(response) : Response.json({}, { status: 503 });
  }) as typeof fetch;
  const accepted: number[] = [];
  const failures: boolean[] = [];
  start(
    (value) => accepted.push(...value.events.map((event) => event.seq)),
    (stopped) => failures.push(stopped),
  );
  await Bun.sleep(0);
  await timer.next(1000);
  await timer.next(2000);
  await timer.next(4000);
  expect(accepted).toEqual([0, 1, 2]);
  await timer.next(1000);
  await timer.next(2000);
  await timer.next(4000);
  expect(failures).toEqual([false, false, false, false, true]);
  expect(cursors).toEqual(['-1', '0', '0', '0', '1', '2', '2', '2']);
  expect(timer.pending.size).toBe(0);
});

test('an aborted response never updates the view or schedules another query', async () => {
  const timer = clock();
  let resolve!: (response: Response) => void;
  globalThis.fetch = (() =>
    new Promise<Response>((done) => {
      resolve = done;
    })) as typeof fetch;
  const received: TrajectoryPage[] = [];
  const dispose = start((value) => received.push(value));
  dispose();
  resolve(Response.json(page(0)));
  await Bun.sleep(0);
  expect(received).toEqual([]);
  expect(timer.pending.size).toBe(0);
});

test('manual restart gets a new failure budget after automatic polling stopped', async () => {
  const timer = clock();
  let available = false;
  globalThis.fetch = (async () =>
    available ? Response.json(page(0)) : Response.json({}, { status: 503 })) as typeof fetch;
  const dispose = start();
  await Bun.sleep(0);
  await timer.next(2000);
  await timer.next(4000);
  expect(timer.pending.size).toBe(0);
  dispose();
  available = true;
  const received: TrajectoryPage[] = [];
  start((value) => received.push(value));
  await Bun.sleep(0);
  expect(received.map((value) => value.next_seq)).toEqual([0]);
  expect([...timer.pending.values()].map((value) => value.delay)).toEqual([1000]);
});

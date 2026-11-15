import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import type { ReactNode } from 'react';
import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';
import { useChannelAuthorization } from './useChannelAuthorization';
const { renderHook, act, cleanup, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  mock.restore();
});
const pending = { id: 'auth-test', status: 'pending', url: 'https://open.feishu.cn/page/launcher' };
function Wrapper({ children }: { children: ReactNode }) {
  return <I18nProvider initialLocale="zh-CN">{children}</I18nProvider>;
}
function setup() {
  resetTestDom('/');
  const timers = new Map<number, () => void>();
  let nextId = 0;
  spyOn(window, 'setInterval').mockImplementation((callback) => {
    timers.set(++nextId, callback as () => void);
    return nextId;
  });
  spyOn(window, 'clearInterval').mockImplementation((id) => {
    timers.delete(Number(id));
  });
  const onBound = mock(() => undefined);
  const onError = mock(() => undefined);
  const hook = renderHook(({ enabled }) => useChannelAuthorization('workspace-test', enabled, onBound, onError), {
    initialProps: { enabled: true },
    wrapper: Wrapper,
  });
  const tick = async () => {
    await act(async () => {
      for (const callback of [...timers.values()]) callback();
    });
  };
  return { ...hook, timers, onBound, onError, tick };
}

test('关闭预开窗口后授权仍继续，保留重新打开链接', async () => {
  const popup = {
    closed: false,
    opener: window,
    location: { replace: mock(() => undefined) },
    document: { title: '', body: { textContent: '', style: { cssText: '' } } },
  };
  spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
  let complete!: (response: Response) => void;
  globalThis.fetch = (() =>
    new Promise<Response>((resolve) => {
      complete = resolve;
    })) as typeof fetch;
  const { result, onError } = setup();
  let request!: Promise<void>;
  act(() => {
    request = result.current.authorize('测试渠道', true);
  });
  await waitFor(() => expect(typeof complete).toBe('function'));
  popup.closed = true;
  await act(async () => {
    complete(Response.json(pending));
    await request;
  });
  expect(result.current.qr?.url).toBe(pending.url);
  expect(popup.location.replace).not.toHaveBeenCalled();
  expect(onError.mock.calls.flat().filter(Boolean)).toEqual([]);
});

test('轮询不重叠，短暂失败恢复到等待状态时清除自身错误', async () => {
  let reads = 0;
  let complete!: (response: Response) => void;
  globalThis.fetch = (async (_input, init) => {
    if (init?.method === 'POST') return Response.json(pending);
    reads += 1;
    if (reads === 1) throw new TypeError('网络中断');
    return new Promise<Response>((resolve) => {
      complete = resolve;
    });
  }) as typeof fetch;
  const { result, tick, onError } = setup();
  await act(async () => {
    await result.current.authorize('测试渠道', false);
  });
  await tick();
  await waitFor(() => expect(result.current.pollError).toBe('Connection error.'));
  await tick();
  await tick();
  expect(reads).toBe(2);
  await act(async () => {
    complete(Response.json(pending));
  });
  await waitFor(() => expect(result.current.pollError).toBe(''));
  expect(result.current.qr?.status).toBe('pending');
  expect(onError.mock.calls.flat().filter(Boolean)).toEqual([]);
});

test.each(['expired', 'error', 'unknown'])('授权进入 %s 后停止轮询', async (status) => {
  globalThis.fetch = (async (_input, init) =>
    Response.json(init?.method === 'POST' ? pending : { ...pending, status })) as typeof fetch;
  const { result, tick, timers } = setup();
  await act(async () => {
    await result.current.authorize('测试渠道', false);
  });
  await tick();
  await waitFor(() => expect(result.current.qr?.status).toBe(status));
  expect(timers.size).toBe(0);
});

test('切到手动输入暂停轮询，返回授权后恢复', async () => {
  globalThis.fetch = (async (_input, init) =>
    Response.json(
      init?.method === 'POST' ? pending : { ...pending, status: 'bound', app_id: 'cli_new' },
    )) as typeof fetch;
  const { result, rerender, timers, onBound, tick } = setup();
  await act(async () => {
    await result.current.authorize('测试渠道', false);
  });
  expect(timers.size).toBe(1);
  rerender({ enabled: false });
  expect(timers.size).toBe(0);
  expect(onBound).not.toHaveBeenCalled();
  rerender({ enabled: true });
  await tick();
  await waitFor(() => expect(onBound).toHaveBeenCalledWith('cli_new'));
  expect(timers.size).toBe(0);
});

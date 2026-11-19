import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { Channel } from './api';
import { channelConnectionStatus } from './status';
import { useChannelStatusTime } from './useChannelStatusTime';

const { act, cleanup, renderHook } = await import('@testing-library/react');

afterEach(() => {
  cleanup();
  mock.restore();
});

test('查询数据未更新时，渠道仍会在连接等待期结束后变为异常', () => {
  resetTestDom('/');
  const createdAt = Date.parse('2026-11-18T07:00:00Z');
  const observedAt = createdAt + 5_000;
  const clock = spyOn(Date, 'now').mockReturnValue(observedAt);
  const timers = spyOn(window, 'setTimeout').mockReturnValue(1);
  const channel: Channel = {
    id: 'chnl_1',
    name: '飞书',
    mode: 'open',
    source: 'console',
    enabled: true,
    connected: false,
    binding_status: 'bound',
    include_thinking: false,
    include_tool_calls: false,
    reply_in_thread: false,
    pending_pairings: 0,
    created_at: new Date(createdAt).toISOString(),
  };
  const { result } = renderHook(() => useChannelStatusTime([channel], observedAt));
  expect(channelConnectionStatus(channel, result.current)).toBe('connecting');
  const [expire, delay] = timers.mock.calls[0];
  expect(delay).toBe(25_000);
  clock.mockReturnValue(createdAt + 30_000);

  if (typeof expire === 'function') act(() => expire());

  expect(channelConnectionStatus(channel, result.current)).toBe('abnormal');
  expect(timers).toHaveBeenCalledTimes(1);
});

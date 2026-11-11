import { expect, test } from 'bun:test';
import { channelConnectionStatus } from './status';
import type { Channel } from './api';

function channel(partial: Partial<Channel>): Channel {
  return {
    id: 'ch_1',
    name: 'demo',
    mode: 'open',
    source: 'console',
    enabled: true,
    connected: true,
    binding_status: 'bound',
    include_thinking: false,
    include_tool_calls: false,
    reply_in_thread: false,
    pending_pairings: 0,
    ...partial,
  };
}

test('maps bridge fields to volcano connection statuses', () => {
  expect(channelConnectionStatus(channel({}))).toBe('connected');
  expect(channelConnectionStatus(channel({ enabled: false }))).toBe('disabled');
  expect(channelConnectionStatus(channel({ binding_status: 'unbound' }))).toBe('pending_test');
  expect(channelConnectionStatus(channel({ connected: false }))).toBe('abnormal');
});

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

test('waits for a newly bound channel to connect before showing an error', () => {
  const createdAt = '2026-11-18T07:00:00Z';
  const now = Date.parse(createdAt);
  const starting = channel({ connected: false, created_at: createdAt, connection_status: 'unknown' });

  expect(channelConnectionStatus(starting, now + 5_000)).toBe('connecting');
  expect(channelConnectionStatus({ ...starting, connection_status: undefined }, now + 5_000)).toBe('connecting');
  expect(channelConnectionStatus({ ...starting, connected: true }, now + 5_000)).toBe('connected');
  expect(channelConnectionStatus({ ...starting, connection_status: 'disconnected' }, now + 5_000)).toBe('abnormal');
  expect(channelConnectionStatus(starting, now + 30_000)).toBe('abnormal');
});

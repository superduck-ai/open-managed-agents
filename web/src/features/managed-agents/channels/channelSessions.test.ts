import { expect, test } from 'bun:test';
import type { SessionApiResponse } from '../types';
import {
  channelSessionActor,
  channelSessionChat,
  channelSessionVersion,
  formatChannelTimestamp,
  selectChannelSessions,
  summarizeChannelSessions,
} from './channelSessions';

function session(patch: Partial<SessionApiResponse> & { id: string }): SessionApiResponse {
  return {
    type: 'session',
    title: patch.id,
    status: 'idle',
    created_at: '2026-11-05T15:09:01Z',
    updated_at: '2026-11-05T15:09:18Z',
    archived_at: null,
    environment_id: 'env',
    resources: [],
    agent: { id: 'agent-1', type: 'agent', version: 1 },
    ...patch,
  };
}

const channel = { id: 'ch-1', app_id: 'cli_a' };

test('按 channel_id 隔离，没有 channel_id 时才回落到 app_id', () => {
  const rows = selectChannelSessions(
    [
      session({ id: 'console', metadata: { source: 'console' } }),
      session({
        id: 'legacy',
        metadata: { source: 'lark-channel-bridge', bridge_binding_id: 'bind', scope_id: 'oc_chat' },
      }),
      session({
        id: 'this-channel',
        updated_at: '2026-11-05T16:00:00Z',
        metadata: { source: 'lark-channel-bridge', channel_id: 'ch-1', app_id: 'cli_other', open_id: 'ou_this' },
      }),
      session({
        id: 'other-channel',
        metadata: { source: 'lark-channel-bridge', channel_id: 'ch-2', app_id: 'cli_a', actor_id: 'ou_other' },
      }),
      session({
        id: 'same-app',
        metadata: { source: 'feishu', app_id: 'cli_a', open_id: 'ou_same' },
      }),
      session({
        id: 'other-app',
        metadata: { source: 'lark', app_id: 'cli_b', bridge_binding_id: 'bind-b' },
      }),
    ],
    channel,
  );
  expect(rows.map((row) => row.id)).toEqual(['this-channel', 'same-app']);
  expect(summarizeChannelSessions(rows).sourceUsers).toBe(2);
});

test('没有 channel_id 和 app_id 的旧桥接会话不进入任何渠道', () => {
  const rows = selectChannelSessions(
    [session({ id: 'bridge', metadata: { source: 'lark-channel-bridge', scope_id: 'oc_chat' } })],
    { id: 'ch-9' },
  );
  expect(rows).toEqual([]);
  expect(summarizeChannelSessions(rows)).toEqual({ total: 0, sourceUsers: 0, lastActive: null });
});

test('来源用户优先使用 open_id，否则回落到会话范围', () => {
  expect(channelSessionActor(session({ id: 'user', metadata: { open_id: ' ou_1 ', scope_id: 'oc_chat' } }))).toEqual({
    kind: 'user',
    id: 'ou_1',
  });
  expect(channelSessionActor(session({ id: 'scope', metadata: { actor_id: '', scope_id: ' oc_chat ' } }))).toEqual({
    kind: 'scope',
    id: 'oc_chat',
  });
  expect(channelSessionActor(session({ id: 'none' }))).toEqual({ kind: 'unknown' });
});

test('统计去重用户，并在缺少用户标识时不把会话范围计成用户', () => {
  const withUsers = summarizeChannelSessions([
    session({ id: 'a', metadata: { open_id: 'ou_1' }, updated_at: '2026-11-05T15:09:18Z' }),
    session({ id: 'b', metadata: { sender_id: 'ou_1' }, updated_at: '2026-11-05T15:10:00Z' }),
    session({ id: 'c', metadata: { actor_id: 'ou_2' }, updated_at: '2026-11-04T00:00:00Z' }),
  ]);
  expect(withUsers).toEqual({ total: 3, sourceUsers: 2, lastActive: '2026-11-05T15:10:00Z' });
  const scopesOnly = summarizeChannelSessions([session({ id: 'scope', metadata: { scope_id: 'oc_chat' } })]);
  expect(scopesOnly.sourceUsers).toBeNull();
  expect(summarizeChannelSessions([]).sourceUsers).toBe(0);
});

test('版本、时间和聊天类型沿用会话字段', () => {
  const row = session({
    id: 'typed',
    metadata: { chat_type: 'p2p' },
    agent: { id: 'agent-1', type: 'agent', version: 1 },
  });
  expect(channelSessionVersion(row)).toBe('V1');
  expect(channelSessionChat(row)).toBe('direct');
  expect(channelSessionChat(session({ id: 'group', metadata: { scope_type: 'group' } }))).toBe('group');
  expect(formatChannelTimestamp('2026-11-05T15:09:18Z', () => 'local-time')).toBe('local-time');
  expect(formatChannelTimestamp('not-a-date', () => 'local-time')).toBe('—');
  expect(formatChannelTimestamp(null, () => 'local-time')).toBe('—');
});

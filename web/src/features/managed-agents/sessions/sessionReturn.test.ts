import { expect, test } from 'bun:test';
import { channelSessionsReturnTo } from './sessionReturn';

const back = '/workspaces/workspace-test/channels/agent-1/ch-1?tab=sessions';

test('会话详情只接受回到当前工作区渠道会话管理的返回地址', () => {
  expect(channelSessionsReturnTo(back, 'workspace-test')).toBe(back);
  expect(channelSessionsReturnTo(back, 'other-workspace')).toBeNull();
  expect(channelSessionsReturnTo('/workspaces/workspace-test/sessions', 'workspace-test')).toBeNull();
  expect(
    channelSessionsReturnTo('https://evil.example/workspaces/workspace-test/channels/a/b?tab=sessions'),
  ).toBeNull();
  expect(channelSessionsReturnTo(`${back}&next=/login`, 'workspace-test')).toBeNull();
  expect(channelSessionsReturnTo('/workspaces/%E0%A4%A/channels/agent/ch?tab=sessions', 'workspace-test')).toBeNull();
});

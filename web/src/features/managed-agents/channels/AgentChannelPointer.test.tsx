import { expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';

const { render, screen } = await import('@testing-library/react');
const { I18nProvider } = await import('../../../shared/i18n');
const { AgentChannelPointer } = await import('./AgentChannelPointer');

test('智能体详情只指向工作区渠道页', () => {
  resetTestDom('/');
  render(
    <I18nProvider initialLocale="zh-CN">
      <AgentChannelPointer workspaceId="workspace-test" agentId="agent-1" canEdit />
    </I18nProvider>,
  );
  expect(screen.queryByText('IM 连接管理')).toBeNull();
  expect(screen.queryByRole('button', { name: '创建机器人' })).toBeNull();
  expect(screen.getByRole('link', { name: '打开工作区渠道页' }).getAttribute('href')).toBe(
    '/workspaces/workspace-test/channels',
  );
  expect(screen.getByRole('link', { name: '新增渠道' }).getAttribute('href')).toBe(
    '/workspaces/workspace-test/channels/new?agentId=agent-1',
  );
});

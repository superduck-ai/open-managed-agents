import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { wizardStepAllowed } from './channelInput';

const { render, screen, fireEvent, cleanup } = await import('@testing-library/react');
const { I18nProvider } = await import('../../../shared/i18n');
const { StepStrategy, WizardStepper } = await import('./ChannelWizardForm');
const { ChannelCredentials } = await import('./ChannelFields');

afterEach(cleanup);

const empty = { name: '', agentId: '', environmentId: '', app_id: '', app_secret: '' };
const pendingQr = { binding: 'qr', saved: null, qrStatus: null };
const named = { ...empty, name: '飞书渠道' };

test('未完成前面的必填项时不能进入后面的步骤', () => {
  expect(wizardStepAllowed(1, empty, pendingQr)).toBe(true);
  expect(wizardStepAllowed(2, empty, pendingQr)).toBe(false);
  expect(wizardStepAllowed(2, named, pendingQr)).toBe(false);
  expect(wizardStepAllowed(2, named, { ...pendingQr, qrStatus: 'bound' })).toBe(true);
  expect(
    wizardStepAllowed(2, named, { binding: 'qr', saved: { binding_status: 'bound', app_id: 'cli_a' }, qrStatus: null }),
  ).toBe(true);
  expect(wizardStepAllowed(2, named, { binding: 'credentials', saved: null })).toBe(false);
  expect(wizardStepAllowed(2, { ...named, app_id: 'cli_a' }, { binding: 'credentials', saved: null })).toBe(false);
  expect(
    wizardStepAllowed(2, { ...named, app_id: 'cli_a', app_secret: 'sec' }, { binding: 'credentials', saved: null }),
  ).toBe(true);
  expect(
    wizardStepAllowed(
      2,
      { ...named, app_id: 'cli_a' },
      {
        binding: 'credentials',
        saved: { binding_status: 'bound', app_id: 'cli_a' },
      },
    ),
  ).toBe(true);
  expect(
    wizardStepAllowed(
      2,
      { ...named, app_id: 'cli_b' },
      {
        binding: 'credentials',
        saved: { binding_status: 'bound', app_id: 'cli_a' },
      },
    ),
  ).toBe(false);
  expect(wizardStepAllowed(3, { name: '  ', agentId: 'agent-1', environmentId: 'env-1' }, pendingQr)).toBe(false);
});

test('点击未解锁的进度步骤不会切换', () => {
  resetTestDom('/');
  const onStepChange = mock(() => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <WizardStepper
        step={1}
        canOpen={(step) => wizardStepAllowed(step, empty, pendingQr)}
        onStepChange={onStepChange}
      />
    </I18nProvider>,
  );
  const locked = screen.getByRole('button', { name: '2 配置渠道策略' });
  fireEvent.click(locked);
  expect(onStepChange).not.toHaveBeenCalled();
  expect(locked.hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '1 连接渠道应用' }));
  expect(onStepChange).toHaveBeenCalledWith(1);
});

test('策略步骤用卡片切换回复展示和渠道模式', () => {
  resetTestDom('/');
  const onChange = mock(() => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <StepStrategy
        value={{
          name: '飞书渠道',
          mode: 'open',
          include_thinking: false,
          include_tool_calls: true,
          reply_in_thread: true,
          app_id: '',
          app_secret: '',
          channelType: 'feishu',
          agentId: '',
          environmentId: '',
        }}
        onChange={onChange}
      />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('switch', { name: '展示思考过程' }));
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ include_thinking: true }));
  fireEvent.click(screen.getByRole('switch', { name: '配对模式' }));
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ mode: 'pairing' }));
  expect(screen.getByRole('switch', { name: '开放模式' }).getAttribute('aria-checked')).toBe('true');
  expect(screen.queryByRole('switch', { name: '自动注入近期聊天内容' })).toBeNull();
});

test('有效应用 ID 的配置应用打开飞书应用详情', () => {
  resetTestDom('/');
  const value = {
    name: '飞书渠道',
    mode: 'open' as const,
    include_thinking: true,
    include_tool_calls: true,
    reply_in_thread: true,
    app_id: 'cli_aa6c7433e1b8dbf1',
    app_secret: '',
  };
  const { rerender } = render(
    <I18nProvider initialLocale="zh-CN">
      <ChannelCredentials value={value} onChange={() => undefined} bound={false} workspaceId="workspace-test" />
    </I18nProvider>,
  );
  expect(screen.getByRole('link', { name: /配置应用/ }).getAttribute('href')).toBe(
    'https://open.feishu.cn/app/cli_aa6c7433e1b8dbf1/baseinfo',
  );
  expect(screen.getByRole('button', { name: '验证 App Secret' }).hasAttribute('disabled')).toBe(true);
  rerender(
    <I18nProvider initialLocale="zh-CN">
      <ChannelCredentials
        value={{ ...value, app_id: 'not-valid' }}
        onChange={() => undefined}
        bound={false}
        workspaceId="workspace-test"
      />
    </I18nProvider>,
  );
  expect(screen.queryByRole('link', { name: /配置应用/ })).toBeNull();
  expect(screen.getByRole('button', { name: /配置应用/ }).hasAttribute('disabled')).toBe(true);
});

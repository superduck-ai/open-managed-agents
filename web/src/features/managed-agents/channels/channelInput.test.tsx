import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { wizardStepAllowed } from './channelInput';

const { render, screen, fireEvent, cleanup } = await import('@testing-library/react');
const { I18nProvider } = await import('../../../shared/i18n');
const { WizardStepper } = await import('./ChannelWizardForm');

afterEach(cleanup);

const empty = { name: '', agentId: '', environmentId: '' };

test('未完成前面的必填项时不能进入后面的步骤', () => {
  expect(wizardStepAllowed(1, empty)).toBe(true);
  expect(wizardStepAllowed(2, empty)).toBe(false);
  expect(wizardStepAllowed(3, empty)).toBe(false);
  expect(wizardStepAllowed(2, { ...empty, name: '飞书渠道' })).toBe(true);
  expect(wizardStepAllowed(3, { ...empty, name: '飞书渠道' })).toBe(true);
  expect(wizardStepAllowed(3, { name: '  ', agentId: 'agent-1', environmentId: 'env-1' })).toBe(false);
});

test('点击未解锁的进度步骤不会切换', () => {
  resetTestDom('/');
  const onStepChange = mock(() => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <WizardStepper step={1} canOpen={(step) => wizardStepAllowed(step, empty)} onStepChange={onStepChange} />
    </I18nProvider>,
  );
  const locked = screen.getByRole('button', { name: '2 配置渠道策略' });
  fireEvent.click(locked);
  expect(onStepChange).not.toHaveBeenCalled();
  expect(locked.hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '1 连接渠道应用' }));
  expect(onStepChange).toHaveBeenCalledWith(1);
});

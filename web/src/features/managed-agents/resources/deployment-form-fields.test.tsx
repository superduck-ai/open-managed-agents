import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';
import { initialFormValues } from './model';
const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');
const { DeploymentFormFields } = await import('./deployment-form-fields');
afterEach(cleanup);

test('localizes configuration and scheduling and gives Chinese labels unique controls', () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/deployments');
  const values = { ...initialFormValues('deployments'), triggerType: 'schedule' as const, timezone: 'Asia/Shanghai' };
  render(
    <I18nProvider initialLocale="zh-CN">
      <DeploymentFormFields
        values={values}
        workspaceId="default"
        agents={[]}
        environments={[]}
        vaults={[]}
        memoryStores={[]}
        loadingOptions={false}
        onChange={() => {}}
      />
    </I18nProvider>,
  );
  expect(screen.getByRole('heading', { name: '配置' })).toBeTruthy();
  expect(screen.getByRole('combobox', { name: '频率' }).textContent).toContain('工作日');
  expect(screen.getByText('接下来 5 次运行')).toBeTruthy();
  expect(screen.getByLabelText('名称').id).not.toBe(screen.getByLabelText('初始消息').id);
  expect(screen.getByRole('combobox', { name: '环境' }).id).not.toBe(screen.getByRole('combobox', { name: '频率' }).id);
  expect(document.querySelector('time')?.textContent).toContain('年');
  fireEvent.click(screen.getByRole('button', { name: '编辑 Cron' }));
  expect(screen.getByLabelText('Cron 表达式')).toBeTruthy();
});

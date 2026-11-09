import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
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
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider initialLocale="zh-CN">
        <DeploymentFormFields
          values={values}
          workspaceId="default"
          environments={[]}
          vaults={[]}
          memoryStores={[]}
          loadingOptions={false}
          editing={false}
          onChange={() => {}}
        />
      </I18nProvider>
    </QueryClientProvider>,
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

test('searches timezone identifiers and changes only the timezone', async () => {
  const { DeploymentScheduleFields } = await import('./deployment-schedule-fields');
  const changes: Array<Record<string, unknown>> = [];
  render(
    <I18nProvider initialLocale="en">
      <DeploymentScheduleFields
        values={{ ...initialFormValues('deployments'), triggerType: 'schedule', timezone: 'UTC' }}
        onChange={(patch) => changes.push(patch)}
      />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'Timezone' }));
  fireEvent.change(screen.getByPlaceholderText('Search options...'), { target: { value: 'Shanghai' } });
  fireEvent.click(await screen.findByRole('option', { name: 'Asia/Shanghai' }));
  expect(changes).toEqual([{ timezone: 'Asia/Shanghai' }]);
});

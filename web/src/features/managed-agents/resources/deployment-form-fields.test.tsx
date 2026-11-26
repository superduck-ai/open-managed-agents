import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';
import { WorkspaceContext } from '../../../shared/workspaces/context';
import { defaultNativeCapabilities } from '../../../shared/workspaces/runtime';
import { budgetUpdateBody, entityBudgetDisplay, entityBudgetUsdInput } from './budget';
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

test('allows clearing and typing two-digit minutes in hourly schedule mode without padding mid-edit', async () => {
  const { DeploymentScheduleFields } = await import('./deployment-schedule-fields');
  const changes: Array<Record<string, unknown>> = [];
  render(
    <I18nProvider initialLocale="en">
      <DeploymentScheduleFields
        values={{
          ...initialFormValues('deployments'),
          triggerType: 'schedule',
          cronExpression: '15 * * * *',
          timezone: 'UTC',
        }}
        onChange={(patch) => changes.push(patch)}
      />
    </I18nProvider>,
  );
  const minute = screen.getByLabelText('At minute') as HTMLInputElement;
  expect(minute.value).toBe('15');
  fireEvent.change(minute, { target: { value: '' } });
  expect(minute.value).toBe('');
  fireEvent.change(minute, { target: { value: '5' } });
  expect(minute.value).toBe('5');
  fireEvent.change(minute, { target: { value: '53' } });
  expect(minute.value).toBe('53');
  expect(changes).toEqual([{ cronExpression: '' }, { cronExpression: '5 * * * *' }, { cronExpression: '53 * * * *' }]);
});

test('uses credits budget and hides credential vaults when runtime disables credential_vaults', () => {
  resetTestDom('https://oma.duck.ai/workspaces/qoder/deployments');
  const qoderWorkspace = {
    id: 'wrkspc_qoder',
    name: 'Qoder Space',
    display_color: '#2563eb',
    is_default: false,
    created_at: '2026-11-27T00:00:00Z',
    runtime: {
      profile_uuid: '00000000-0000-4000-8000-000000000099',
      profile_id: 'rprof_qoder',
      profile_name: 'Qoder Cloud CN',
      provider_kind: 'qoder_cloud',
      capabilities: {
        ...defaultNativeCapabilities,
        deployments: true,
        memory_stores: true,
        credential_vaults: false,
        session_budget: 'credits' as const,
      },
    },
  };
  const values = { ...initialFormValues('deployments'), budgetMode: 'credits' as const };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider initialLocale="en">
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'org-1',
            workspaces: [qoderWorkspace],
            activeWorkspace: qoderWorkspace,
            activeWorkspaceId: qoderWorkspace.id,
            isLoading: false,
            error: null,
            selectWorkspace: () => undefined,
            createWorkspace: async () => qoderWorkspace,
            refreshWorkspaces: async () => undefined,
          }}
        >
          <DeploymentFormFields
            values={values}
            workspaceId={qoderWorkspace.id}
            environments={[]}
            vaults={[]}
            memoryStores={[]}
            loadingOptions={false}
            editing={false}
            onChange={() => {}}
          />
        </WorkspaceContext.Provider>
      </I18nProvider>
    </QueryClientProvider>,
  );

  expect(screen.getByLabelText(/Budget \(Credits\)/)).toBeTruthy();
  expect(screen.queryByText('Credential vaults')).toBeNull();

  const creditEntity = { budget: { type: 'limit', max_credit_cost: '42.50' } };
  expect(entityBudgetUsdInput(creditEntity)).toBe('42.50');
  expect(entityBudgetDisplay(creditEntity as never)).toBe('42.50 Credits');
  expect(
    budgetUpdateBody({
      ...initialFormValues('deployments'),
      budgetMode: 'credits',
      originalBudgetUsd: '42.50',
      budgetUsd: '100',
    }),
  ).toEqual({ type: 'limit', max_credit_cost: '100.00' });
});

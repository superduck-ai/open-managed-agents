import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin } from '../../../test/platform-admin';
import { PlatformAdminClient } from '../../../shared/api/platform-admin';
import { PlatformAdminContext } from '../context';
import { SystemSkillDrawer } from './SystemSkillDrawer';
import type { SystemSkill } from './types';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});
const skill: SystemSkill = {
  id: 'demo',
  display_title: 'Demo',
  publisher: 'OMA',
  source_url: '',
  license: '',
  listed: true,
  default_version: '1.0.0',
  revision: 8,
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
};

function renderDrawer() {
  const client = new PlatformAdminClient(() => {});
  const changed = mock(async () => {});
  renderPlatformAdmin(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: { uuid: 'operator', name: 'Operator', email: 'test@example.com' },
            platform_access: { role: 'super_admin', permissions: ['platform:access'] },
            csrf_token: 'test',
          },
        }}
      >
        <SystemSkillDrawer skillID="demo" onClose={() => {}} onUpload={() => {}} onChanged={changed} />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
  return changed;
}

test('修订冲突保留错误，禁止以已刷新的版本静默重试', async () => {
  globalThis.fetch = mock(async (_url: RequestInfo | URL, options?: RequestInit) =>
    options?.method === 'POST'
      ? Response.json({ error: { code: 'revision_conflict', message: '目录已被更新' } }, { status: 409 })
      : Response.json(skill),
  ) as typeof fetch;
  const changed = renderDrawer();
  fireEvent.click(await screen.findByRole('button', { name: '下架技能' }));
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '目录已被更新');
  expect((screen.getByRole('button', { name: '确认' }) as HTMLButtonElement).disabled).toBe(true);
  expect(changed).not.toHaveBeenCalled();
});

test('下架确认提交被确认时的修订号和操作', async () => {
  let body: unknown;
  globalThis.fetch = mock(async (_url: RequestInfo | URL, options?: RequestInit) => {
    if (options?.method === 'POST') body = JSON.parse(String(options.body));
    return Response.json(skill);
  }) as typeof fetch;
  const changed = renderDrawer();
  fireEvent.click(await screen.findByRole('button', { name: '下架技能' }));
  expect(screen.getByText(/已有 Agent 引用和会话继续可用/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  await waitFor(() => expect(changed).toHaveBeenCalled());
  expect(body).toEqual({ action: 'unlist', version: '', revision: 8 });
});

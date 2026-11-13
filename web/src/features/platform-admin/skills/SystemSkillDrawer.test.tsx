import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin } from '../../../test/platform-admin';
import { PlatformAdminClient } from '../../../shared/api/platform-admin';
import { PlatformAdminContext } from '../context';
import { SystemSkillDrawer } from './SystemSkillDrawer';
import type { SystemSkill } from './types';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const originalCreate = URL.createObjectURL;
const originalRevoke = URL.revokeObjectURL;
const originalClick = window.HTMLAnchorElement.prototype.click;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  URL.createObjectURL = originalCreate;
  URL.revokeObjectURL = originalRevoke;
  window.HTMLAnchorElement.prototype.click = originalClick;
});
const skill: SystemSkill = {
  id: 'demo',
  bundled: true,
  display_title: 'Demo',
  listed: true,
  default_version: '1.0.0',
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
};

const content = {
  name: 'demo',
  description: 'Demo skill',
  files: ['demo/SKILL.md'],
  skill_md: '# Demo source',
  sha256: 'hash',
  size_bytes: 13,
  existing: null,
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

test('上下架操作失败保留错误并允许重试', async () => {
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) =>
    String(url).endsWith('/content')
      ? Response.json(content)
      : options?.method === 'POST'
        ? Response.json({ error: { code: 'unavailable', message: '操作失败' } }, { status: 503 })
        : Response.json(skill),
  ) as typeof fetch;
  const changed = renderDrawer();
  fireEvent.click(await screen.findByRole('button', { name: '下架技能' }));
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '操作失败');
  expect((screen.getByRole('button', { name: '确认' }) as HTMLButtonElement).disabled).toBe(false);
  expect(changed).not.toHaveBeenCalled();
});

test('下架确认提交操作', async () => {
  let body: unknown;
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
    if (options?.method === 'POST') body = JSON.parse(String(options.body));
    return Response.json(String(url).endsWith('/content') ? content : skill);
  }) as typeof fetch;
  const changed = renderDrawer();
  fireEvent.click(await screen.findByRole('button', { name: '下架技能' }));
  expect(screen.getByText(/已有 Agent 引用和会话继续可用/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  await waitFor(() => expect(changed).toHaveBeenCalled());
  expect(body).toEqual({ action: 'unlist' });
});

test('详情直接展示技能包原文，不再显示标签页和预置来源', async () => {
  const downloads: string[] = [];
  URL.createObjectURL = mock(() => 'blob:skill');
  URL.revokeObjectURL = mock(() => {});
  window.HTMLAnchorElement.prototype.click = () => {};
  globalThis.fetch = mock(async (url: RequestInfo | URL) => {
    const path = String(url);
    if (path.endsWith('?download=1')) {
      downloads.push(path);
      return new Response('zip');
    }
    return Response.json(path.endsWith('/content') ? content : skill);
  }) as typeof fetch;
  renderDrawer();
  expect(await screen.findByRole('region', { name: 'demo/SKILL.md' })).toHaveProperty(
    'textContent',
    expect.stringContaining('# Demo source'),
  );
  expect(screen.queryByRole('tab')).toBeNull();
  expect(screen.queryByText('项目预置')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '下载 ZIP' }));
  await waitFor(() => expect(downloads).toContain('/api/platform-admin/skills/demo/versions/1.0.0/content?download=1'));
  await waitFor(() => expect(screen.getByRole('button', { name: '下载 ZIP' })).toHaveProperty('disabled', false));
});

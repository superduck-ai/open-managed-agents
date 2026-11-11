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
  revision: 8,
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

test('修订冲突保留错误，禁止以已刷新的版本静默重试', async () => {
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) =>
    String(url).endsWith('/content')
      ? Response.json(content)
      : options?.method === 'POST'
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
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
    if (options?.method === 'POST') body = JSON.parse(String(options.body));
    return Response.json(String(url).endsWith('/content') ? content : skill);
  }) as typeof fetch;
  const changed = renderDrawer();
  fireEvent.click(await screen.findByRole('button', { name: '下架技能' }));
  expect(screen.getByText(/已有 Agent 引用和会话继续可用/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  await waitFor(() => expect(changed).toHaveBeenCalled());
  expect(body).toEqual({ action: 'unlist', version: '', revision: 8 });
});

test('详情默认展示技能包原文，不再显示概览和预置来源', async () => {
  globalThis.fetch = mock(async (url: RequestInfo | URL) =>
    Response.json(String(url).endsWith('/content') ? content : skill),
  ) as typeof fetch;
  renderDrawer();
  expect(await screen.findByRole('region', { name: 'demo/SKILL.md' })).toHaveProperty(
    'textContent',
    expect.stringContaining('# Demo source'),
  );
  expect(screen.getByRole('tab', { name: '包内容' }).getAttribute('aria-selected')).toBe('true');
  expect(screen.queryByRole('tab', { name: '概览' })).toBeNull();
  expect(screen.queryByText('项目预置')).toBeNull();
  expect(screen.queryByText(/正在查看/)).toBeNull();
});

test('历史版本提示与 ZIP 下载跟随当前预览，返回默认版本后隐藏提示', async () => {
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
    if (path.includes('/versions?'))
      return Response.json({
        data: ['1.0.0', '0.9.0'].map((version) => ({
          id: version,
          version,
          description: '',
          created_at: skill.created_at,
        })),
        has_more: false,
      });
    return Response.json(path.endsWith('/content') ? content : skill);
  }) as typeof fetch;
  renderDrawer();
  await screen.findByRole('region', { name: 'demo/SKILL.md' });
  for (const version of ['0.9.0', '1.0.0']) {
    fireEvent.click(screen.getByRole('tab', { name: '版本' }));
    fireEvent.click(await screen.findByRole('button', { name: version, exact: true }));
    await screen.findByRole('region', { name: 'demo/SKILL.md' });
    expect(screen.queryByText('正在查看 0.9.0') !== null).toBe(version === '0.9.0');
    fireEvent.click(screen.getByRole('button', { name: '下载 ZIP' }));
    await waitFor(() =>
      expect(downloads).toContain(`/api/platform-admin/skills/demo/versions/${version}/content?download=1`),
    );
    await waitFor(() => expect(screen.getByRole('button', { name: '下载 ZIP' })).toHaveProperty('disabled', false));
  }
});

test.each([
  { actor_name: '张三', actor_id: 'user_operator', label: '张三' },
  { actor_name: '', actor_id: 'user_operator', label: 'user_operator' },
  { actor_name: '', actor_id: '', label: 'actor-uuid' },
])('操作记录显示真实账号：$label', async ({ label, ...identity }) => {
  globalThis.fetch = mock(async (url: RequestInfo | URL) => {
    if (String(url).includes('/events'))
      return Response.json({
        data: [
          {
            id: 'event-one',
            actor_uuid: 'actor-uuid',
            ...identity,
            action: 'list',
            version: '',
            created_at: skill.created_at,
          },
        ],
        has_more: false,
      });
    return Response.json(String(url).endsWith('/content') ? content : skill);
  }) as typeof fetch;
  renderDrawer();
  fireEvent.click(await screen.findByRole('tab', { name: '操作记录' }));
  expect(await screen.findByText('操作人: ' + label)).toHaveProperty('title', identity.actor_id || 'actor-uuid');
});

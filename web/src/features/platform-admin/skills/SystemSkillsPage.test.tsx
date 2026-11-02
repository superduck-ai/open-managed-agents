import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { renderPlatformAdmin } from '../../../test/platform-admin';
import { resetTestDom } from '../../../test/setup';
import { PlatformAdminClient } from '../../../shared/api/platform-admin';
import { PlatformAdminContext } from '../context';
import { SystemSkillsPage } from './SystemSkillsPage';
import type { SystemSkill } from './types';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('系统技能列表点击详情、关闭后打开导入仍可响应', async () => {
  resetTestDom('https://oma.duck.ai/platform-admin/skills');
  Object.assign(globalThis, { scrollTo: () => {} });
  const skill: SystemSkill = {
    id: 'demo',
    display_title: 'Demo',
    listed: true,
    default_version: '1.0.0',
    revision: 1,
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  };
  globalThis.fetch = mock(async (url: RequestInfo | URL) =>
    Response.json(String(url).endsWith('/demo') ? skill : { data: [skill], has_more: false }),
  ) as typeof fetch;
  const root = createRootRoute();
  const route = createRoute({
    getParentRoute: () => root,
    path: '/platform-admin/skills',
    component: SystemSkillsPage,
    validateSearch: (search: Record<string, unknown>) => ({
      skill: typeof search.skill === 'string' ? search.skill : undefined,
    }),
  });
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory({ initialEntries: ['/platform-admin/skills'] }),
  });
  const client = new PlatformAdminClient(() => {});
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
        <RouterProvider router={router} />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
  expect(await screen.findByRole('button', { name: 'Demo', exact: true })).toBeTruthy();
  expect(screen.queryByRole('columnheader', { name: '发布者' })).toBeNull();
  expect(screen.queryByRole('textbox', { name: '发布者' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Demo', exact: true }));
  expect(await screen.findByRole('heading', { name: 'Demo' })).toBeTruthy();
  expect(router.state.location.search).toMatchObject({ skill: 'demo' });
  fireEvent.click(screen.getByRole('button', { name: '关闭' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: '导入技能' }));
  expect(await screen.findByRole('dialog', { name: '导入技能' })).toBeTruthy();
});

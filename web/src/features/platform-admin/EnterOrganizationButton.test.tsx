import { afterEach, expect, mock, test } from 'bun:test';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { resetTestDom } from '../../test/setup';
import { OrganizationContext } from '../../shared/organizations/context';
import { I18nProvider } from '../../shared/i18n';
import { EnterOrganizationButton } from './EnterOrganizationButton';

const { render, cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);
function mount(enter: (org: string) => Promise<boolean>) {
  resetTestDom('https://oma.duck.ai/detail');
  Object.assign(globalThis, { scrollTo: () => {} });
  const root = createRootRoute();
  const detail = createRoute({
    getParentRoute: () => root,
    path: '/detail',
    component: () => <EnterOrganizationButton organizationUuid="target" />,
  });
  const workbench = createRoute({ getParentRoute: () => root, path: '/workbench', component: () => <p>工作台</p> });
  const router = createRouter({
    routeTree: root.addChildren([detail, workbench]),
    history: createMemoryHistory({ initialEntries: ['/detail'] }),
  });
  render(
    <I18nProvider initialLocale="zh-CN">
      <OrganizationContext.Provider
        value={{
          memberships: [],
          switching: false,
          error: null,
          switchOrganization: async () => false,
          retry: async () => false,
          enterOrganization: enter,
        }}
      >
        <RouterProvider router={router} />
      </OrganizationContext.Provider>
    </I18nProvider>,
  );
  return router;
}

test('进入失败显示错误并保留详情页', async () => {
  const router = mount(async () => {
    throw new Error('denied');
  });
  fireEvent.click(await screen.findByRole('button', { name: '进入组织工作台' }));
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(router.state.location.pathname).toBe('/detail');
});

test('组织详情入口安装目标成功后导航工作台', async () => {
  const enter = mock(async () => true);
  const router = mount(enter);
  fireEvent.click(await screen.findByRole('button', { name: '进入组织工作台' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/workbench'));
  expect(enter).toHaveBeenCalledWith('target');
});

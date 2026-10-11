import { afterEach, beforeEach, expect, test } from 'bun:test';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { WorkspaceSettingsRedirect } from './WorkspaceSettingsRedirect';

const { cleanup, render, screen, waitFor } = await import('@testing-library/react');

const originalScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo');
beforeEach(() => Object.assign(globalThis, { scrollTo: () => undefined }));
afterEach(() => {
  cleanup();
  if (originalScrollTo) Object.defineProperty(globalThis, 'scrollTo', originalScrollTo);
  else Reflect.deleteProperty(globalThis, 'scrollTo');
});

test.each([
  { resource: 'keys' as const, workspaceId: 'workspace_default' },
  { resource: 'keys' as const, workspaceId: 'wrkspc_research' },
  { resource: 'webhooks' as const, workspaceId: 'wrkspc_research' },
])('redirects $resource to the active workspace $workspaceId', async ({ resource, workspaceId }) => {
  resetTestDom('https://oma.duck.ai/alias');
  const root = createRootRoute();
  const alias = createRoute({
    getParentRoute: () => root,
    path: 'alias',
    component: () => <WorkspaceSettingsRedirect resource={resource} />,
  });
  const destination = createRoute({
    getParentRoute: () => root,
    path: 'settings/workspaces/$workspaceId/$resource',
    component: () => <div>Workspace settings destination</div>,
  });
  const router = createRouter({
    routeTree: root.addChildren([alias, destination]),
    history: createMemoryHistory({ initialEntries: ['/alias'] }),
  });
  await router.load();
  const activeWorkspace = { ...defaultWorkspace, id: workspaceId };
  render(
    <WorkspaceContext.Provider
      value={{
        activeWorkspace,
        activeWorkspaceId: workspaceId,
        workspaces: [activeWorkspace],
        isLoading: false,
        error: null,
        selectWorkspace: () => undefined,
        createWorkspace: async () => activeWorkspace,
        refreshWorkspaces: async () => undefined,
      }}
    >
      <RouterProvider router={router} />
    </WorkspaceContext.Provider>,
  );
  await screen.findByText('Workspace settings destination');
  await waitFor(() => expect(router.state.location.pathname).toBe(`/settings/workspaces/${workspaceId}/${resource}`));
  expect(router.history.length).toBe(1);
});

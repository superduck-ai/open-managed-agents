import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { OrganizationMemberRemoval } from './OrganizationMemberRemoval';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function renderRemoval() {
  resetTestDom('https://oma.duck.ai/settings/members');
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <OrganizationMemberRemoval
        orgUuid="org_one"
        organizationName="Team One"
        csrfToken="csrf-test"
        member={{ id: 'user_member', name: 'Member', email: 'member@example.com', role: 'user' }}
      />
    </QueryClientProvider>,
  );
}

async function openRemoval() {
  const trigger = screen.getByRole('button', { name: 'More actions for Member' });
  trigger.focus();
  fireEvent.keyDown(trigger, { key: 'ArrowDown' });
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove member' }));
  await screen.findByRole('alertdialog');
}

test('取消移除不会发送请求', async () => {
  let calls = 0;
  globalThis.fetch = (async () => {
    calls++;
    return Response.json({});
  }) as typeof fetch;
  renderRemoval();
  await openRemoval();
  expect(screen.getByText(/Are you sure you want to remove/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  await waitFor(() =>
    expect(Boolean(document.querySelector('[data-slot="alert-dialog-content"][data-open]'))).toBe(false),
  );
  expect(calls).toBe(0);
});

test('后端拒绝最后管理员移除时保留弹窗和错误', async () => {
  globalThis.fetch = (async () =>
    Response.json(
      {
        error: 'last_organization_admin',
        message: 'The last organization administrator cannot be removed or demoted.',
      },
      { status: 409 },
    )) as typeof fetch;
  renderRemoval();
  await openRemoval();
  fireEvent.click(screen.getByRole('button', { name: 'Remove', exact: true }));
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(screen.getByRole('alertdialog')).toBeTruthy();
});

test('确认后向当前组织发送带 CSRF 的 DELETE', async () => {
  let request: { url: string; init?: RequestInit } | undefined;
  globalThis.fetch = (async (url, init) => {
    request = { url: String(url), init };
    return Response.json({ id: 'user_member', type: 'user_deleted' });
  }) as typeof fetch;
  renderRemoval();
  await openRemoval();
  expect(request).toBeUndefined();
  fireEvent.click(screen.getByRole('button', { name: 'Remove', exact: true }));
  await waitFor(() =>
    expect(Boolean(document.querySelector('[data-slot="alert-dialog-content"][data-open]'))).toBe(false),
  );
  expect(request?.url).toContain('/api/console/organizations/org_one/members/user_member');
  expect(request?.init?.method).toBe('DELETE');
  expect(new Headers(request?.init?.headers).get('X-CSRF-Token')).toBe('csrf-test');
});

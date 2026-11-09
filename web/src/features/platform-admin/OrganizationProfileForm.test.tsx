import { afterEach, expect, mock, test } from 'bun:test';
import { renderPlatformAdmin as render } from '../../test/platform-admin';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { PlatformAdminContext } from './context';
import { OrganizationProfileForm } from './OrganizationProfileForm';
import type { Organization } from './types';

const { cleanup, screen, fireEvent } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('后台刷新不替换编辑版本，冲突保留输入', async () => {
  resetTestDom('https://oma.duck.ai/platform-admin');
  let payload: unknown;
  globalThis.fetch = mock(async (_url: RequestInfo | URL, options?: RequestInit) => {
    payload = JSON.parse(String(options?.body));
    return Response.json({ error: { code: 'version_conflict', message: '组织信息已变更' } }, { status: 409 });
  }) as unknown as typeof fetch;
  const client = new PlatformAdminClient(() => {});
  const queries = new QueryClient();
  const organization: Organization = {
    uuid: 'organization',
    name: '原名称',
    website: '',
    industry: '',
    version: 7,
    admin_count: 1,
    member_count: 1,
    workspace_count: 1,
    created_at: '',
  };
  const saved = mock();
  function Form({ version }: { version: number }) {
    return (
      <QueryClientProvider client={queries}>
        <PlatformAdminContext.Provider
          value={{
            client,
            bootstrap: {
              account: { uuid: 'operator', email: '', name: '' },
              platform_access: { role: 'super_admin', permissions: ['platform:access'] },
              csrf_token: '',
            },
          }}
        >
          <OrganizationProfileForm organization={{ ...organization, version }} onSaved={saved} onCancel={() => {}} />
        </PlatformAdminContext.Provider>
      </QueryClientProvider>
    );
  }
  const view = render(<Form version={7} />);
  fireEvent.change(screen.getByLabelText('组织名称'), { target: { value: '编辑名称' } });
  view.rerender(<Form version={8} />);
  fireEvent.click(screen.getByRole('button', { name: '保存资料' }));
  expect((await screen.findByRole('alert')).textContent).toContain('输入已保留');
  expect(payload).toMatchObject({ name: '编辑名称', expected_version: 7 });
  expect((screen.getByLabelText('组织名称') as HTMLInputElement).value).toBe('编辑名称');
  expect(saved).not.toHaveBeenCalled();
  queries.clear();
});

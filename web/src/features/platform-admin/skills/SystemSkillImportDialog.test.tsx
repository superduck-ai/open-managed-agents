import { StrictMode } from 'react';
import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin } from '../../../test/platform-admin';
import { PlatformAdminClient } from '../../../shared/api/platform-admin';
import { PlatformAdminContext } from '../context';
import { SystemSkillImportDialog } from './SystemSkillImportDialog';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function renderImport() {
  const client = new PlatformAdminClient(() => {});
  client.setCSRFToken('test-token');
  const onClose = mock();
  const onImported = mock(async () => {});
  renderPlatformAdmin(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}
    >
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: { uuid: 'operator', name: 'Operator', email: 'test@example.com' },
            platform_access: { role: 'super_admin', permissions: ['platform:access', 'platform:skills:manage'] },
            csrf_token: 'test-token',
          },
        }}
      >
        <StrictMode>
          <SystemSkillImportDialog onClose={onClose} onImported={onImported} />
        </StrictMode>
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
  return { onClose, onImported };
}

test('检查失败保留文件且禁止导入', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({ error: { code: 'invalid_package', message: 'Missing SKILL.md' } }, { status: 400 }),
  ) as typeof fetch;
  renderImport();
  fireEvent.change(screen.getByLabelText('选择技能包'), { target: { files: [new File(['invalid'], 'broken.zip')] } });
  expect(await screen.findByText('Missing SKILL.md')).toBeTruthy();
  expect(screen.getByRole('button', { name: '导入', exact: true })).toHaveProperty('disabled', true);
  expect(screen.queryByRole('button', { name: '下一步' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '移除 broken.zip' }));
  expect(screen.queryByText('broken.zip')).toBeNull();
});
test('统一两步导入传递图标，省略版本和草稿配置', async () => {
  const bodies: FormData[] = [];
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
    if (String(url).endsWith('/inspect')) return Response.json({ name: 'demo', existing: null });
    bodies.push(options?.body as FormData);
    return Response.json({ skill: { id: 'demo', revision: 1 }, duplicate: false });
  }) as typeof fetch;
  const { onImported } = renderImport();
  fireEvent.change(screen.getByLabelText('选择技能包'), { target: { files: [new File(['zip'], 'demo.zip')] } });
  expect(await screen.findByText('检查通过')).toBeTruthy();
  expect(bodies).toHaveLength(0);
  expect(screen.queryByRole('checkbox')).toBeNull();
  expect(screen.queryByLabelText(/版本/)).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '更换demo图标' }));
  fireEvent.click(screen.getByRole('button', { name: '文档', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: '导入', exact: true }));
  await waitFor(() => expect(onImported).toHaveBeenCalledWith('demo'));
  expect(bodies).toHaveLength(1);
  expect(bodies[0].get('icon_key')).toBe('file-text');
  expect(bodies[0].get('version')).toBeNull();
  expect(bodies[0].get('publish')).toBeNull();
  expect(bodies[0].get('revision')).toBe('0');
});
test('批量部分失败后只重试失败包并保留最新修订号', async () => {
  let writes = 0;
  let inspections = 0;
  const revisions: string[] = [];
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
    if (String(url).endsWith('/inspect')) {
      inspections++;
      return Response.json({
        name: 'demo',
        existing: { id: 'demo', display_title: 'Demo', revision: inspections > 2 ? 4 : 3 },
      });
    }
    revisions.push(String((options?.body as FormData).get('revision')));
    writes++;
    if (writes === 2)
      return Response.json({ error: { code: 'unavailable', message: 'Temporary failure' } }, { status: 503 });
    return Response.json({ skill: { id: 'demo', revision: writes === 1 ? 4 : 5 }, duplicate: false });
  }) as typeof fetch;
  const { onClose } = renderImport();
  fireEvent.change(screen.getByLabelText('选择技能包'), {
    target: { files: [new File(['one'], 'one.zip'), new File(['two'], 'two.zip')] },
  });
  await waitFor(() => expect(screen.getAllByText('检查通过')).toHaveLength(2));
  fireEvent.click(screen.getByRole('button', { name: '导入', exact: true }));
  expect(await screen.findByText('Temporary failure')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  await waitFor(() => expect(screen.queryByText('Temporary failure')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: '导入', exact: true }));
  await waitFor(() => expect(onClose).toHaveBeenCalled());
  expect(revisions).toEqual(['3', '4', '4']);
});

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { toast } from 'sonner';
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
        <SystemSkillImportDialog onClose={onClose} onImported={onImported} />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
  return { onClose, onImported };
}

test('错误包保留在列表，移除前不能进入确认', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({ error: { code: 'invalid_package', message: 'Missing SKILL.md' } }, { status: 400 }),
  ) as typeof fetch;
  renderImport();
  fireEvent.change(screen.getByLabelText('选择 ZIP 包'), { target: { files: [new File(['invalid'], 'broken.zip')] } });
  expect(await screen.findByText('Missing SKILL.md')).toBeTruthy();
  expect(screen.getByText('broken.zip')).toBeTruthy();
  expect((screen.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: '移除 broken.zip' }));
  expect(screen.queryByText('broken.zip')).toBeNull();
});

for (const duplicate of [true, false]) {
  test(
    duplicate
      ? '重复上传明确提示已存在，关闭导入且不声称新建版本'
      : '真实选择文件、结构检查、填写版本和发布使用分开的接口',
    async () => {
      const notice = spyOn(toast, 'info').mockImplementation(() => 'duplicate-notice');
      try {
        const writes: RequestInit[] = [];
        globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
          if (String(url).endsWith('/inspect'))
            return Response.json({
              name: 'demo',
              description: 'Demo',
              sha256: 'hash',
              size_bytes: 100,
              files: ['demo/SKILL.md'],
              skill_md: '# Demo',
              existing: null,
            });
          writes.push(options ?? {});
          return Response.json({ skill: { id: 'demo', revision: 2 }, duplicate });
        }) as typeof fetch;
        const { onClose, onImported } = renderImport();
        fireEvent.change(screen.getByLabelText('选择 ZIP 包'), {
          target: { files: [new File(['zip content'], 'demo.zip')] },
        });
        expect(await screen.findByText('检查通过')).toBeTruthy();
        expect(writes).toHaveLength(0);
        for (const label of ['发布者', '授权说明', '来源说明或原始链接', '更新说明'])
          expect(screen.queryByLabelText(label)).toBeNull();
        expect((screen.getByLabelText('demo.zip 的版本') as HTMLInputElement).value).toBe('1.0.0');
        expect((screen.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(false);
        expect(screen.queryByText(/支持多个独立 ZIP/)).toBeNull();
        expect(screen.queryByText(/结构检查不能替代运行验收/)).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: '下一步' }));
        expect(screen.getByRole('checkbox', { name: '导入后立即发布' })).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '导入并发布' }));
        await waitFor(() => expect(onImported).toHaveBeenCalledWith('demo'));
        expect(onClose).toHaveBeenCalled();
        expect(writes).toHaveLength(1);
        const body = writes[0].body as FormData;
        expect(body.get('version')).toBe('1.0.0');
        expect(body.get('publish')).toBe('true');
        for (const field of ['publisher', 'source_url', 'license', 'notes']) expect(body.has(field)).toBe(false);
        expect(new Headers(writes[0].headers).get('X-CSRF-Token')).toBe('test-token');
        if (duplicate) expect(notice).toHaveBeenCalledWith('demo 1.0.0 已存在，无需重复导入，内容未改变。');
        else expect(notice).not.toHaveBeenCalled();
      } finally {
        notice.mockRestore();
      }
    },
  );
}

for (const [value, message] of [
  ['', '请输入版本号'],
  ['latest', '版本号格式无效'],
  ['bad/version', '版本号格式无效'],
]) {
  test(`版本 ${JSON.stringify(value)} 就地提示，修正后可继续且无需重新检查包`, async () => {
    const fetch = mock(async () => Response.json({ name: 'demo', existing: null }));
    globalThis.fetch = fetch as typeof globalThis.fetch;
    renderImport();
    fireEvent.change(screen.getByLabelText('选择 ZIP 包'), {
      target: { files: [new File(['fixture'], 'demo.zip')] },
    });
    expect(await screen.findByText('检查通过')).toBeTruthy();
    const version = screen.getByLabelText('demo.zip 的版本') as HTMLInputElement;
    fireEvent.change(version, { target: { value } });
    expect((screen.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));
    expect(screen.getByRole('alert').textContent).toBe(message);
    expect(version.getAttribute('aria-invalid')).toBe('true');
    expect(document.activeElement).toBe(version);
    expect(screen.getByText('检查通过')).toBeTruthy();
    fireEvent.change(version, { target: { value: '2.0.0' } });
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));
    expect(screen.getByRole('button', { name: '导入并发布' })).toBeTruthy();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
}

test('已有技能追加版本保持空值，填写新版本后才能确认', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({ name: 'demo', existing: { id: 'demo', display_title: 'Demo', revision: 3 } }),
  ) as typeof fetch;
  renderImport();
  fireEvent.change(screen.getByLabelText('选择 ZIP 包'), {
    target: { files: [new File(['fixture'], 'demo.zip')] },
  });
  expect(await screen.findByText('检查通过')).toBeTruthy();
  const version = screen.getByLabelText('demo.zip 的版本') as HTMLInputElement;
  expect(version.value).toBe('');
  expect(version.placeholder).toBe('请输入版本号');
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.getByRole('alert').textContent).toBe('请输入版本号');
  fireEvent.change(version, { target: { value: '2.0.0' } });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  expect(screen.getByRole('button', { name: '导入并发布' })).toBeTruthy();
});

function directoryFile(path: string, content: string | ArrayBuffer = 'fixture') {
  const file = new File([content], path.split('/').at(-1)!);
  Object.defineProperty(file, 'webkitRelativePath', { value: path });
  return file;
}

test('文件夹缺少根目录 SKILL.md 时保留错误且不发送文件', async () => {
  const fetch = mock(async () => Response.json({}));
  globalThis.fetch = fetch as typeof globalThis.fetch;
  renderImport();
  fireEvent.change(screen.getByLabelText('选择技能文件夹'), {
    target: { files: [directoryFile('demo/references/SKILL.md')] },
  });
  expect(await screen.findByText('请选择单个技能文件夹，根目录需包含 SKILL.md。')).toBeTruthy();
  expect(fetch).not.toHaveBeenCalled();
  expect(screen.getByRole('button', { name: '下一步' }).hasAttribute('disabled')).toBe(true);
});

test('文件夹按所有文件的总大小限制上传', async () => {
  const fetch = mock(async () => Response.json({}));
  globalThis.fetch = fetch as typeof globalThis.fetch;
  renderImport();
  fireEvent.change(screen.getByLabelText('选择技能文件夹'), {
    target: {
      files: [directoryFile('demo/SKILL.md'), directoryFile('demo/data.bin', new ArrayBuffer(8 * 1024 * 1024))],
    },
  });
  expect(await screen.findByText('ZIP 包或技能文件夹的总大小不得超过 8 MiB。')).toBeTruthy();
  expect(fetch).not.toHaveBeenCalled();
});

test('文件夹检查和导入均保留完整目录、中文路径和空辅助文件，超过20个文件仍作为一个技能', async () => {
  const requests: FormData[] = [];
  globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
    requests.push(options?.body as FormData);
    if (String(url).endsWith('/inspect'))
      return Response.json({
        name: 'demo',
        description: 'Demo',
        files: ['demo/SKILL.md'],
        skill_md: '# Demo',
        existing: null,
      });
    return Response.json({ skill: { id: 'demo', revision: 1 }, duplicate: false });
  }) as typeof fetch;
  const { onImported } = renderImport();
  const files = [
    directoryFile('demo/SKILL.md'),
    directoryFile('demo/references/中文.md'),
    directoryFile('demo/scripts/__init__.py', ''),
    ...Array.from({ length: 21 }, (_, index) => directoryFile(`demo/references/${index}.txt`)),
  ];
  const input = screen.getByLabelText('选择技能文件夹');
  expect(input.hasAttribute('webkitdirectory')).toBe(true);
  fireEvent.change(input, { target: { files } });
  expect(await screen.findByText('检查通过')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('demo 的版本'), { target: { value: '1.0.0' } });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(screen.getByRole('button', { name: '导入并发布' }));
  await waitFor(() => expect(onImported).toHaveBeenCalledWith('demo'));
  expect(requests).toHaveLength(2);
  for (const body of requests) {
    const uploaded = body.getAll('files[]') as File[];
    const payload = await new Request('http://localhost', { method: 'POST', body }).text();
    const paths = Array.from(payload.matchAll(/filename="([^"]*)"/g), (match) => match[1]);
    expect(paths).toEqual(files.map((file) => file.webkitRelativePath));
    expect(uploaded[2].size).toBe(0);
  }
});

for (const recheck of [false, true]) {
  test(`同技能部分导入成功后重试保留修订号，重新检查=${recheck}`, async () => {
    const revisions: string[] = [];
    let writes = 0;
    let inspections = 0;
    globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
      if (String(url).endsWith('/inspect')) {
        inspections++;
        return Response.json({
          name: 'demo',
          existing: { id: 'demo', display_title: 'Demo', revision: inspections > 2 ? 8 : 3 },
        });
      }
      const body = options?.body as FormData;
      revisions.push(String(body.get('revision')));
      writes++;
      if (writes === 2)
        return Response.json({ error: { code: 'unavailable', message: 'Temporary failure' } }, { status: 503 });
      return Response.json({ skill: { id: 'demo', revision: writes === 1 ? 4 : 9 }, duplicate: false });
    }) as typeof fetch;
    const { onClose } = renderImport();
    fireEvent.change(screen.getByLabelText('选择 ZIP 包'), {
      target: { files: [new File(['one'], 'one.zip'), new File(['two'], 'two.zip')] },
    });
    await waitFor(() => expect(screen.getAllByText('检查通过')).toHaveLength(2));
    fireEvent.change(screen.getByLabelText('one.zip 的版本'), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText('two.zip 的版本'), { target: { value: '2' } });
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));
    fireEvent.click(screen.getByRole('button', { name: '导入并发布' }));
    expect(await screen.findByText('Temporary failure')).toBeTruthy();
    if (recheck) {
      fireEvent.click(screen.getByRole('button', { name: '重新检查' }));
      await waitFor(() => expect(screen.queryByText('Temporary failure')).toBeNull());
    } else {
      fireEvent.change(screen.getByLabelText('two.zip 的版本'), { target: { value: '3' } });
    }
    fireEvent.click(screen.getByRole('button', { name: '下一步' }));
    fireEvent.click(screen.getByRole('button', { name: '导入并发布' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(revisions).toEqual(['3', '4', recheck ? '8' : '4']);
  });
}

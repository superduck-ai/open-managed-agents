import { afterEach, expect, mock, test } from 'bun:test';
import '../../test/setup';
import { cancelScopeRequests, setConsoleRequestContext } from '@/shared/api/client';
import { downloadSkillPackage, skillErrorMessage, workspaceSkillContentUrl } from './api';

const originalFetch = globalThis.fetch;
const originalCreate = URL.createObjectURL;
const originalRevoke = URL.revokeObjectURL;
const originalClick = window.HTMLAnchorElement.prototype.click;
afterEach(() => {
  globalThis.fetch = originalFetch;
  URL.createObjectURL = originalCreate;
  URL.revokeObjectURL = originalRevoke;
  window.HTMLAnchorElement.prototype.click = originalClick;
  setConsoleRequestContext({});
});

test('下载失败不生成 ZIP 文件', async () => {
  URL.createObjectURL = mock(() => 'blob:invalid');
  globalThis.fetch = mock(async () =>
    Response.json({ error: { message: '没有权限' } }, { status: 403 }),
  ) as typeof fetch;
  await expect(downloadSkillPackage('/api/skill')).rejects.toThrow('没有权限');
  expect(URL.createObjectURL).not.toHaveBeenCalled();
});

test('工作区切换后不下载旧作用域的响应', async () => {
  let finish!: (value: Response) => void;
  URL.createObjectURL = mock(() => 'blob:old');
  globalThis.fetch = mock(
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  ) as typeof fetch;
  const pending = downloadSkillPackage('/api/skill', 'workspace');
  cancelScopeRequests();
  finish(new Response('old'));
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  expect(URL.createObjectURL).not.toHaveBeenCalled();
});

for (const workspaceId of ['workspace', undefined]) {
  test(`下载完整 ZIP 并正确隔离请求头 ${workspaceId || 'platform-admin'}`, async () => {
    setConsoleRequestContext({ organizationUuid: 'organization', workspaceId: 'other', csrfToken: 'token' });
    let captured: RequestInit | undefined;
    let requestUrl = '';
    globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
      captured = options;
      requestUrl = String(url);
      return new Response('ZIP-content', { headers: { 'Content-Disposition': 'attachment; filename=demo.zip' } });
    }) as typeof fetch;
    URL.createObjectURL = mock(() => 'blob:zip');
    URL.revokeObjectURL = mock(() => {});
    const downloaded: string[] = [];
    window.HTMLAnchorElement.prototype.click = function () {
      downloaded.push(this.download);
    };
    const url = workspaceId
      ? workspaceSkillContentUrl(workspaceId, 'demo', '1')
      : '/api/platform-admin/skills/demo/versions/1/content';
    await downloadSkillPackage(url, workspaceId);
    expect(requestUrl).toBe(`${url}?download=1`);
    expect(downloaded).toEqual(['demo.zip']);
    expect(captured?.credentials).toBe('include');
    const headers = new Headers(captured?.headers);
    expect(headers.get('X-Workspace-ID')).toBe(workspaceId ?? null);
    expect(headers.get('X-Organization-UUID')).toBe(workspaceId ? 'organization' : null);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:zip');
  });
}

test('重复名称的接口错误转换为操作提示，不展示内部字段名', () => {
  const msg = (key: string, fallback: string) =>
    key === 'skills.import.duplicateName' ? '已存在同名技能，请修改名称或使用“更新技能”。' : fallback;
  for (const error of [
    new Error('Skill cannot reuse an existing display_title: browser'),
    { message: 'Skill cannot reuse an existing display_title: browser' },
  ]) {
    expect(skillErrorMessage(error, msg)).toBe('已存在同名技能，请修改名称或使用“更新技能”。');
  }
  expect(skillErrorMessage(new Error('Storage unavailable'), msg)).toBe('Storage unavailable');
});

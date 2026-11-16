import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin } from '../../test/platform-admin';
import { SkillContent } from './SkillContent';
import { SkillCodeBlock } from './SkillCodeBlock';

const { cleanup, screen, within, fireEvent, waitFor } = await import('@testing-library/react');
afterEach(cleanup);

test('保留 Markdown 完整原文和目录层级，不渲染标题、HTML、图片或链接', () => {
  const source =
    '---\nname: demo\ndescription: Test\n---\n# 一级标题\n\n## 二级标题\n\n- 整理 **记录**。\n\n<script>alert(1)</script>\n\n![图片](https://example.com/image.png)\n\n[参考](https://example.com)';
  const { container } = renderPlatformAdmin(
    <QueryClientProvider client={new QueryClient()}>
      <SkillContent
        contentKey="demo"
        loadFile={async () => {
          throw new Error('not requested');
        }}
        content={{ files: ['demo/SKILL.md', 'demo/references/guide.md', 'demo/scripts/run.py'], skill_md: source }}
      />
    </QueryClientProvider>,
  );
  const code = container.querySelector('pre.skill-source > code.language-markdown');
  expect(code?.textContent).toBe(source);
  expect(code?.querySelector('.hljs-section')?.textContent).toContain('一级标题');
  expect(code?.querySelector('h1, h2, strong, script, img, a')).toBeNull();
  expect(screen.queryByRole('heading', { name: '一级标题' })).toBeNull();
  const files = screen.getByRole('navigation', { name: '文件目录' });
  expect(within(files).getByRole('button', { name: 'demo' }).getAttribute('aria-expanded')).toBe('true');
  expect(within(files).getByRole('button', { name: 'SKILL.md' }).getAttribute('aria-current')).toBe('true');
  expect(within(files).getByRole('button', { name: 'references' }).getAttribute('aria-expanded')).toBe('false');
  expect(within(files).queryByRole('button', { name: 'guide.md' })).toBeNull();
  fireEvent.click(within(files).getByRole('button', { name: 'references' }));
  expect(within(files).getByText('references').closest('li')?.textContent).toContain('guide.md');
  fireEvent.click(within(files).getByRole('button', { name: 'scripts' }));
  expect(within(files).getByText('run.py').closest('[title]')?.getAttribute('title')).toBe('demo/scripts/run.py');
  fireEvent.click(screen.getByRole('button', { name: /文件目录/ }));
  expect(screen.getByRole('button', { name: /文件目录/ }).getAttribute('aria-expanded')).toBe('false');
});

function renderFiles(loadFile: (path: string, signal: AbortSignal) => Promise<import('./api').SkillFilePreview>) {
  const view = renderPlatformAdmin(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <SkillContent
        contentKey="demo/1"
        loadFile={loadFile}
        content={{
          files: [
            'demo/SKILL.md',
            'demo/scripts/main.py',
            'demo/Main.java',
            'demo/tool.bin',
            'demo/large.txt',
            'demo/empty.txt',
          ],
          skill_md: '# Original Markdown',
        }}
      />
    </QueryClientProvider>,
  );
  return view;
}

test('预览失败后可重试，不保留上个文件内容', async () => {
  const load = mock(async (path: string) => ({ path, content: 'class Main {}', previewable: true, reason: '' }));
  load.mockImplementationOnce(async () => {
    throw new Error('network');
  });
  const { container } = renderFiles(load);
  fireEvent.click(screen.getByRole('button', { name: 'Main.java' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('文件预览加载失败'));
  expect(container.querySelector('pre')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  await waitFor(() => expect(container.querySelector('pre')?.textContent).toBe('class Main {}'));
  expect(container.querySelector('code.language-java .hljs-keyword')?.textContent).toBe('class');
});

test('二进制与大文件显示无法预览，空文本正常展示', async () => {
  const { container } = renderFiles(async (path) => ({
    path,
    content: '',
    previewable: path.endsWith('empty.txt'),
    reason: path.endsWith('large.txt') ? 'too_large' : 'binary',
  }));
  fireEvent.click(screen.getByRole('button', { name: 'tool.bin' }));
  expect(await screen.findByText('无法预览此文件。')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'large.txt' }));
  expect(await screen.findByText('文件超过 256 KiB，无法预览。')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'empty.txt' }));
  await waitFor(() => expect(container.querySelector('pre > code')?.textContent).toBe(''));
});

test('默认展示 SKILL.md，点击代码文件读取完整路径，返回 Markdown 不发请求', async () => {
  const source = 'print("<script>不可执行</script>")\n';
  const load = mock(async (path: string) => ({ path, content: source, previewable: true, reason: '' }));
  const { container } = renderFiles(load);
  expect(container.querySelector('pre')?.textContent).toBe('# Original Markdown');
  expect(load).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'scripts' }));
  fireEvent.click(screen.getByRole('button', { name: 'main.py' }));
  await waitFor(() => expect(container.querySelector('pre')?.textContent).toBe(source));
  expect(load.mock.calls[0][0]).toBe('demo/scripts/main.py');
  expect(container.querySelector('pre script')).toBeNull();
  expect(container.querySelector('code.language-python .hljs-built_in')?.textContent).toBe('print');
  expect(screen.getByRole('button', { name: 'main.py' }).getAttribute('aria-current')).toBe('true');
  fireEvent.click(screen.getByRole('button', { name: 'scripts' }));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'main.py' })).toBeNull());
  expect(container.querySelector('pre')?.textContent).toBe(source);
  fireEvent.click(screen.getByRole('button', { name: 'SKILL.md' }));
  expect(container.querySelector('pre')?.textContent).toBe('# Original Markdown');
  expect(load).toHaveBeenCalledTimes(1);
});

test('未知扩展名回退纯文本，保留 CRLF 和 HTML 原文', () => {
  const source = '<script>alert(1)</script>\r\n---\r\n';
  const { container } = renderPlatformAdmin(<SkillCodeBlock source={source} path="demo/source.constructor" />);
  const code = container.querySelector('code.language-plaintext');
  expect(code?.textContent).toBe(source);
  expect(code?.querySelector('script')).toBeNull();
});

test('Markdown 高亮保留 frontmatter 和 CRLF 原始换行', () => {
  const source = '---\r\nname: demo\r\ndescription: Test\r\n---\r\n# Title\r\n';
  const { container } = renderPlatformAdmin(<SkillCodeBlock source={source} path="demo/SKILL.md" />);
  expect(container.querySelector('code')?.textContent).toBe(source);
});

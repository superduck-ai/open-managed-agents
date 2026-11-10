import { afterEach, expect, test } from 'bun:test';
import { renderPlatformAdmin } from '../../test/platform-admin';
import { SkillContent } from './SkillContent';

const { cleanup, screen, within, fireEvent } = await import('@testing-library/react');
afterEach(cleanup);

test('保留 Markdown 完整原文和目录层级，不渲染标题、HTML、图片或链接', () => {
  const source =
    '---\nname: demo\ndescription: Test\n---\n# 一级标题\n\n## 二级标题\n\n- 整理 **记录**。\n\n<script>alert(1)</script>\n\n![图片](https://example.com/image.png)\n\n[参考](https://example.com)';
  const { container } = renderPlatformAdmin(
    <SkillContent
      content={{ files: ['demo/SKILL.md', 'demo/references/guide.md', 'demo/scripts/run.py'], skill_md: source }}
    />,
  );
  const code = container.querySelector('pre.skill-source > code.language-markdown');
  expect(code?.textContent).toBe(source);
  expect(code?.querySelector('h1, h2, strong, script, img, a')).toBeNull();
  expect(screen.queryByRole('heading', { name: '一级标题' })).toBeNull();
  const files = screen.getByRole('navigation', { name: '文件目录' });
  expect(within(files).getByText('references').closest('li')?.textContent).toContain('guide.md');
  expect(within(files).getByText('run.py').closest('[title]')?.getAttribute('title')).toBe('demo/scripts/run.py');
  fireEvent.click(screen.getByRole('button', { name: /文件目录/ }));
  expect(screen.getByRole('button', { name: /文件目录/ }).getAttribute('aria-expanded')).toBe('false');
});

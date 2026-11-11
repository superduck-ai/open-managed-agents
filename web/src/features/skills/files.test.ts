import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import '../../test/setup';
import { pickSkillFolder, skillFilePath, skillPackageBody } from './files';

const originalPicker = Object.getOwnPropertyDescriptor(window, 'showDirectoryPicker');
afterEach(() => {
  mock.restore();
  if (originalPicker) Object.defineProperty(window, 'showDirectoryPicker', originalPicker);
  else Reflect.deleteProperty(window, 'showDirectoryPicker');
});

test('目录选择被取消时不回退到批量上传确认', async () => {
  Object.defineProperty(window, 'showDirectoryPicker', {
    configurable: true,
    value: mock(async () => {
      throw new DOMException('Cancelled', 'AbortError');
    }),
  });
  await expect(pickSkillFolder()).rejects.toMatchObject({ name: 'AbortError' });
});

test('不支持目录选择接口时保留兼容入口', async () => {
  Object.defineProperty(window, 'showDirectoryPicker', { configurable: true, value: undefined });
  expect(pickSkillFolder()).toBeNull();
});

test('只读目录选择保留根目录和嵌套文件路径，不提前上传', async () => {
  const script = new File(['print(1)'], 'run.py');
  const markdown = new File(['# Test'], 'SKILL.md');
  const picker = mock(async () => ({
    name: 'deep-research',
    async *values() {
      yield { kind: 'file', name: 'SKILL.md', getFile: async () => markdown };
      yield {
        kind: 'directory',
        name: 'scripts',
        async *values() {
          yield { kind: 'file', name: 'run.py', getFile: async () => script };
        },
      };
    },
  }));
  Object.defineProperty(window, 'showDirectoryPicker', { configurable: true, value: picker });
  const files = (await pickSkillFolder())!;
  expect(picker).toHaveBeenCalledWith({ mode: 'read' });
  expect(files.map(skillFilePath)).toEqual(['deep-research/SKILL.md', 'deep-research/scripts/run.py']);
  expect(await files[0].text()).toBe('# Test');
  expect(await files[1].text()).toBe('print(1)');
  const append = spyOn(FormData.prototype, 'append');
  skillPackageBody(files);
  expect(append).toHaveBeenCalledWith('files[]', markdown, 'deep-research/SKILL.md');
  expect(append).toHaveBeenCalledWith('files[]', script, 'deep-research/scripts/run.py');
});

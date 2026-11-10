import { expect, test } from 'bun:test';
import { skillErrorMessage } from './errors';

const msg = (key: string, fallback: string) =>
  key === 'skills.import.duplicateName' ? '已存在同名技能，请修改名称或使用“更新技能”。' : fallback;

test('重复名称的接口错误转换为操作提示，不展示内部字段名', () => {
  for (const error of [
    new Error('Skill cannot reuse an existing display_title: browser'),
    { message: 'Skill cannot reuse an existing display_title: browser' },
  ]) {
    expect(skillErrorMessage(error, msg)).toBe('已存在同名技能，请修改名称或使用“更新技能”。');
  }
});

test('保留其他错误的具体原因', () => {
  expect(skillErrorMessage(new Error('Storage unavailable'), msg)).toBe('Storage unavailable');
});

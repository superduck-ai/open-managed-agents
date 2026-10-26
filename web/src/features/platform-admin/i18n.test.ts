import { expect, test } from 'bun:test';
import en from '../../shared/i18n/messages/en.json';
import zh from '../../shared/i18n/messages/zh-CN.json';

test('后台两种语言的翻译完整，动态参数保持一致', () => {
  const english = Object.entries(en).filter(([key]) => key.startsWith('platformAdmin.'));
  const chinese = zh as Record<string, string>;
  expect(english.length).toBeGreaterThan(100);
  for (const [key, value] of english) {
    expect(chinese[key]?.trim()).toBeTruthy();
    const parameters = (text: string) => [...text.matchAll(/\{([a-zA-Z][\w]*)[,}]/g)].map((match) => match[1]).sort();
    expect(parameters(chinese[key])).toEqual(parameters(value));
  }
});

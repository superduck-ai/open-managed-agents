import { expect, test } from 'bun:test';
import { documentationSearchRecords, navigationSearchRecords, searchMatches } from './search-data';

test('API references are excluded while API key navigation remains searchable', () => {
  const docs = documentationSearchRecords('zh-CN');
  expect(docs.length).toBe(17);
  expect(docs.some((record) => record.href.includes('/api/'))).toBe(false);
  expect(docs.some((record) => searchMatches(record, '启动会话'))).toBe(true);
  const pages = navigationSearchRecords(undefined, undefined, 'research', (_id, text) => text);
  expect(pages.find((record) => searchMatches(record, 'API keys'))?.href).toBe('/settings/workspaces/research/keys');
});

test('search handles names, IDs, multiple terms and nonmatching terms', () => {
  const record = {
    id: 'agent_123',
    name: 'Research assistant',
    context: 'Default',
    href: '/agents',
    kind: 'resource' as const,
  };
  expect(searchMatches(record, 'AGENT_123')).toBe(true);
  expect(searchMatches(record, 'research Default')).toBe(true);
  expect(searchMatches(record, 'unknown')).toBe(false);
});

test('page results retain module icons and single-line scope context', () => {
  const pages = navigationSearchRecords(undefined, undefined, 'research', (_id, text) => text, 'Research');
  expect(pages.find((record) => record.id === '/agents')?.context).toBe('Research');
  expect(pages.find((record) => record.id === '/files')?.context).toBe('Resources');
  expect(pages.find((record) => record.id === '/settings/llm')?.context).toBe('Models and access');
  expect(pages.find((record) => record.id === '/agents')?.icon).toBeTruthy();
  expect(pages.find((record) => record.id === '/files')?.icon).not.toBe(
    pages.find((record) => record.id === '/agents')?.icon,
  );
});

test('every indexed document resolves to an existing localized guide', async () => {
  for (const locale of ['zh-CN', 'en']) {
    for (const record of documentationSearchRecords(locale)) {
      const url = new URL(record.href);
      expect(url.origin).toBe('https://oma.mintlifysite.com');
      expect(url.pathname.startsWith(`/docs/${locale === 'zh-CN' ? 'zh' : 'en'}/`)).toBe(true);
      expect(await Bun.file(new URL(`../../../..${url.pathname}.mdx`, import.meta.url)).exists()).toBe(true);
    }
  }
});

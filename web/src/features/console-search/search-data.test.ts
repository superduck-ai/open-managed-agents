import { expect, test } from 'bun:test';
import { consoleNavigation } from '../../shared/navigation/navigation';
import { settingsGroups } from '../../shared/navigation/settings-navigation';
import { productIcons } from '../../shared/icons/product-icons';
import { documentationSearchRecords, navigationSearchRecords, searchMatches } from './search-data';

const account = {
  uuid: 'a',
  email_address: 'a@example.com',
  effectiveOrganizationUuid: 'org',
  permissions: ['organization:manage', 'members:manage'],
};

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

test('organization management stays out of search results for ordinary users', () => {
  const member = { ...account, permissions: [] };
  expect(
    navigationSearchRecords(member, 'org', 'w', (_id, text) => text).some(
      (record) => record.id === '/settings/members',
    ),
  ).toBe(false);
});

test('search results use the same module icons as the sidebar', () => {
  const administrator = { ...account, permissions: [...account.permissions, 'usage:view', 'cost:view'] };
  const records = navigationSearchRecords(administrator, 'org', 'w', (_id, text) => text);
  const pages = [...consoleNavigation, ...settingsGroups].flatMap((item) =>
    item.type === 'group' ? item.children : [item],
  );
  for (const page of pages) {
    expect(records.find((record) => record.id === page.href)?.icon).toBe(page.icon);
  }
});

test('channels follow skills in search results with the shared context and icon', () => {
  const channel = navigationSearchRecords(account, 'org', 'workspace', (_id, text) => text).find(
    (item) => item.id === '/channels',
  );
  expect(channel?.context).toBe('Resources');
  expect(channel?.icon).toBe(productIcons.channels);
});

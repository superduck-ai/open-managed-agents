import { expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { productIcons } from '../../shared/icons/product-icons';
import { consoleNavigation } from './navigation';
import { navigationHref, isActivePath } from './navigation-paths';
import {
  isSettingsPath,
  settingsItem,
  settingScope,
  settingsGroups,
  visibleSettingsGroups,
} from './settings-navigation';
import { navigationSearchRecords } from '../../features/console-search/search-data';

const account = {
  uuid: 'a',
  email_address: 'a@example.com',
  effectiveOrganizationUuid: 'org',
  permissions: ['organization:manage', 'members:manage'],
};

test('ordinary users cannot see organization management or its search results', () => {
  const member = { ...account, permissions: [] };
  expect(visibleSettingsGroups(member, 'org').map((group) => group.id)).toEqual(['access', 'activity', 'personal']);
  expect(visibleSettingsGroups(account, 'other').map((group) => group.id)).not.toContain('organization');
  expect(
    navigationSearchRecords(member, 'org', 'w', (_id, text) => text).some(
      (record) => record.id === '/settings/members',
    ),
  ).toBe(false);
});

test('workspace resources do not match the organization workspace directory', () => {
  for (const resource of ['webhooks', 'mcp-tunnels']) {
    expect(isSettingsPath(`/settings/workspaces/w/${resource}`)).toBe(false);
    expect(isSettingsPath(`/settings/workspaces/w/${resource}/resource`)).toBe(false);
  }
  expect(isSettingsPath('/settings/workspaces')).toBe(true);
  expect(isSettingsPath('/settings/workspaces/w/keys')).toBe(true);
  expect(isSettingsPath('/workspaces/w/observability')).toBe(true);
});

test('settings use the confirmed four groups and organization-level model and telemetry entries', () => {
  const groups = visibleSettingsGroups(account, 'org');
  expect(groups.map((group) => group.id)).toEqual(['access', 'activity', 'organization', 'personal']);
  expect(groups[0].children.map((item) => item.href)).toEqual(['/settings/llm', '/api-keys']);
  expect(groups[2].children.map((item) => item.href)).toEqual([
    '/settings/members',
    '/settings/workspaces',
    '/settings/organization',
  ]);
  const resources = consoleNavigation.find((item) => item.type === 'group');
  expect(resources?.type === 'group' && resources.children.map((item) => item.href)).toEqual([
    '/files',
    '/skills',
    '/memory-stores',
    '/credential-vaults',
    '/mcp-servers',
    '/mcp-tunnels',
    '/webhooks',
  ]);
});

test('scope labels reflect actual ownership', () => {
  expect(settingScope('/settings/llm')).toBe('organization');
  expect(settingScope('/settings/telemetry')).toBe('organization');
  expect(settingScope('/usage')).toBe('organization');
  expect(settingScope('/settings/profile')).toBe('personal');
  expect(settingScope('/settings/workspaces/w/keys')).toBe('workspace');
});

test('each navigation group and module has a distinct icon shape', () => {
  const entries = [...consoleNavigation, ...settingsGroups].flatMap((item) =>
    item.type === 'group' ? [item, ...item.children] : [item],
  );
  const icons = [...entries.map((item) => item.icon), productIcons.settings];
  const shapes = icons.map((Icon) => {
    expect(Icon).toBeTruthy();
    return renderToStaticMarkup(createElement(Icon!)).replace(/<svg[^>]*>|<\/svg>/g, '');
  });
  expect(new Set(shapes).size).toBe(shapes.length);
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

test('retains channel navigation after consolidating workspace paths', () => {
  expect(consoleNavigation.some((item) => item.type === 'link' && item.href === '/channels')).toBe(true);
  expect(navigationHref('/channels', 'team')).toBe('/workspaces/team/channels');
  expect(isActivePath('/workspaces/team/channels/agent/channel', '/channels')).toBe(true);
  expect(isActivePath('/workspaces/default/channels', '/channels')).toBe(true);
  expect(isActivePath('/workspaces/team/channels-other', '/channels')).toBe(false);
});

test('legacy workspace telemetry uses organization settings before redirecting', () => {
  const path = '/settings/workspaces/workspace-b/telemetry';
  expect(isSettingsPath(path)).toBe(true);
  expect(settingsItem(path)?.href).toBe('/settings/telemetry');
  expect(settingsItem(path)?.group.id).toBe('activity');
  expect(settingScope(path)).toBe('organization');
});

test.each(['/settings/billing', '/billing', '/settings/limits', '/settings/service-accounts'])(
  'legacy settings path %s keeps the settings shell',
  (path) => {
    expect(isSettingsPath(path)).toBe(true);
    expect(settingScope(path)).toBe('organization');
  },
);

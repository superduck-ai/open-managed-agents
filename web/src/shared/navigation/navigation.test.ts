import { expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { productIcons } from '../icons/product-icons';
import { consoleNavigation, sortedResourceNavigation } from './navigation';
import { navigationHref, isActivePath } from './navigation-paths';
import {
  isSettingsPath,
  settingsItem,
  settingScope,
  settingsGroups,
  visibleSettingsGroups,
} from './settings-navigation';

const account = {
  uuid: 'a',
  email_address: 'a@example.com',
  effectiveOrganizationUuid: 'org',
  permissions: ['organization:manage', 'members:manage'],
};

test('ordinary users cannot see organization management', () => {
  const member = { ...account, permissions: [] };
  expect(visibleSettingsGroups(member, 'org').map((group) => group.id)).toEqual(['access', 'activity', 'personal']);
  expect(visibleSettingsGroups(account, 'other').map((group) => group.id)).not.toContain('organization');
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
  expect(groups[0].children.map((item) => item.href)).toEqual(['/llm-models', '/api-keys']);
  expect(groups[2].children.map((item) => item.href)).toEqual([
    '/settings/members',
    '/settings/workspaces',
    '/settings/organization',
  ]);
  const resources = consoleNavigation.find((item) => item.type === 'group');
  expect(resources?.type === 'group' && resources.children.map((item) => item.href)).toEqual([
    '/files',
    '/skills',
    '/channels',
    '/memory-stores',
    '/credential-vaults',
    '/mcp-servers',
    '/mcp-tunnels',
    '/webhooks',
  ]);
});

test('scope labels reflect actual ownership', () => {
  expect(settingScope('/settings/llm')).toBe('workspace');
  expect(settingScope('/settings/llm/organization')).toBe('organization');
  expect(settingScope('/settings/workspaces/team/llm')).toBe('workspace');
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

test('retains channel navigation after consolidating workspace paths', () => {
  expect(consoleNavigation.some((item) => item.type === 'link' && item.href === '/channels')).toBe(false);
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

test.each(['en', 'zh-CN'])('channels follows skills in the %s resource navigation', (locale) => {
  const resources = consoleNavigation.find((item) => item.type === 'group');
  expect(resources?.type).toBe('group');
  if (!resources || resources.type !== 'group') throw new Error('Missing resource navigation');
  const items = sortedResourceNavigation(resources.children, locale);
  const skills = items.findIndex((item) => item.href === '/skills');
  expect(items[skills + 1].href).toBe('/channels');
  expect(items[skills + 1].icon).toBe(productIcons.channels);
});

test('both model scopes select the single workspace-addressed settings entry', () => {
  for (const path of ['/settings/workspaces/team/llm', '/settings/llm/organization', '/settings/llm']) {
    expect(settingsItem(path)?.href).toBe('/llm-models');
    expect(isActivePath(path, '/llm-models')).toBe(true);
  }
  expect(navigationHref('/llm-models', 'team')).toBe('/settings/workspaces/team/llm');
});

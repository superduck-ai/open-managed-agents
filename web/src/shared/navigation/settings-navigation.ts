import { productIcons } from '../icons/product-icons';
import type { AuthAccount } from '../auth/api';
import { canManageOrganization } from '../permissions/organization';
import { canManageMembers } from '../permissions/members';
import { canViewAnalytics } from '../permissions/analytics';
import { isActivePath } from './navigation-paths';
import type { NavGroupItem } from './navigation';

export const settingsGroups: (NavGroupItem & { id: string })[] = [
  {
    type: 'group',
    id: 'access',
    label: 'Models and access',
    labelId: 'nav.modelsAndAccess',
    icon: productIcons.modelsAndAccess,
    children: [
      { href: '/settings/llm', label: 'LLM models', labelId: 'nav.llmModels', icon: productIcons.models },
      { href: '/api-keys', label: 'API keys', labelId: 'nav.apiKeys', icon: productIcons.keys },
    ],
  },
  {
    type: 'group',
    id: 'activity',
    label: 'Usage and operations',
    labelId: 'nav.usageAndOperations',
    icon: productIcons.usageAndOperations,
    children: [
      { href: '/usage', label: 'Usage', labelId: 'nav.usage', icon: productIcons.usage },
      { href: '/cost', label: 'Cost', labelId: 'nav.cost', icon: productIcons.cost },
      {
        href: '/observability',
        label: 'Observability',
        labelId: 'nav.observability',
        icon: productIcons.observability,
      },
      {
        href: '/settings/telemetry',
        label: 'OpenTelemetry',
        labelId: 'nav.telemetry',
        icon: productIcons.telemetry,
      },
    ],
  },
  {
    type: 'group',
    id: 'organization',
    label: 'Organization management',
    labelId: 'nav.organizationManagement',
    icon: productIcons.organizationManagement,
    children: [
      { href: '/settings/members', label: 'Members', labelId: 'nav.members', icon: productIcons.members },
      { href: '/settings/workspaces', label: 'Workspaces', labelId: 'nav.workspaces', icon: productIcons.workspace },
      {
        href: '/settings/organization',
        label: 'Organization profile',
        labelId: 'nav.organizationProfile',
        icon: productIcons.organizationProfile,
      },
    ],
  },
  {
    type: 'group',
    id: 'personal',
    label: 'Personal settings',
    labelId: 'account.personalSettings',
    icon: productIcons.personalSettings,
    children: [
      { href: '/settings/profile', label: 'Profile', labelId: 'nav.profile', icon: productIcons.profile },
      {
        href: '/settings/appearance',
        label: 'Appearance and language',
        labelId: 'nav.appearanceAndLanguage',
        icon: productIcons.appearance,
      },
    ],
  },
];

export function canAccessSettings(href: string, account: AuthAccount | null | undefined, orgUuid?: string) {
  if (href === '/usage' || href === '/cost')
    return canViewAnalytics(account, orgUuid, href === '/usage' ? 'usage:view' : 'cost:view');
  if (href === '/settings/members') return canManageMembers(account, orgUuid);
  if (['/settings/organization', '/settings/workspaces'].includes(href)) return canManageOrganization(account, orgUuid);
  return true;
}

export function visibleSettingsGroups(account: AuthAccount | null | undefined, orgUuid?: string) {
  return settingsGroups
    .map((group) => ({
      ...group,
      children: group.children.filter((item) => canAccessSettings(item.href, account, orgUuid)),
    }))
    .filter((group) => group.children.length > 0);
}

export function settingsItem(path: string) {
  const canonicalPath = isActivePath(path, '/telemetry') ? '/settings/telemetry' : path;
  return settingsGroups
    .flatMap((group) => group.children.map((item) => ({ ...item, group })))
    .find((item) =>
      item.href.startsWith('/settings/') ? canonicalPath === item.href : isActivePath(canonicalPath, item.href),
    );
}

export function isSettingsPath(path: string) {
  if (/^\/settings\/workspaces\/[^/]+\/(webhooks|mcp-tunnels)(\/|$)/.test(path)) return false;
  return (
    path.startsWith('/settings/') ||
    Boolean(settingsItem(path)) ||
    ['/llm-models', '/members', '/telemetry', '/billing'].includes(path) ||
    /^\/workspaces\/[^/]+\/llm-models/.test(path)
  );
}

export function settingScope(path: string) {
  if (path === '/billing' || isActivePath(path, '/telemetry')) return 'organization';
  if (path === '/usage' || path === '/cost' || /^\/workspaces\/[^/]+\/cost/.test(path)) return 'organization';
  if (path === '/settings/profile' || path === '/settings/appearance') return 'personal';
  if (path.startsWith('/settings/') && !/^\/settings\/workspaces\/[^/]+\//.test(path)) return 'organization';
  return 'workspace';
}

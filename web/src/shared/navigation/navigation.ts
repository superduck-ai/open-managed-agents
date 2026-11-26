import { productIcons as navigationIcons } from '@/shared/icons/product-icons';
import {
  Activity,
  BarChart3,
  Bot,
  Box,
  Braces,
  Building2,
  CircleDollarSign,
  Code2,
  Copy,
  Cpu,
  Database,
  FileText,
  Gauge,
  IdCard,
  KeyRound,
  ListChecks,
  LockKeyhole,
  Logs,
  Moon,
  Network,
  Palette,
  Plug,
  Receipt,
  Settings,
  Shield,
  Tags,
  TerminalSquare,
  ToggleLeft,
  UsersRound,
  Vault,
  WalletCards,
  Webhook,
} from 'lucide-react';
import type { ComponentType } from 'react';
import type { BooleanRuntimeCapabilityKey, RuntimeCapabilities } from '../workspaces/runtime';

export type IconComponent = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;

export type NavLinkItem = {
  type: 'link';
  href: string;
  label: string;
  labelId: string;
  icon?: IconComponent;
  requiredCapability?: BooleanRuntimeCapabilityKey;
};

export type NavGroupItem = {
  type: 'group';
  label: string;
  labelId: string;
  icon: IconComponent;
  children: Array<{
    href: string;
    label: string;
    labelId: string;
    icon?: IconComponent;
    requiredCapability?: BooleanRuntimeCapabilityKey;
  }>;
};

export type NavItem = NavLinkItem | NavGroupItem;

export const consoleNavigation: NavItem[] = [
  { type: 'link', href: '/dashboard', label: 'Overview', labelId: 'nav.overview', icon: navigationIcons.overview },
  {
    type: 'link',
    href: '/quickstart',
    label: 'Quickstart',
    labelId: 'nav.quickstart',
    icon: navigationIcons.quickstart,
  },
  { type: 'link', href: '/agents', label: 'Agents', labelId: 'nav.agents', icon: navigationIcons.agents },
  { type: 'link', href: '/sessions', label: 'Sessions', labelId: 'nav.sessions', icon: navigationIcons.sessions },
  {
    type: 'link',
    href: '/deployments',
    label: 'Deployments',
    labelId: 'nav.deployments',
    icon: navigationIcons.deployments,
    requiredCapability: 'deployments',
  },
  {
    type: 'link',
    href: '/environments',
    label: 'Environments',
    labelId: 'nav.environments',
    icon: navigationIcons.environments,
  },
  {
    type: 'link',
    href: '/evaluations',
    label: 'Evaluation',
    labelId: 'nav.evaluations',
    icon: navigationIcons.evaluations,
    requiredCapability: 'evaluations',
  },
  {
    type: 'group',
    label: 'Resources',
    labelId: 'nav.resources',
    icon: navigationIcons.resources,
    children: [
      { href: '/files', label: 'Files', labelId: 'nav.files', icon: navigationIcons.files },
      {
        href: '/skills',
        label: 'Skills',
        labelId: 'nav.skills',
        icon: navigationIcons.skills,
        requiredCapability: 'skills',
      },
      {
        href: '/channels',
        label: 'Channels',
        labelId: 'nav.channels',
        icon: navigationIcons.channels,
        requiredCapability: 'channels',
      },
      {
        href: '/identities',
        label: 'Identities',
        labelId: 'nav.identities',
        icon: navigationIcons.identities,
        requiredCapability: 'channels',
      },
      {
        href: '/memory-stores',
        label: 'Memory stores',
        labelId: 'nav.memoryStores',
        icon: navigationIcons.memory,
        requiredCapability: 'memory_stores',
      },
      {
        href: '/credential-vaults',
        label: 'Credential vaults',
        labelId: 'nav.credentialVaults',
        icon: navigationIcons.vaults,
        requiredCapability: 'credential_vaults',
      },
      {
        href: '/mcp-servers',
        label: 'MCP Servers',
        labelId: 'nav.mcpServers',
        icon: navigationIcons.mcp,
        requiredCapability: 'mcp_servers',
      },
      {
        href: '/mcp-tunnels',
        label: 'MCP tunnels',
        labelId: 'nav.mcpTunnels',
        icon: navigationIcons.tunnel,
        requiredCapability: 'mcp_tunnels',
      },
      {
        href: '/webhooks',
        label: 'Webhooks',
        labelId: 'nav.webhooks',
        icon: navigationIcons.webhooks,
        requiredCapability: 'session_webhooks',
      },
    ],
  },
];

function supportsNavigationCapability(
  requiredCapability: BooleanRuntimeCapabilityKey | undefined,
  capabilities?: RuntimeCapabilities,
) {
  if (!requiredCapability || !capabilities) {
    return true;
  }
  return Boolean(capabilities[requiredCapability]);
}

export function visibleConsoleNavigation(capabilities?: RuntimeCapabilities): NavItem[] {
  return consoleNavigation.flatMap((item): NavItem[] => {
    if (item.type === 'link') {
      return supportsNavigationCapability(item.requiredCapability, capabilities) ? [item] : [];
    }
    const children = item.children.filter((child) =>
      supportsNavigationCapability(child.requiredCapability, capabilities),
    );
    return children.length > 0 ? [{ ...item, children }] : [];
  });
}

export function sortedResourceNavigation(items: NavGroupItem['children'], locale: string) {
  const order =
    locale === 'zh-CN'
      ? [
          '/files',
          '/skills',
          '/channels',
          '/identities',
          '/memory-stores',
          '/mcp-tunnels',
          '/webhooks',
          '/credential-vaults',
          '/mcp-servers',
        ]
      : [
          '/files',
          '/skills',
          '/channels',
          '/identities',
          '/webhooks',
          '/mcp-tunnels',
          '/mcp-servers',
          '/memory-stores',
          '/credential-vaults',
        ];
  return [...items].sort((left, right) => order.indexOf(left.href) - order.indexOf(right.href));
}

export const placeholderIcons = {
  '/llm-models': Cpu,
  '/api-keys': KeyRound,
  '/workbench': Code2,
  '/playground': Braces,
  '/files': FileText,
  '/skills': ToggleLeft,
  '/mcp-servers': Plug,
  '/batches': Receipt,
  '/quickstart': Bot,
  '/agents': Bot,
  '/sessions': Activity,
  '/evaluations': ListChecks,
  '/deployments': Network,
  '/environments': Box,
  '/channels': navigationIcons.channels,
  '/identities': IdCard,
  '/credential-vaults': Vault,
  '/memory-stores': Database,
  '/dreams': Moon,
  '/usage': BarChart3,
  '/usage/cache': Gauge,
  '/usage/limits': Gauge,
  '/cost': CircleDollarSign,
  '/logs': Logs,
  '/claude-code/usage': TerminalSquare,
  '/claude-code/settings': Settings,
  '/limits': Gauge,
  '/members': UsersRound,
  '/service-accounts': LockKeyhole,
  '/privacy-controls': Shield,
  '/security': Shield,
  '/webhooks': Webhook,
  '/telemetry': Activity,
  '/mcp-tunnels': Plug,
  '/tags': Tags,
  '/billing': WalletCards,
  '/settings/appearance': Palette,
  '/settings/organization': Building2,
  copy: Copy,
} as const;

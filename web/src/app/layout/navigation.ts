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
  FlaskConical,
  Gauge,
  KeyRound,
  LayoutDashboard,
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

export type IconComponent = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;

export type NavLinkItem = {
  type: 'link';
  href: string;
  label: string;
  labelId: string;
  icon?: IconComponent;
  badge?: string;
  badgeId?: string;
};

export type NavGroupItem = {
  type: 'group';
  label: string;
  labelId: string;
  icon: IconComponent;
  hidden?: boolean;
  children: Array<{
    href: string;
    label: string;
    labelId: string;
    badge?: string;
    badgeId?: string;
    hidden?: boolean;
  }>;
};

export type NavItem = NavLinkItem | NavGroupItem;

export const consoleNavigation: NavItem[] = [
  { type: 'link', href: '/dashboard', label: 'Dashboard', labelId: 'nav.dashboard', icon: LayoutDashboard },
  { type: 'link', href: '/llm-models', label: 'LLM models', labelId: 'nav.llmModels', icon: Cpu },
  { type: 'link', href: '/api-keys', label: 'API keys', labelId: 'nav.apiKeys', icon: KeyRound },
  {
    type: 'group',
    label: 'Build',
    labelId: 'nav.build',
    icon: Braces,
    children: [
      // Temporarily hidden from the sidebar; this entry stays so the route can be restored.
      { href: '/workbench', label: 'Workbench', labelId: 'nav.workbench', hidden: true },
      { href: '/files', label: 'Files', labelId: 'nav.files' },
      { href: '/skills', label: 'Skills', labelId: 'nav.skills' },
      { href: '/mcp-servers', label: 'MCP Servers', labelId: 'nav.mcpServers' },
      { href: '/batches', label: 'Batches', labelId: 'nav.batches', hidden: true },
    ],
  },
  {
    type: 'group',
    label: 'Managed Agents',
    labelId: 'nav.managedAgents',
    icon: Network,
    children: [
      { href: '/quickstart', label: 'Quickstart', labelId: 'nav.quickstart' },
      { href: '/agents', label: 'Agents', labelId: 'nav.agents' },
      { href: '/sessions', label: 'Sessions', labelId: 'nav.sessions' },
      { href: '/evaluations', label: 'Evaluation', labelId: 'nav.evaluations' },
      { href: '/observability', label: 'Observability', labelId: 'nav.observability' },
      { href: '/deployments', label: 'Deployments', labelId: 'nav.deployments' },
      { href: '/environments', label: 'Environments', labelId: 'nav.environments' },
      { href: '/credential-vaults', label: 'Credential vaults', labelId: 'nav.credentialVaults' },
      { href: '/memory-stores', label: 'Memory stores', labelId: 'nav.memoryStores' },
    ],
  },
  {
    type: 'group',
    label: 'Analytics',
    labelId: 'nav.analytics',
    icon: BarChart3,
    children: [
      { href: '/usage', label: 'Usage', labelId: 'nav.usage' },
      { href: '/usage/cache', label: 'Caching', labelId: 'nav.caching' },
      { href: '/usage/limits', label: 'Rate limits', labelId: 'nav.rateLimits' },
      { href: '/cost', label: 'Cost', labelId: 'nav.cost' },
      { href: '/logs', label: 'Logs', labelId: 'nav.logs' },
    ],
  },
  {
    // Temporarily hidden from the sidebar; the group definition stays so it can be restored.
    type: 'group',
    label: 'Claude Code',
    labelId: 'nav.claudeCode',
    icon: TerminalSquare,
    hidden: true,
    children: [
      { href: '/claude-code/usage', label: 'Usage', labelId: 'nav.usage' },
      { href: '/claude-code/settings', label: 'Settings', labelId: 'nav.settings' },
    ],
  },
  {
    type: 'group',
    label: 'Workspace settings',
    labelId: 'nav.workspaceSettings',
    icon: Settings,
    children: [
      { href: '/members', label: 'Members', labelId: 'nav.members' },
      { href: '/api-keys', label: 'API keys', labelId: 'nav.apiKeys' },
      { href: '/webhooks', label: 'Webhooks', labelId: 'nav.webhooks' },
      {
        href: '/mcp-tunnels',
        label: 'MCP tunnels',
        labelId: 'nav.mcpTunnels',
        badge: 'Preview',
        badgeId: 'nav.preview',
      },
    ],
  },
];

export function visibleConsoleNavigation(items: readonly NavItem[] = consoleNavigation): NavItem[] {
  return items.flatMap<NavItem>((item) => {
    if (item.type === 'link') {
      return [item];
    }
    if (item.hidden) {
      return [];
    }
    return [{ ...item, children: item.children.filter((child) => !child.hidden) }];
  });
}

export const settingsNavigation = [
  { href: '/settings/organization', label: 'Organization', labelId: 'nav.organization' },
  { href: '/settings/llm', label: 'LLM configuration', labelId: 'nav.llmSettings' },
  { href: '/settings/members', label: 'Members', labelId: 'nav.members' },
  { href: '/settings/workspaces', label: 'Workspaces', labelId: 'nav.workspaces' },
] as const;

export const personalSettingsNavigation = [
  { href: '/settings/profile', label: 'Profile', labelId: 'nav.profile' },
  { href: '/settings/appearance', label: 'Appearance', labelId: 'nav.appearance' },
] as const;

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
  '/evaluations/evaluators': Tags,
  '/evaluations/experiments': FlaskConical,
  '/deployments': Network,
  '/environments': Box,
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
  '/mcp-tunnels': Plug,
  '/tags': Tags,
  '/billing': WalletCards,
  '/settings/appearance': Palette,
  '/settings/organization': Building2,
  copy: Copy,
} as const;

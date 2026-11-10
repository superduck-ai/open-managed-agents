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

export type IconComponent = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;

export type NavLinkItem = {
  type: 'link';
  href: string;
  label: string;
  labelId: string;
  icon?: IconComponent;
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
    hidden?: boolean;
    section?: string;
    icon?: IconComponent;
  }>;
};

export type NavItem = NavLinkItem | NavGroupItem;

export const workspaceManagementSections = [
  { id: 'access', label: 'Models and access', labelId: 'nav.modelsAndAccess' },
  { id: 'activity', label: 'Usage and operations', labelId: 'nav.usageAndOperations' },
  { id: 'integrations', label: 'Integrations', labelId: 'nav.integrations' },
] as const;

export const workspaceManagementNavigation: NavGroupItem = {
  type: 'group',
  label: 'Workspace management',
  labelId: 'nav.workspaceManagement',
  icon: navigationIcons.settings,
  children: [
    { href: '/members', section: 'access', label: 'Members', labelId: 'nav.members', icon: navigationIcons.members },
    {
      href: '/llm-models',
      section: 'access',
      label: 'LLM models',
      labelId: 'nav.llmModels',
      icon: navigationIcons.models,
    },
    { href: '/api-keys', section: 'access', label: 'API keys', labelId: 'nav.apiKeys', icon: navigationIcons.keys },
    { href: '/usage', section: 'activity', label: 'Usage', labelId: 'nav.usage', icon: navigationIcons.usage },
    { href: '/cost', section: 'activity', label: 'Cost', labelId: 'nav.cost', icon: navigationIcons.cost },
    {
      href: '/observability',
      section: 'activity',
      label: 'Observability',
      labelId: 'nav.observability',
      icon: navigationIcons.observability,
    },
    {
      href: '/usage/cache',
      hidden: true,
      section: 'activity',
      label: 'Caching',
      labelId: 'nav.caching',
      icon: navigationIcons.memory,
    },
    {
      href: '/usage/limits',
      hidden: true,
      section: 'activity',
      label: 'Rate limits',
      labelId: 'nav.rateLimits',
      icon: navigationIcons.settings,
    },
    {
      href: '/logs',
      hidden: true,
      section: 'activity',
      label: 'Logs',
      labelId: 'nav.logs',
      icon: navigationIcons.files,
    },
    {
      href: '/mcp-tunnels',
      section: 'integrations',
      label: 'MCP tunnels',
      labelId: 'nav.mcpTunnels',
      icon: navigationIcons.tunnel,
    },
    {
      href: '/webhooks',
      section: 'integrations',
      label: 'Webhooks',
      labelId: 'nav.webhooks',
      icon: navigationIcons.webhooks,
    },
    {
      href: '/telemetry',
      section: 'integrations',
      label: 'OpenTelemetry',
      labelId: 'nav.telemetry',
      icon: navigationIcons.observability,
    },
  ],
};

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
    href: '/evaluations',
    label: 'Evaluation',
    labelId: 'nav.evaluations',
    icon: navigationIcons.evaluations,
  },
  {
    type: 'link',
    href: '/deployments',
    label: 'Deployments',
    labelId: 'nav.deployments',
    icon: navigationIcons.deployments,
  },
  {
    type: 'link',
    href: '/environments',
    label: 'Environments',
    labelId: 'nav.environments',
    icon: navigationIcons.environments,
  },
  {
    type: 'group',
    label: 'Resources',
    labelId: 'nav.resources',
    icon: navigationIcons.resources,
    children: [
      { href: '/skills', label: 'Skills', labelId: 'nav.skills', icon: navigationIcons.skills },
      { href: '/files', label: 'Files', labelId: 'nav.files', icon: navigationIcons.files },
      { href: '/memory-stores', label: 'Memory stores', labelId: 'nav.memoryStores', icon: navigationIcons.memory },
      {
        href: '/credential-vaults',
        label: 'Credential vaults',
        labelId: 'nav.credentialVaults',
        icon: navigationIcons.vaults,
      },
      { href: '/mcp-servers', label: 'MCP Servers', labelId: 'nav.mcpServers', icon: navigationIcons.mcp },
    ],
  },
];

export const settingsNavigation = [
  { href: '/settings/organization', label: 'Organization', labelId: 'nav.organization' },
  { href: '/settings/llm', label: 'LLM configuration', labelId: 'nav.llmSettings' },
  { href: '/settings/members', label: 'Members', labelId: 'nav.members' },
  { href: '/settings/workspaces', label: 'Workspaces', labelId: 'nav.workspaces' },
  { href: '/settings/telemetry', label: 'OpenTelemetry', labelId: 'nav.telemetry' },
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
  '/telemetry': Activity,
  '/mcp-tunnels': Plug,
  '/tags': Tags,
  '/billing': WalletCards,
  '/settings/appearance': Palette,
  '/settings/organization': Building2,
  copy: Copy,
} as const;

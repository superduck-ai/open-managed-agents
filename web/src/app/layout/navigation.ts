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
  Database,
  FileText,
  Gauge,
  KeyRound,
  LayoutDashboard,
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
} from "lucide-react";
import type { ComponentType } from "react";

export type IconComponent = ComponentType<{ className?: string; "aria-hidden"?: boolean }>;

export type NavLinkItem = {
  type: "link";
  href: string;
  label: string;
  labelId: string;
  icon?: IconComponent;
  badge?: string;
  badgeId?: string;
};

export type NavGroupItem = {
  type: "group";
  label: string;
  labelId: string;
  icon: IconComponent;
  children: Array<{
    href: string;
    label: string;
    labelId: string;
    badge?: string;
    badgeId?: string;
  }>;
};

export type NavItem = NavLinkItem | NavGroupItem;

export const consoleNavigation: NavItem[] = [
  { type: "link", href: "/dashboard", label: "Dashboard", labelId: "nav.dashboard", icon: LayoutDashboard },
  { type: "link", href: "/api-keys", label: "API keys", labelId: "nav.apiKeys", icon: KeyRound },
  {
    type: "group",
    label: "Build",
    labelId: "nav.build",
    icon: Braces,
    children: [
      { href: "/workbench", label: "Workbench", labelId: "nav.workbench" },
      { href: "/files", label: "Files", labelId: "nav.files" },
      { href: "/skills", label: "Skills", labelId: "nav.skills" },
      { href: "/batches", label: "Batches", labelId: "nav.batches" },
    ],
  },
  {
    type: "group",
    label: "Managed Agents",
    labelId: "nav.managedAgents",
    icon: Network,
    children: [
      { href: "/quickstart", label: "Quickstart", labelId: "nav.quickstart" },
      { href: "/agents", label: "Agents", labelId: "nav.agents" },
      { href: "/sessions", label: "Sessions", labelId: "nav.sessions" },
      { href: "/deployments", label: "Deployments", labelId: "nav.deployments", badge: "New", badgeId: "nav.new" },
      { href: "/environments", label: "Environments", labelId: "nav.environments" },
      { href: "/credential-vaults", label: "Credential vaults", labelId: "nav.credentialVaults" },
      { href: "/memory-stores", label: "Memory stores", labelId: "nav.memoryStores" },
    ],
  },
  {
    type: "group",
    label: "Analytics",
    labelId: "nav.analytics",
    icon: BarChart3,
    children: [
      { href: "/usage", label: "Usage", labelId: "nav.usage" },
      { href: "/usage/cache", label: "Caching", labelId: "nav.caching" },
      { href: "/usage/limits", label: "Rate limits", labelId: "nav.rateLimits" },
      { href: "/cost", label: "Cost", labelId: "nav.cost" },
      { href: "/logs", label: "Logs", labelId: "nav.logs" },
    ],
  },
  {
    type: "group",
    label: "Claude Code",
    labelId: "nav.claudeCode",
    icon: TerminalSquare,
    children: [
      { href: "/claude-code/usage", label: "Usage", labelId: "nav.usage" },
      { href: "/claude-code/settings", label: "Settings", labelId: "nav.settings" },
    ],
  },
  {
    type: "group",
    label: "Manage",
    labelId: "nav.manage",
    icon: Settings,
    children: [
      { href: "/limits", label: "Limits", labelId: "nav.limits" },
      { href: "/members", label: "Members", labelId: "nav.members" },
      { href: "/service-accounts", label: "Service accounts", labelId: "nav.serviceAccounts" },
      { href: "/privacy-controls", label: "Privacy controls", labelId: "nav.privacyControls" },
      { href: "/security", label: "Security", labelId: "nav.security" },
      { href: "/webhooks", label: "Webhooks", labelId: "nav.webhooks" },
    ],
  },
];

export const settingsNavigation = [
  { href: "/settings/profile", label: "Profile", labelId: "nav.profile" },
  { href: "/settings/appearance", label: "Appearance", labelId: "nav.appearance" },
  { href: "/settings/organization", label: "Organization", labelId: "nav.organization" },
  { href: "/settings/members", label: "Members", labelId: "nav.members" },
  { href: "/settings/workspaces", label: "Workspaces", labelId: "nav.workspaces" },
  { href: "/settings/billing", label: "Billing", labelId: "nav.billing" },
  { href: "/settings/limits", label: "Limits", labelId: "nav.limits" },
  { href: "/settings/api-keys", label: "API keys", labelId: "nav.apiKeys" },
  { href: "/settings/admin-keys", label: "Admin keys", labelId: "nav.adminKeys" },
  { href: "/settings/service-accounts", label: "Service accounts", labelId: "nav.serviceAccounts" },
  { href: "/settings/workload-identity", label: "Workload identity", labelId: "nav.workloadIdentity" },
  { href: "/settings/privacy-controls", label: "Privacy controls", labelId: "nav.privacyControls" },
  { href: "/settings/identity-and-access", label: "Identity and access", labelId: "nav.identityAndAccess" },
] as const;

export const placeholderIcons = {
  "/api-keys": KeyRound,
  "/workbench": Code2,
  "/playground": Braces,
  "/files": FileText,
  "/skills": ToggleLeft,
  "/batches": Receipt,
  "/quickstart": Bot,
  "/agents": Bot,
  "/sessions": Activity,
  "/deployments": Network,
  "/environments": Box,
  "/credential-vaults": Vault,
  "/memory-stores": Database,
  "/dreams": Moon,
  "/usage": BarChart3,
  "/usage/cache": Gauge,
  "/usage/limits": Gauge,
  "/cost": CircleDollarSign,
  "/logs": Logs,
  "/claude-code/usage": TerminalSquare,
  "/claude-code/settings": Settings,
  "/limits": Gauge,
  "/members": UsersRound,
  "/service-accounts": LockKeyhole,
  "/privacy-controls": Shield,
  "/security": Shield,
  "/webhooks": Webhook,
  "/mcp-tunnels": Plug,
  "/tags": Tags,
  "/billing": WalletCards,
  "/settings/appearance": Palette,
  "/settings/organization": Building2,
  copy: Copy,
} as const;

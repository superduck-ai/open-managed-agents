import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router';
import { ConsoleLayout } from './layout/ConsoleLayout';
import { ProtectedConsoleLayout } from './layout/ProtectedConsoleLayout';
import { DashboardPage } from '../features/dashboard/DashboardPage';
import { CachingPage, CostPage, LogsPage, RateLimitsPage, UsagePage } from '../features/analytics/AnalyticsPages';
import { LoginPage } from '../features/auth/LoginPage';
import { ManagedAgentsPage } from '../features/managed-agents/ManagedAgentsPage';
import { OrganizationSettingsPage } from '../features/settings/OrganizationSettingsPage';
import { WorkspaceApiKeysPage } from '../features/settings/WorkspaceApiKeysPage';
import { WorkspaceWebhooksPage } from '../features/settings/WorkspaceWebhooksPage';
import { WorkbenchPage } from '../features/workbench/WorkbenchPage';
import { normalizeReturnTo } from '../shared/auth/redirects';

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'login',
  validateSearch: (search: Record<string, unknown>) => ({
    returnTo: normalizeReturnTo(typeof search.returnTo === 'string' ? search.returnTo : undefined),
  }),
  component: LoginPage,
});

const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'protected',
  component: ProtectedConsoleLayout,
});

const consoleRoute = createRoute({
  getParentRoute: () => protectedRoute,
  id: 'console',
  component: ConsoleLayout,
});

const dashboardRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: '/',
  component: DashboardPage,
});

const dashboardAliasRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'dashboard',
  component: DashboardPage,
});

const workbenchRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workbench',
  component: WorkbenchPage,
});

const workbenchPromptRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workbench/$promptId',
  component: WorkbenchPage,
});

const playgroundRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'playground',
  component: () => <DashboardPage section="playground" />,
});

const workspacePlaygroundRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/playground',
  component: () => <DashboardPage section="playground" />,
});

const filesRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'files',
  component: () => <DashboardPage section="files" />,
});

const workspaceFilesRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/files',
  component: () => <DashboardPage section="files" />,
});

const skillsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'skills',
  component: () => <DashboardPage section="skills" />,
});

const skillNewRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'skills/new',
  component: () => <DashboardPage section="skill-new" />,
});

const skillDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'skills/$skillId',
  component: () => <DashboardPage section="skill-detail" />,
});

const workspaceSkillsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/skills',
  component: () => <DashboardPage section="skills" />,
});

const workspaceSkillNewRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/skills/new',
  component: () => <DashboardPage section="skill-new" />,
});

const workspaceSkillDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/skills/$skillId',
  component: () => <DashboardPage section="skill-detail" />,
});

const batchesRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'batches',
  component: () => <DashboardPage section="batches" />,
});

const workspaceBatchesRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/batches',
  component: () => <DashboardPage section="batches" />,
});

const apiKeysRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'api-keys',
  component: () => <DashboardPage section="api-keys" />,
});

const quickstartRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'quickstart',
  component: () => <ManagedAgentsPage section="quickstart" />,
});

const workspaceAgentQuickstartRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/agent-quickstart',
  component: () => <ManagedAgentsPage section="quickstart" />,
});

const sessionsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'sessions',
  component: () => <ManagedAgentsPage section="sessions" />,
});

const workspaceSessionsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/sessions',
  component: () => <ManagedAgentsPage section="sessions" />,
});

const sessionDetailSearch = (search: Record<string, unknown>) => ({
  segment: search.segment === 'debug' ? 'debug' : undefined,
  event: typeof search.event === 'string' && search.event.trim() ? search.event.trim() : undefined,
});

const workspaceSessionDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/sessions/$sessionId',
  validateSearch: sessionDetailSearch,
  component: () => <ManagedAgentsPage section="sessions" />,
});

const deploymentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'deployments',
  component: () => <ManagedAgentsPage section="deployments" />,
});

const workspaceDeploymentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/deployments',
  component: () => <ManagedAgentsPage section="deployments" />,
});

const workspaceDeploymentDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/deployments/$deploymentId',
  component: () => <ManagedAgentsPage section="deployments" />,
});

const environmentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'environments',
  component: () => <ManagedAgentsPage section="environments" />,
});

const workspaceEnvironmentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/environments',
  component: () => <ManagedAgentsPage section="environments" />,
});

const workspaceEnvironmentDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/environments/$environmentId',
  component: () => <ManagedAgentsPage section="environments" />,
});

const credentialVaultsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'credential-vaults',
  component: () => <ManagedAgentsPage section="credential-vaults" />,
});

const workspaceCredentialVaultsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/vaults',
  component: () => <ManagedAgentsPage section="credential-vaults" />,
});

const workspaceCredentialVaultDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/vaults/$vaultId',
  component: () => <ManagedAgentsPage section="credential-vaults" />,
});

const memoryStoresRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'memory-stores',
  component: () => <ManagedAgentsPage section="memory-stores" />,
});

const workspaceMemoryStoresRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/memory-stores',
  component: () => <ManagedAgentsPage section="memory-stores" />,
});

const workspaceMemoryStoreDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/memory-stores/$memoryStoreId',
  component: () => <ManagedAgentsPage section="memory-stores" />,
});

const dreamsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'dreams',
  component: () => <ManagedAgentsPage section="dreams" />,
});

const workspaceDreamsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/dreams',
  component: () => <ManagedAgentsPage section="dreams" />,
});

const usageRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'usage',
  component: UsagePage,
});

const usageCacheRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'usage/cache',
  component: CachingPage,
});

const usageLimitsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'usage/limits',
  component: RateLimitsPage,
});

const cachingRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'caching',
  component: CachingPage,
});

const rateLimitsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'rate-limits',
  component: RateLimitsPage,
});

const costRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'cost',
  component: CostPage,
});

const workspaceCostRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/cost',
  component: CostPage,
});

const logsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'logs',
  component: LogsPage,
});

const workspaceLogsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/logs',
  component: LogsPage,
});

const claudeCodeUsageRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'claude-code/usage',
  component: () => <DashboardPage section="claude-code-usage" />,
});

const claudeCodeSettingsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'claude-code/settings',
  component: () => <DashboardPage section="claude-code-settings" />,
});

const limitsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'limits',
  component: () => <DashboardPage section="limits" />,
});

const membersRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'members',
  component: () => <DashboardPage section="members" />,
});

const billingRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'billing',
  component: () => <DashboardPage section="billing" />,
});

const agentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'agents',
  component: () => <ManagedAgentsPage section="agents" />,
});

const agentDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'agents/$agentId',
  component: () => <ManagedAgentsPage section="agents" />,
});

const workspaceAgentsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/agents',
  component: () => <ManagedAgentsPage section="agents" />,
});

const workspaceAgentDetailRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'workspaces/$workspaceId/agents/$agentId',
  component: () => <ManagedAgentsPage section="agents" />,
});

const serviceAccountsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'service-accounts',
  component: () => <DashboardPage section="service-accounts" />,
});

const privacyControlsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'privacy-controls',
  component: () => <DashboardPage section="privacy-controls" />,
});

const securityRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'security',
  component: () => <DashboardPage section="security" />,
});

const webhooksRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'webhooks',
  component: () => <DashboardPage section="webhooks" />,
});

const mcpTunnelsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'mcp-tunnels',
  component: () => <DashboardPage section="mcp-tunnels" />,
});

const tagsRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'tags',
  component: () => <DashboardPage section="tags" />,
});

const settingsWorkspaceKeysRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'settings/workspaces/$workspaceId/keys',
  component: WorkspaceApiKeysPage,
});

const settingsWorkspaceWebhooksRoute = createRoute({
  getParentRoute: () => consoleRoute,
  path: 'settings/workspaces/$workspaceId/webhooks',
  component: WorkspaceWebhooksPage,
});

const settingsOrganizationRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: 'settings/organization',
  component: OrganizationSettingsPage,
});

const settingsLimitsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: 'settings/limits',
  component: () => <OrganizationSettingsPage section="limits" />,
});

const settingsMembersRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: 'settings/members',
  component: () => <OrganizationSettingsPage section="members" />,
});

const settingsServiceAccountsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: 'settings/service-accounts',
  component: () => <OrganizationSettingsPage section="service-accounts" />,
});

const settingsFallbackRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: 'settings/$setting',
  component: () => <OrganizationSettingsPage />,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  protectedRoute.addChildren([
    consoleRoute.addChildren([
      dashboardRoute,
      dashboardAliasRoute,
      workbenchRoute,
      workbenchPromptRoute,
      playgroundRoute,
      workspacePlaygroundRoute,
      filesRoute,
      workspaceFilesRoute,
      skillsRoute,
      skillNewRoute,
      skillDetailRoute,
      workspaceSkillsRoute,
      workspaceSkillNewRoute,
      workspaceSkillDetailRoute,
      batchesRoute,
      workspaceBatchesRoute,
      apiKeysRoute,
      quickstartRoute,
      workspaceAgentQuickstartRoute,
      agentsRoute,
      agentDetailRoute,
      workspaceAgentsRoute,
      workspaceAgentDetailRoute,
      sessionsRoute,
      workspaceSessionsRoute,
      workspaceSessionDetailRoute,
      deploymentsRoute,
      workspaceDeploymentsRoute,
      workspaceDeploymentDetailRoute,
      environmentsRoute,
      workspaceEnvironmentsRoute,
      workspaceEnvironmentDetailRoute,
      credentialVaultsRoute,
      workspaceCredentialVaultsRoute,
      workspaceCredentialVaultDetailRoute,
      memoryStoresRoute,
      workspaceMemoryStoresRoute,
      workspaceMemoryStoreDetailRoute,
      dreamsRoute,
      workspaceDreamsRoute,
      usageRoute,
      usageCacheRoute,
      usageLimitsRoute,
      cachingRoute,
      rateLimitsRoute,
      costRoute,
      workspaceCostRoute,
      logsRoute,
      workspaceLogsRoute,
      claudeCodeUsageRoute,
      claudeCodeSettingsRoute,
      limitsRoute,
      membersRoute,
      billingRoute,
      serviceAccountsRoute,
      privacyControlsRoute,
      securityRoute,
      webhooksRoute,
      mcpTunnelsRoute,
      tagsRoute,
      settingsWorkspaceKeysRoute,
      settingsWorkspaceWebhooksRoute,
    ]),
    settingsOrganizationRoute,
    settingsLimitsRoute,
    settingsMembersRoute,
    settingsServiceAccountsRoute,
    settingsFallbackRoute,
  ]),
]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

import { useI18n } from '../../../shared/i18n';
import { Bot, BriefcaseBusiness, Cloud, Database, LockKeyhole, MessageCircle } from 'lucide-react';
import { type ReactNode } from 'react';
import { AgentDetailPage, AgentsResourcePage } from '../agents/AgentsResourcePage';
import { CompactChip, StatusPill } from '../components/common';
import { SessionDetailPage } from '../sessions/SessionDetailPage';
import { type ManagedAgentSection, type ManagedEntitySection, type ResourceConfig } from '../types';
import { currentPathname, managedAgentIdFromPath, managedEntityIdFromPath } from '../utils';
import { ManagedEntityDetailPage } from './detail';
import { ManagedEntitiesPage } from './entities';

export * from './detail';
export * from './dialogs';
export * from './entities';
export * from './model';

export const deploymentRows: Array<Record<string, ReactNode>> = [
  {
    ID: 'dep_7be...RBZ1oL',
    Name: 'deployment-sandbox-1781760043...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_FKb8Gkiy3...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_zcT...rRpT1v',
    Name: 'go-sdk-manual-run-deployment-1...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_PIXRdYnbh...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_yeM...swefHi',
    Name: 'Updated order triage',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_3Ss9giOjp...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_9Bw...19F73P',
    Name: 'deployment-sandbox-1781759888...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_UiozOnkBS...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_u2f...wxMWqs',
    Name: 'go-sdk-manual-run-deployment-1...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_TqIatVd2cE...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
];

export const agentRows: Array<Record<string, ReactNode>> = [
  {
    ID: 'agent_pyFfN...yKtN6c',
    Name: 'Structured extractor',
    Model: 'claude-sonnet-4-6',
    Status: <StatusPill>Active</StatusPill>,
    Created: '7 minutes ago',
    'Last updated': '7 minutes ago',
  },
  {
    ID: 'agent_p5M3v...1Mcu0R',
    Name: 'agent_d7f1f3b8e6a6_1',
    Model: 'claude-sonnet-4-6',
    Status: <StatusPill>Active</StatusPill>,
    Created: '15 minutes ago',
    'Last updated': '15 minutes ago',
  },
  {
    ID: 'agent_jR13P...BHjtj8',
    Name: 'agent_5e2f4a9c0b12_2',
    Model: 'claude-sonnet-4-6',
    Status: <StatusPill>Active</StatusPill>,
    Created: '34 minutes ago',
    'Last updated': '33 minutes ago',
  },
];

export const resourceConfigs: Record<Exclude<ManagedAgentSection, 'quickstart' | 'dreams'>, ResourceConfig> = {
  agents: {
    section: 'agents',
    title: 'Agents',
    description: 'Create and manage autonomous agents.',
    createLabel: 'Create agent',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Created  All time', 'Status  Active'],
    columns: ['ID', 'Name', 'Model', 'Status', 'Created', 'Last updated'],
    emptyTitle: 'No agents yet',
    emptyAction: 'Get started with agents',
    emptyIcon: Bot,
    rows: agentRows,
  },
  sessions: {
    section: 'sessions',
    title: 'Sessions',
    description: 'Trace and debug Claude Managed Agents sessions.',
    createLabel: 'Create session',
    searchPrefix: 'ID',
    searchPlaceholder: 'Search by session ID',
    filters: ['Created  All time', 'Agent  All', 'Deployment  All', 'Status  Active'],
    columns: ['', 'ID', 'Name', 'Status', 'Agent', 'Created'],
    emptyTitle: 'No sessions yet',
    emptyBody: 'Sessions will appear here once created through the API.',
    emptyIcon: MessageCircle,
  },
  deployments: {
    section: 'deployments',
    title: 'Deployments',
    description: 'A deployment binds an agent to credentials, an environment, and a schedule so it can run on its own.',
    createLabel: 'Create deployment',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Agent  All', 'Status  All'],
    columns: ['ID', 'Name', 'Status', 'Agent', 'Trigger', 'Created'],
    emptyTitle: 'No deployments yet',
    emptyBody: 'Deployments will appear after an agent is deployed.',
    emptyIcon: BriefcaseBusiness,
    rows: deploymentRows,
  },
  environments: {
    section: 'environments',
    title: 'Environments',
    description: 'Configuration template for containers, such as sessions or code execution.',
    createLabel: 'Create environment',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Status  All'],
    columns: ['ID', 'Name', 'Status', 'Type', 'Updated at'],
    emptyTitle: 'No environments yet',
    emptyBody: 'Create your first environment to get started.',
    emptyIcon: Cloud,
  },
  'credential-vaults': {
    section: 'credential-vaults',
    title: 'Credential vaults',
    description: 'Manage credential vaults that provide your agents with access to MCP servers and other tools.',
    createLabel: 'Create vault',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Status  All'],
    columns: ['ID', 'Name', 'Status', 'Created'],
    emptyTitle: 'No vaults yet',
    emptyBody: 'Create your first vault to get started.',
    emptyIcon: LockKeyhole,
  },
  'memory-stores': {
    section: 'memory-stores',
    title: 'Memory stores',
    description: 'Browse and manage persistent memory for your agents.',
    createLabel: 'Create memory store',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Created  All time', 'Status  Active'],
    columns: ['', 'ID', 'Name', 'Status', 'Created'],
    emptyTitle: 'No memory stores yet',
    emptyBody: 'Memory stores give agents persistent, cross-session memory.',
    emptyIcon: Database,
  },
};

export function ManagedResourcePage({
  config,
  routeWorkspaceId,
}: {
  config: ResourceConfig;
  routeWorkspaceId?: string;
}) {
  if (config.section !== 'agents') {
    const entityConfig = config as ResourceConfig & { section: ManagedEntitySection };
    const detailId = managedEntityIdFromPath(entityConfig.section);
    if (detailId) {
      if (entityConfig.section === 'sessions') {
        return <SessionDetailPage config={entityConfig} sessionId={detailId} />;
      }
      return <ManagedEntityDetailPage config={entityConfig} entityId={detailId} />;
    }
    return <ManagedEntitiesPage config={entityConfig} />;
  }

  const agentId = managedAgentIdFromPath(currentPathname());
  if (agentId) {
    return <AgentDetailPage agentId={agentId} routeWorkspaceId={routeWorkspaceId} />;
  }

  return <AgentsResourcePage config={config} routeWorkspaceId={routeWorkspaceId} />;
}

export function DreamingPage() {
  const { msg } = useI18n();
  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      <h1 className="text-[32px] font-semibold leading-tight text-foreground">
        {msg('managedAgents.dreams.title', 'Dreaming')}
      </h1>
      <p className="mt-3 text-[15px] text-foreground">
        {msg('managedAgents.dreams.description', 'Review recent sessions to verify memory and surface new learnings.')}
      </p>
      <div className="mt-9 rounded-lg border border-border bg-popover px-6 py-6 text-[15px] text-foreground">
        {msg('managedAgents.dreams.loading', 'Captured Dreaming assets are loading.')}
      </div>
    </section>
  );
}

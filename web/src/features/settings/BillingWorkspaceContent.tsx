import type { ReactNode } from 'react';
import { FolderOpen } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';

const resourceTitles: Record<string, [string, string]> = {
  agents: ['managedAgents.agents.title', 'Agents'],
  files: ['files.title', 'Files'],
  skills: ['skills.title', 'Skills'],
  sessions: ['managedAgents.sessions.title', 'Sessions'],
  environments: ['managedAgents.environments.title', 'Environments'],
  vaults: ['managedAgents.credentialVaults.title', 'Credential vaults'],
  'credential-vaults': ['managedAgents.credentialVaults.title', 'Credential vaults'],
  'memory-stores': ['managedAgents.memoryStores.title', 'Memory stores'],
};

export function BillingWorkspaceContent({ currentPath, children }: { currentPath: string; children: ReactNode }) {
  const { activeWorkspace, workspaces, isLoading } = useWorkspace();
  const { msg } = useI18n();
  const resource = currentPath.replace(/^\/workspaces\/[^/]+/, '').split('/')[1];
  const title = resourceTitles[resource];
  const routeWorkspaceId = workspaceIdFromPath(currentPath);
  const workspace = routeWorkspaceId
    ? workspaces.find((entry) => entry.id === routeWorkspaceId || entry.external_id === routeWorkspaceId)
    : activeWorkspace;

  if (!title) return children;
  if (isLoading) return <p>{msg('common.loading', 'Loading...')}</p>;
  if (workspace?.effective_role !== 'workspace_billing') return children;

  return (
    <section className="space-y-6">
      <h1 className="text-[28px] font-semibold leading-tight">{msg(title[0], title[1])}</h1>
      <Empty className="min-h-[320px]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FolderOpen aria-hidden />
          </EmptyMedia>
          <EmptyTitle>{msg('workspaces.resources.empty', 'No resources to display.')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    </section>
  );
}

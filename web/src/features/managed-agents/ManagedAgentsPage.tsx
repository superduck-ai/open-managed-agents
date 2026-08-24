import { useWorkspace } from '../../shared/workspaces/context';
import { useEffect } from 'react';
import { AgentQuickstartPage } from './quickstart/AgentQuickstartPage';
import { DreamingPage, ManagedResourcePage, resourceConfigs } from './resources/ManagedResources';
import { type ManagedAgentSection } from './types';
import { currentPathname, managedWorkspaceIdFromPath } from './utils';

export function ManagedAgentsPage({ section }: { section: ManagedAgentSection }) {
  const { activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = managedWorkspaceIdFromPath(currentPathname());
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  if (section === 'quickstart') {
    return <AgentQuickstartPage />;
  }

  if (section === 'dreams') {
    return <DreamingPage />;
  }

  return <ManagedResourcePage config={resourceConfigs[section]} routeWorkspaceId={routeWorkspaceId} />;
}

export type { ManagedAgentSection } from './types';

import { useI18n } from '../../shared/i18n';
import { Toaster } from '../../shared/ui/sonner';
import { useWorkspace } from '../../shared/workspaces/context';
import { useEffect } from 'react';
import { AgentQuickstartPage } from './quickstart/AgentQuickstartPage';
import { DreamingPage, ManagedResourcePage, resourceConfigs } from './resources/ManagedResources';
import { type ManagedAgentSection } from './types';
import { currentPathname, managedWorkspaceIdFromPath } from './utils';

export function ManagedAgentsPage({ section }: { section: ManagedAgentSection }) {
  const { msg } = useI18n();
  const { activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = managedWorkspaceIdFromPath(currentPathname());
  const notifications = (
    <Toaster
      position="top-right"
      duration={2200}
      closeButton
      toastOptions={{ closeButtonAriaLabel: msg('common.close', 'Close') }}
    />
  );

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  if (section === 'quickstart') {
    return (
      <>
        {notifications}
        <AgentQuickstartPage />
      </>
    );
  }

  if (section === 'dreams') {
    return (
      <>
        {notifications}
        <DreamingPage />
      </>
    );
  }

  return (
    <>
      {notifications}
      <ManagedResourcePage config={resourceConfigs[section]} routeWorkspaceId={routeWorkspaceId} />
    </>
  );
}

export type { ManagedAgentSection } from './types';

import { useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';
import { TelemetryDestinationsPage } from './TelemetryDestinationsPage';

export function WorkspaceTelemetryPage() {
  const location = useLocation();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const { workspaces, activeWorkspace, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const workspace = workspaces.find((candidate) => candidate.id === routeWorkspaceId) ?? activeWorkspace;
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) selectWorkspace(routeWorkspaceId);
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);
  return <TelemetryDestinationsPage key={workspace.id} workspaceId={workspace.id} />;
}

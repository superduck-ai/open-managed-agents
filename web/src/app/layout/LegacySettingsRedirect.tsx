import { useNavigate, useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';

export function LegacySettingsRedirect({ to }: { to: string }) {
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    void navigate({
      href: to + location.searchStr + (location.hash ? `#${location.hash}` : ''),
      replace: true,
    });
  }, [navigate, to, location.searchStr, location.hash]);
  return null;
}

export function LegacyLLMSettingsRedirect() {
  const location = useLocation();
  const { activeWorkspaceId, selectWorkspace, workspaces, isLoading } = useWorkspace();
  const requestedWorkspaceId = workspaceIdFromPath(location.pathname);
  const workspaceId = workspaces.find((workspace) => workspace.id === requestedWorkspaceId)?.id;
  useEffect(() => {
    if (workspaceId && workspaceId !== activeWorkspaceId) selectWorkspace(workspaceId);
  }, [workspaceId, activeWorkspaceId, selectWorkspace]);
  if (isLoading || (workspaceId && workspaceId !== activeWorkspaceId)) return null;
  return <LegacySettingsRedirect to="/settings/llm" />;
}

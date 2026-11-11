import { useNavigate, useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';

export function LegacySettingsRedirect({ to }: { to: '/settings/llm' | '/settings/telemetry' | '/settings/members' }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { activeWorkspaceId, selectWorkspace, workspaces } = useWorkspace();
  const workspaceId = workspaceIdFromPath(location.pathname);
  useEffect(() => {
    if (
      workspaceId &&
      workspaceId !== activeWorkspaceId &&
      workspaces.some((workspace) => workspace.id === workspaceId)
    )
      selectWorkspace(workspaceId);
  }, [workspaceId, activeWorkspaceId, workspaces, selectWorkspace]);
  useEffect(() => {
    void navigate({
      href: to + location.searchStr + (location.hash ? `#${location.hash}` : ''),
      replace: true,
    });
  }, [navigate, to, location.searchStr, location.hash]);
  return null;
}

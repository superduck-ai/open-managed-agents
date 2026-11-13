import { useNavigate, useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath, workspaceLLMModelsPath } from '../../shared/workspaces/presentation';

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
  const { activeWorkspaceId, isLoading } = useWorkspace();
  if (isLoading) return null;
  return (
    <LegacySettingsRedirect to={workspaceLLMModelsPath(workspaceIdFromPath(location.pathname) ?? activeWorkspaceId)} />
  );
}

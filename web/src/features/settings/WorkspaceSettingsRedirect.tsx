import { Navigate } from '@tanstack/react-router';
import { useWorkspace } from '../../shared/workspaces/context';

export function WorkspaceSettingsRedirect({
  resource,
}: {
  resource: 'keys' | 'webhooks' | 'telemetry' | 'mcp-tunnels';
}) {
  const { activeWorkspaceId } = useWorkspace();
  return (
    <Navigate
      to={`/settings/workspaces/$workspaceId/${resource}`}
      params={{ workspaceId: activeWorkspaceId }}
      replace
    />
  );
}

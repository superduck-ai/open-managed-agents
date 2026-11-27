import { ShieldAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';
import { useWorkspaceCapabilities, useWorkspaceRuntime } from '../../shared/workspaces/context';
import type { BooleanRuntimeCapabilityKey } from '../../shared/workspaces/runtime';

const routeCapabilityRules: Array<{ pattern: RegExp; capability: BooleanRuntimeCapabilityKey }> = [
  { pattern: /^\/(?:workspaces\/[^/]+\/)?deployments(?:\/|$)/, capability: 'deployments' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?observability(?:\/|$)/, capability: 'observability_dashboard' },
  { pattern: /^\/(?:settings\/workspaces\/[^/]+\/)?mcp-tunnels(?:\/|$)/, capability: 'mcp_tunnels' },
  { pattern: /^\/(?:settings\/workspaces\/[^/]+\/)?webhooks(?:\/|$)/, capability: 'session_webhooks' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?channels(?:\/|$)/, capability: 'channels' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?identities(?:\/|$)/, capability: 'channels' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?evaluations(?:\/|$)/, capability: 'evaluations' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?skills(?:\/|$)/, capability: 'skills' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?memory-stores(?:\/|$)/, capability: 'memory_stores' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?credential-vaults(?:\/|$)/, capability: 'credential_vaults' },
  { pattern: /^\/(?:workspaces\/[^/]+\/)?mcp-servers(?:\/|$)/, capability: 'mcp_servers' },
];

export function requiredCapabilityForPath(pathname: string): BooleanRuntimeCapabilityKey | null {
  const normalized = pathname.split(/[?#]/)[0];
  return routeCapabilityRules.find((rule) => rule.pattern.test(normalized))?.capability ?? null;
}

export function RuntimeCapabilityGuard({
  currentPath,
  onNavigate,
  children,
}: {
  currentPath: string;
  onNavigate?: (href: string) => Promise<void> | void;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  const runtime = useWorkspaceRuntime();
  const capabilities = useWorkspaceCapabilities();
  const required = requiredCapabilityForPath(currentPath);

  if (!required || capabilities[required]) {
    return children;
  }

  const handleReturnOverview = () => {
    if (onNavigate) {
      void onNavigate('/dashboard');
      return;
    }
    if (typeof window !== 'undefined') {
      window.location.assign('/dashboard');
    }
  };

  return (
    <Empty className="mx-auto my-12 max-w-lg border border-border bg-card">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ShieldAlert className="size-5 text-muted-foreground" aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{msg('workspace.runtime.unsupportedTitle', 'Not supported by this workspace runtime')}</EmptyTitle>
        <EmptyDescription>
          {msg(
            'workspace.runtime.unsupportedDescription',
            'The current workspace uses {runtime}, which does not support this feature.',
            { runtime: runtime.profile_name },
          )}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button type="button" onClick={handleReturnOverview}>
          {msg('workspace.runtime.returnOverview', 'Return to overview')}
        </Button>
      </EmptyContent>
    </Empty>
  );
}

import type { ReactNode } from 'react';
import { useI18n } from '@/shared/i18n';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';

export function CreateAgentPageLayout({
  workspaceId,
  busy,
  children,
}: {
  workspaceId: string;
  busy?: boolean;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  return (
    <main
      aria-label={msg('managedAgents.agents.createLabel', 'Create agent')}
      aria-busy={busy}
      className="min-w-0 text-foreground"
    >
      <ManagedDetailBreadcrumb
        listHref={`/workspaces/${encodeURIComponent(workspaceId || 'default')}/agents`}
        listLabel={msg('managedAgents.agents.title', 'Agents')}
        currentLabel={msg('managedAgents.agents.newAgent', 'New agent')}
      />
      <div className="mx-auto mt-7 w-full max-w-[824px] pb-6">
        <header className="mb-5">
          <h1 className="text-2xl font-semibold leading-8">
            {msg('managedAgents.agents.createLabel', 'Create agent')}
          </h1>
        </header>
        {children}
      </div>
    </main>
  );
}

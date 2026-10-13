import { useI18n } from '@/shared/i18n';
import { toast } from '@/shared/ui/sonner';
import { createAgentDetailSession } from '../api';
import { ManagedEntityDialog } from '../resources/dialogs';
import type { AgentApiResponse } from '../types';
import { managedEntityDetailHref } from '../utils';

export function AgentStartSessionDialog({
  agent,
  workspaceId,
  onClose,
}: {
  agent: AgentApiResponse;
  workspaceId: string;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  return (
    <ManagedEntityDialog
      section="sessions"
      title={msg('managedAgents.sessions.createLabel', 'Create session')}
      lockedAgent={agent}
      workspaceId={workspaceId}
      onClose={onClose}
      onSubmit={async (values) => {
        const session = await createAgentDetailSession(agent, values, workspaceId);
        onClose();
        toast.success(msg('managedAgents.sessions.toastCreated', 'Session started'));
        window.history.pushState(
          null,
          '',
          `${managedEntityDetailHref(workspaceId, 'sessions', session.id)}?interactive=true`,
        );
        const event = typeof PopStateEvent === 'function' ? new PopStateEvent('popstate') : new Event('popstate');
        window.dispatchEvent(event);
      }}
    />
  );
}

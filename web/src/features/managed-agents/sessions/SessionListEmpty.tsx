import { productIcons } from '@/shared/icons/product-icons';
import { Search } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { AgentsListState } from '../components/common';

export function SessionListEmpty({
  filtered,
  error,
  onCreate,
  onReset,
  onRetry,
}: {
  filtered: boolean;
  error: string | null;
  onCreate: () => void;
  onReset: () => void;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div role={error ? 'alert' : undefined}>
      <AgentsListState
        icon={filtered ? Search : productIcons.sessions}
        title={
          error
            ? msg('managedAgents.sessions.loadFailed', 'Failed to load sessions')
            : filtered
              ? msg('managedAgents.sessions.noMatchingSessions', 'No matching sessions')
              : msg('managedAgents.sessions.emptyTitle', 'No sessions yet')
        }
        body={
          error ||
          (filtered
            ? msg('managedAgents.sessions.noMatchingHint', 'No sessions match the current filters.')
            : msg('managedAgents.sessions.emptyBody', 'Create a session to trace and debug it here.'))
        }
        actionLabel={
          error
            ? msg('common.retry', 'Retry')
            : filtered
              ? msg('managedAgents.sessions.resetFilters', 'Reset filters')
              : msg('managedAgents.sessions.createLabel', 'Create session')
        }
        onAction={error ? onRetry : filtered ? onReset : onCreate}
      />
    </div>
  );
}

import { MessageSquare, Search } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../../shared/ui/empty';

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
    <Empty className="min-h-64 rounded-none border-b">
      <EmptyHeader>
        <EmptyMedia variant="icon">{filtered ? <Search aria-hidden /> : <MessageSquare aria-hidden />}</EmptyMedia>
        <EmptyTitle>
          {error
            ? msg('managedAgents.sessions.loadFailed', 'Failed to load sessions')
            : filtered
              ? msg('managedAgents.sessions.noMatchingSessions', 'No matching sessions')
              : msg('managedAgents.sessions.emptyTitle', 'No sessions yet')}
        </EmptyTitle>
        <EmptyDescription role={error ? 'alert' : undefined}>
          {error ||
            (filtered
              ? msg('managedAgents.sessions.noMatchingHint', 'No sessions match the current filters.')
              : msg('managedAgents.sessions.emptyBody', 'Create a session to trace and debug it here.'))}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" onClick={error ? onRetry : filtered ? onReset : onCreate}>
          {error
            ? msg('common.retry', 'Retry')
            : filtered
              ? msg('managedAgents.sessions.resetFilters', 'Reset filters')
              : msg('managedAgents.sessions.createLabel', 'Create session')}
        </Button>
      </EmptyContent>
    </Empty>
  );
}

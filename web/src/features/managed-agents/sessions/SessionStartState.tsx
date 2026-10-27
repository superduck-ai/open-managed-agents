import { MessageSquare } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../../shared/ui/empty';

export function SessionStartState({ onStart }: { onStart: () => void }) {
  const { msg } = useI18n();
  return (
    <Empty className="min-h-56 py-16">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <MessageSquare aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{msg('managedAgents.sessions.startConversation', 'Start a conversation')}</EmptyTitle>
        <EmptyDescription>
          {msg(
            'managedAgents.sessions.startConversationHint',
            'Your session is ready. Send a message below to give your agent a task.',
          )}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" onClick={onStart}>
          {msg('managedAgents.sessions.writeMessage', 'Write a message')}
        </Button>
      </EmptyContent>
    </Empty>
  );
}

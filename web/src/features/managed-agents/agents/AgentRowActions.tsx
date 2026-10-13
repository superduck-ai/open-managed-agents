import { Archive, PanelRight, Play } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { MoreActionsButton } from '@/shared/ui/data-table-interactions';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/shared/ui/dropdown-menu';
import type { AgentApiResponse } from '../types';

export function AgentRowActions({
  agent,
  archiving,
  onQuickView,
  onStartSession,
  onArchive,
}: {
  agent: AgentApiResponse;
  archiving: boolean;
  onQuickView: () => void;
  onStartSession: () => void;
  onArchive: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex items-center justify-end gap-1">
      <Button
        type="button"
        variant="ghost"
        size="xs"
        onClick={onQuickView}
        aria-label={msg('managedAgents.agents.quickViewAgent', 'Quick view {name}', { name: agent.name || agent.id })}
      >
        <span className="hidden sm:inline">{msg('managedAgents.agents.quickView', 'Quick view')}</span>
        <PanelRight aria-hidden />
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <MoreActionsButton label={msg('managedAgents.common.moreActions', 'More actions')} disabled={archiving} />
          }
        />
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuItem disabled={archiving || Boolean(agent.archived_at)} onClick={onStartSession}>
            <Play aria-hidden />
            {msg('managedAgents.agents.startConversation', 'Start conversation')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={archiving || Boolean(agent.archived_at)} onClick={onArchive}>
            <Archive aria-hidden />
            {msg('managedAgents.agents.archiveAgent', 'Archive agent')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

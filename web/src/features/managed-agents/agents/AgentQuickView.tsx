import { useQuery } from '@tanstack/react-query';
import { ArrowUpRight, Bot, ChevronDown, ChevronUp, Play, X } from 'lucide-react';
import type { ReactNode } from 'react';
import { useI18n } from '@/shared/i18n';
import { Badge } from '@/shared/ui/badge';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { retrieveAgent } from '../api';
import { ManagedErrorAlert } from '../components/common';
import { managedColumnLabel } from '../labels';
import type { AgentApiResponse } from '../types';
import { agentDetailHref, errorMessage, handleInternalLinkClick } from '../utils';
import { agentModelName, agentSkillLabel, agentSkillSnapshotTitle, formatDetailDate } from './model';
import { AgentToolsSection } from './tools/AgentToolsSection';
import { hasConfiguredAgentTools } from './tools/model';

export function AgentQuickView({
  agentId,
  version,
  agentIds,
  workspaceId,
  orgUuid,
  onSelect,
  onClose,
  onStartSession,
}: {
  version?: number;
  agentId: string;
  agentIds: string[];
  workspaceId: string;
  orgUuid: string;
  onSelect: (agentId: string) => void;
  onClose: () => void;
  onStartSession?: (agent: AgentApiResponse) => void;
}) {
  const { msg } = useI18n();
  const index = agentIds.indexOf(agentId);
  const query = useQuery({
    queryKey: ['agent-quick-view', orgUuid, workspaceId, agentId, version],
    queryFn: () => retrieveAgent(agentId, workspaceId, version),
    staleTime: 0,
    retry: false,
  });
  return (
    <Sheet open modal={false} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        showOverlay={false}
        showCloseButton={false}
        className="gap-0 sm:inset-y-2 sm:right-2 sm:h-auto sm:max-w-[480px] sm:rounded-xl sm:border"
      >
        <SheetHeader className="flex-row items-center gap-2 border-b px-4 py-3">
          <div className="min-w-0 flex-1">
            <SheetTitle className="text-xs text-muted-foreground">
              {msg('managedAgents.agents.kindTitle', 'Agent')}
            </SheetTitle>
            <SheetDescription className="truncate font-mono text-xs">{agentId}</SheetDescription>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={msg('managedAgents.agents.previousAgent', 'Previous agent')}
            disabled={index <= 0}
            onClick={() => onSelect(agentIds[index - 1])}
          >
            <ChevronUp aria-hidden />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={msg('managedAgents.agents.nextAgent', 'Next agent')}
            disabled={index < 0 || index >= agentIds.length - 1}
            onClick={() => onSelect(agentIds[index + 1])}
          >
            <ChevronDown aria-hidden />
          </Button>
          <SheetClose render={<Button variant="ghost" size="icon-sm" aria-label={msg('common.close', 'Close')} />}>
            <X aria-hidden />
          </SheetClose>
        </SheetHeader>
        <div className="subtle-scrollbar min-h-0 flex-1 overflow-y-auto p-4" aria-busy={query.isPending}>
          {query.isPending ? (
            <p role="status" className="text-sm text-muted-foreground">
              {msg('managedAgents.agents.loading', 'Loading agents...')}
            </p>
          ) : null}
          {query.isError ? (
            <div className="space-y-3">
              <ManagedErrorAlert>{errorMessage(query.error)}</ManagedErrorAlert>
              <Button variant="outline" onClick={() => void query.refetch()}>
                {msg('common.retry', 'Retry')}
              </Button>
            </div>
          ) : null}
          {query.data ? (
            <AgentQuickViewContent
              agent={query.data}
              workspaceId={workspaceId}
              orgUuid={orgUuid}
              onStartSession={onStartSession}
            />
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function QuickViewSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-medium">{title}</h3>
      {children}
    </section>
  );
}

function AgentQuickViewContent({
  agent,
  workspaceId,
  orgUuid,
  onStartSession,
}: {
  agent: AgentApiResponse;
  workspaceId: string;
  orgUuid: string;
  onStartSession?: (agent: AgentApiResponse) => void;
}) {
  const { msg, locale } = useI18n();
  const href = agentDetailHref(workspaceId, agent.id);
  const metadata = [
    [managedColumnLabel('Created', msg), formatDetailDate(agent.created_at, locale)],
    [managedColumnLabel('Last updated', msg), formatDetailDate(agent.updated_at, locale)],
    [msg('analytics.table.model', 'Model'), agentModelName(agent.model)],
  ];
  return (
    <div className="space-y-6 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <Bot className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <h2 className="min-w-0 flex-1 break-words font-semibold">
          {agent.name || msg('managedAgents.agents.untitled', 'Untitled agent')}
        </h2>
        <Badge variant="secondary">V{agent.version}</Badge>
        <ButtonLink href={href} variant="outline" size="xs" onClick={(event) => handleInternalLinkClick(event, href)}>
          {msg('managedAgents.agents.openAgent', 'Open')}
          <ArrowUpRight aria-hidden />
        </ButtonLink>
      </div>
      <dl className="space-y-2">
        {metadata.map(([label, value]) => (
          <div key={label} className="grid grid-cols-[100px_minmax(0,1fr)] gap-3">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-words">{value || '-'}</dd>
          </div>
        ))}
      </dl>
      <QuickViewSection title={msg('common.description', 'Description')}>
        <p className="whitespace-pre-wrap break-words rounded-lg bg-muted p-3">
          {agent.description || msg('managedAgents.agents.noDescription', 'No description provided.')}
        </p>
      </QuickViewSection>
      <QuickViewSection title={msg('managedAgents.agents.detail.systemPrompt', 'System prompt')}>
        <p className="whitespace-pre-wrap break-words rounded-lg bg-muted p-3 leading-6">
          {agent.system || msg('managedAgents.agents.detail.noSystemPrompt', 'No system prompt configured.')}
        </p>
      </QuickViewSection>
      <QuickViewSection title={msg('managedAgents.agents.detail.mcpsAndTools', 'MCPs and tools')}>
        {hasConfiguredAgentTools(agent) ? (
          <AgentToolsSection agent={agent} workspaceId={workspaceId} orgUuid={orgUuid} />
        ) : (
          <p className="text-muted-foreground">
            {msg('managedAgents.agents.detail.noMcpsOrTools', 'No MCPs or tools configured.')}
          </p>
        )}
      </QuickViewSection>
      <QuickViewSection title={msg('managedAgents.skills.title', 'Skills')}>
        {agent.skills.length ? (
          <ul className="space-y-2">
            {agent.skills.map((skill, index) => (
              <li key={index} className="break-words rounded-lg bg-muted p-3">
                {agentSkillSnapshotTitle(skill) || agentSkillLabel(skill)}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-muted-foreground">
            {msg('managedAgents.agents.detail.noSkills', 'No skills configured.')}
          </p>
        )}
      </QuickViewSection>
      {onStartSession ? (
        <Button className="w-full" disabled={Boolean(agent.archived_at)} onClick={() => onStartSession(agent)}>
          <Play aria-hidden />
          {msg('managedAgents.agents.startConversation', 'Start conversation')}
        </Button>
      ) : null}
    </div>
  );
}

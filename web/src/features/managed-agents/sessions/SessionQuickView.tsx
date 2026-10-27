import { ArrowUpRight, MessageSquare, RotateCcw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from '../../../shared/ui/sheet';
import { Skeleton } from '../../../shared/ui/skeleton';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { listSessionEvents, retrieveSessionDetailSession } from '../api';
import { ManagedErrorAlert } from '../components/common';
import { ResourceIdButton } from '../components/ResourceIdButton';
import { entityAgentId } from '../resources/model';
import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import {
  agentDetailHref,
  errorMessage,
  handleInternalLinkClick,
  managedEntityDetailHref,
  navigateToInternalHref,
} from '../utils';
import { buildSessionTraceEntries } from './sessionTraceModel';
import { SessionQuickViewActivity } from './SessionQuickViewActivity';
import { SessionStatusPill } from './sessionTimeline';
import { buildSessionDetailSummary } from './sessionDetailModel';

export function SessionQuickView({
  session,
  workspaceId,
  onClose,
}: {
  session: SessionApiResponse;
  workspaceId: string;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(true);
  const [detail, setDetail] = useState(session);
  const [events, setEvents] = useState<QuickstartSessionEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    setLoading(true);
    setLoadError(null);
    void Promise.all([
      retrieveSessionDetailSession(session.id, workspaceId),
      listSessionEvents(session.id, workspaceId, 'desc', 20, undefined, controller.signal),
    ])
      .then(([nextSession, page]) => {
        if (!active) return;
        setDetail(nextSession);
        setEvents([...(page.data ?? [])].reverse());
        setLoading(false);
      })
      .catch((error) => {
        if (!active) return;
        setLoadError(errorMessage(error));
        setLoading(false);
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [session.id, workspaceId, refreshKey]);
  const entries = buildSessionTraceEntries(events, 'transcript', 0, msg, { platformTranscriptFiltering: true });
  const href = `${managedEntityDetailHref(workspaceId, 'sessions', session.id)}?interactive=true`;
  const viewOnly = Boolean(detail.archived_at) || ['terminated', 'deleted'].includes(detail.status);
  return (
    <TooltipProvider>
      <Sheet open={open} onOpenChange={setOpen} onOpenChangeComplete={(nextOpen) => !nextOpen && onClose()}>
        <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-xl">
          <SheetHeader className="p-5 pr-12">
            <SheetTitle className="break-words">
              {detail.title || msg('managedAgents.sessions.quickView', 'Quick view')}
            </SheetTitle>
            <SheetDescription className="sr-only">
              {msg('managedAgents.sessions.quickViewHint', 'Inspect this session without leaving the list.')}
            </SheetDescription>
            <ResourceIdButton value={detail.id} wrap />
          </SheetHeader>
          <div className="subtle-scrollbar min-h-0 flex-1 overflow-y-auto p-5">
            <SessionQuickViewFacts session={detail} events={events} workspaceId={workspaceId} />
            <div className="mt-6 flex items-center justify-between gap-2">
              <h3 className="text-sm font-medium">
                {msg('managedAgents.sessions.conversationPreview', 'Conversation preview')}
              </h3>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={loading}
                aria-label={msg('common.refresh', 'Refresh')}
                onClick={() => setRefreshKey((value) => value + 1)}
              >
                <RotateCcw className="size-4" aria-hidden />
              </Button>
            </div>
            {loadError ? <ManagedErrorAlert className="mt-3">{loadError}</ManagedErrorAlert> : null}
            {loading ? (
              <div className="mt-3 space-y-3">
                <Skeleton className="h-16" />
                <Skeleton className="h-16" />
              </div>
            ) : (
              <SessionQuickViewActivity entries={entries} />
            )}
            {!loading && !loadError && !entries.length ? (
              <p className="py-6 text-sm text-muted-foreground">
                {events.length
                  ? msg(
                      'managedAgents.sessions.noPreviewContent',
                      'No conversation content in the latest 20 events. Open the session for full history.',
                    )
                  : msg(
                      'managedAgents.sessions.noActivity',
                      'No activity yet. Open the conversation to send the first message.',
                    )}
              </p>
            ) : null}
          </div>
          <SheetFooter className="p-5">
            <Button
              onClick={() => {
                onClose();
                navigateToInternalHref(href);
              }}
            >
              {viewOnly ? (
                <ArrowUpRight className="size-4" aria-hidden />
              ) : (
                <MessageSquare className="size-4" aria-hidden />
              )}
              {viewOnly
                ? msg('managedAgents.sessions.viewSession', 'View session')
                : msg('managedAgents.sessions.openConversation', 'Open conversation')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </TooltipProvider>
  );
}

function SessionQuickViewFacts({
  session,
  events,
  workspaceId,
}: {
  session: SessionApiResponse;
  events: QuickstartSessionEvent[];
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const summary = buildSessionDetailSummary(session, events, formatters, msg);
  const agentId = entityAgentId(session);
  const references = [
    { label: msg('managedAgents.sessions.detail.agentTab', 'Agent'), id: agentId, section: 'agents' as const },
    {
      label: msg('managedAgents.sessions.detail.environmentTab', 'Environment'),
      id: session.environment_id,
      section: 'environments' as const,
    },
    {
      label: msg('managedAgents.sessions.fields.deployment', 'Deployment'),
      id: session.deployment_id,
      section: 'deployments' as const,
    },
  ];
  return (
    <dl className="space-y-3 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <dt className="text-muted-foreground">{msg('common.status', 'Status')}</dt>
        <dd>
          <SessionStatusPill status={session.archived_at ? msg('common.archived', 'Archived') : summary.statusLabel} />
        </dd>
      </div>
      {references
        .filter((reference) => reference.id)
        .map((reference) => {
          const href =
            reference.section === 'agents'
              ? agentDetailHref(workspaceId, reference.id!)
              : managedEntityDetailHref(workspaceId, reference.section, reference.id!);
          return (
            <div key={reference.section} className="space-y-1">
              <dt className="text-muted-foreground">{reference.label}</dt>
              <dd className="flex min-w-0 items-center gap-2">
                <ResourceIdButton value={reference.id!} />
                <a
                  href={href}
                  className="shrink-0 rounded-sm p-1 text-muted-foreground hover:text-foreground"
                  aria-label={reference.label}
                  onClick={(event) => handleInternalLinkClick(event, href)}
                >
                  <ArrowUpRight className="size-4" aria-hidden />
                </a>
              </dd>
            </div>
          );
        })}
      <div className="flex flex-wrap justify-between gap-2">
        <dt className="text-muted-foreground">{msg('common.created', 'Created')}</dt>
        <dd>{formatters.date(session.created_at, { dateStyle: 'medium', timeStyle: 'short' })}</dd>
      </div>
    </dl>
  );
}

import { ChevronRight, Wrench } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import type { SessionTraceEntry } from '../types';
import { TranscriptContent } from './SessionTranscriptContent';
import {
  sessionEventErrorMessage,
  sessionEventIsThinking,
  sessionThinkingText,
  sessionToolDisplayText,
  sessionTraceTextIsJson,
} from './sessionTraceModel';

export function SessionQuickViewActivity({ entries }: { entries: SessionTraceEntry[] }) {
  const { msg } = useI18n();
  return (
    <ol className="mt-4 space-y-5">
      {entries.map((entry) => {
        const error = entry.isError ? sessionEventErrorMessage(entry.event) : '';
        const content = error && !sessionTraceTextIsJson(error) ? error : entry.displayText || entry.preview;
        return (
          <li key={entry.id} className="min-w-0 break-words text-sm">
            {sessionEventIsThinking(entry.event) ? (
              <Collapsible>
                <CollapsibleTrigger className="group flex items-center gap-1 text-xs text-muted-foreground">
                  <ChevronRight className="size-3 transition-transform group-aria-expanded:rotate-90" aria-hidden />
                  {entry.label}
                </CollapsibleTrigger>
                <CollapsibleContent className="mt-2 text-muted-foreground [&_*]:text-muted-foreground">
                  <TranscriptContent value={sessionThinkingText(entry.event) || entry.preview} />
                </CollapsibleContent>
              </Collapsible>
            ) : entry.family === 'tool_use' ? (
              <div className="flex items-start gap-2 rounded-md bg-muted/50 px-3 py-2">
                <Wrench className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                <div className="min-w-0">
                  <p className="font-medium">{entry.label}</p>
                  <p className="mt-0.5 whitespace-pre-wrap break-all text-xs text-muted-foreground">
                    {sessionToolDisplayText(entry.event)}
                  </p>
                  {entry.resultEvent?.is_error ? (
                    <p className="mt-1 text-xs text-destructive">
                      {msg('managedAgents.sessions.state.failed', 'Failed')}
                    </p>
                  ) : null}
                </div>
              </div>
            ) : (
              <>
                <p className="mb-1.5 text-xs font-medium text-muted-foreground">
                  {entry.family === 'user'
                    ? msg('managedAgents.sessions.trace.user', 'User')
                    : entry.family === 'agent'
                      ? msg('managedAgents.sessions.trace.agent', 'Agent')
                      : entry.label}
                </p>
                <div className={entry.isError ? 'text-destructive [&_*]:text-destructive' : undefined}>
                  <TranscriptContent value={content} />
                </div>
              </>
            )}
          </li>
        );
      })}
    </ol>
  );
}

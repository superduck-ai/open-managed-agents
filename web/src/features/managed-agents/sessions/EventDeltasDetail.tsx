import { useI18n } from '../../../shared/i18n';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '../../../shared/ui/accordion';
import { CopyButton, SyntaxCodeBlock } from '../components/CodeBlocks';
import { toRecord } from '../utils';
import type { QuickstartSessionEvent } from '../types';
import { sessionEventDebugJson, sessionEventType } from './sessionTraceModel';

export function EventDeltasDetail({
  eventId,
  eventType,
  frames,
}: {
  eventId: string;
  eventType: string;
  frames: QuickstartSessionEvent[];
}) {
  const { msg } = useI18n();
  return (
    <div className="mx-3 mb-4 mt-3 border-t border-dashed border-border pt-3">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2 font-mono text-xs text-muted-foreground">
        <span>{eventType}</span>
        <span className="min-w-0 break-all">{eventId}</span>
      </div>
      <p className="mb-3 text-xs text-muted-foreground">
        {msg(
          'managedAgents.sessions.trace.ephemeralFrames',
          'Ephemeral data — only events streamed live in this browser tab are captured.',
        )}
      </p>
      <Accordion
        multiple
        defaultValue={eventType === 'agent.thinking' ? [0, 1] : []}
        className="overflow-hidden rounded-xl border border-border"
      >
        {frames.map((event, index) => {
          const json = sessionEventDebugJson(event);
          return (
            <AccordionItem key={index} value={index}>
              <AccordionTrigger className="min-w-0 px-3 py-3 font-mono text-xs hover:no-underline">
                <span className="flex min-w-0 items-baseline gap-2">
                  <span className="shrink-0 text-muted-foreground">{sessionEventType(event)}</span>
                  <span className="min-w-0 truncate">{frameSummary(event, eventId)}</span>
                </span>
              </AccordionTrigger>
              <AccordionContent className="px-3 pb-3">
                <div className="relative">
                  <div className="absolute right-2 top-2 z-10">
                    <CopyButton value={json} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
                  </div>
                  <SyntaxCodeBlock value={json} language="json" />
                </div>
              </AccordionContent>
            </AccordionItem>
          );
        })}
      </Accordion>
    </div>
  );
}

function frameSummary(event: QuickstartSessionEvent, eventId: string) {
  if (sessionEventType(event) !== 'event_delta') return eventId;
  const content = toRecord(toRecord(event.delta)?.content);
  return typeof content?.text === 'string' ? content.text.replaceAll('\n', '↵') : eventId;
}

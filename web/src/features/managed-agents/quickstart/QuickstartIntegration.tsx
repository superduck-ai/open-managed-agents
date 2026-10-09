import { QuickstartChat } from './QuickstartChat';
import { useState } from 'react';
import { RotateCcw } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '../../../shared/ui/accordion';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { quickstartCopy } from './copy';
import { quickstartCurl, RequestPreview } from './RequestPreview';
import type { QuickstartWizard } from './useQuickstartWizard';
import { useQuickstartConversation } from './useQuickstartConversation';
import { QuickstartFooter } from './QuickstartFooter';
import { QuickstartApiKey } from './QuickstartApiKey';

export function QuickstartIntegration({
  wizard,
  workspaceID,
  accountID,
  binding,
}: {
  wizard: QuickstartWizard;
  workspaceID: string;
  accountID: string;
  binding: string;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const conversation = useQuickstartConversation(wizard, workspaceID, accountID, binding);
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col pt-4">
      <div className="mb-5 shrink-0 space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{text.integrationTitle}</h1>
        <p className="text-muted-foreground">{text.integrationSubtitle}</p>
      </div>
      <div className="grid min-h-0 min-w-0 flex-1 auto-rows-max items-stretch gap-6 overflow-y-auto min-[860px]:grid-cols-2 min-[860px]:grid-rows-[minmax(0,1fr)] min-[860px]:overflow-hidden">
        <QuickstartApiCalls wizard={wizard} workspaceID={workspaceID} message={conversation.message} />
        <QuickstartChat conversation={conversation} wizard={wizard} workspaceID={workspaceID} />
      </div>
      <QuickstartFooter wizard={wizard}>
        <Button
          variant="outline"
          disabled={
            conversation.sending ||
            conversation.running ||
            Boolean(conversation.action) ||
            conversation.unconfirmed ||
            wizard.busy ||
            Boolean(wizard.progress.pending)
          }
          onClick={conversation.newConversation}
        >
          <RotateCcw />
          {text.newSession}
        </Button>
        <ButtonLink
          href={`/workspaces/${encodeURIComponent(workspaceID)}/agents/${encodeURIComponent(wizard.agent?.id ?? '')}`}
        >
          {text.viewAgent}
        </ButtonLink>
      </QuickstartFooter>
    </div>
  );
}

function QuickstartApiCalls({
  wizard,
  workspaceID,
  message,
}: {
  wizard: QuickstartWizard;
  workspaceID: string;
  message: string;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const [exampleSessionID, setExampleSessionID] = useState('');
  const id = encodeURIComponent(exampleSessionID || 'SESSION_ID');
  const calls = [
    {
      id: 'create',
      title: text.createSession,
      hint: text.createSessionHint,
      code: quickstartCurl('/v1/sessions', workspaceID, {
        agent: { type: 'agent', id: wizard.agent?.id, version: wizard.agent?.version },
        environment_id: wizard.environment?.id,
        vault_ids: [],
        resources: [],
        metadata: {},
      }),
    },
    {
      id: 'listen',
      title: text.listen,
      hint: text.listenHint,
      code: quickstartCurl(`/v1/sessions/${id}/events/stream`, workspaceID, undefined, true),
    },
    {
      id: 'send',
      title: text.sendMessage,
      hint: text.sendHint,
      code: quickstartCurl(`/v1/sessions/${id}/events`, workspaceID, {
        events: [{ type: 'user.message', content: [{ type: 'text', text: message || text.suggested }] }],
      }),
    },
  ];
  return (
    <aside className="min-h-0 min-w-0 space-y-5 rounded-xl bg-muted/50 p-4 min-[860px]:overflow-y-auto sm:p-6">
      <QuickstartApiKey workspaceID={workspaceID} />
      <Accordion defaultValue={['create']} aria-label={text.apiCalls} className="gap-3">
        {calls.map((call, index) => (
          <AccordionItem
            key={call.id}
            value={call.id}
            className="rounded-lg border border-border bg-background last:border-b"
          >
            <AccordionTrigger className="px-4 hover:no-underline">
              {index + 1}. {call.title}
            </AccordionTrigger>
            <AccordionContent className="space-y-3 border-t border-border px-4 pt-4">
              <p className="text-sm leading-relaxed text-muted-foreground">{call.hint}</p>
              {call.id !== 'create' && (
                <Field>
                  <FieldLabel htmlFor={`example-session-id-${call.id}`}>{text.sessionID}</FieldLabel>
                  <Input
                    id={`example-session-id-${call.id}`}
                    value={exampleSessionID}
                    placeholder={text.sessionIDPlaceholder}
                    onChange={(event) => setExampleSessionID(event.target.value)}
                    className="font-mono text-xs"
                    aria-describedby={`example-session-hint-${call.id}`}
                  />
                  <p id={`example-session-hint-${call.id}`} className="text-xs leading-relaxed text-muted-foreground">
                    {text.sessionIDHint}
                  </p>
                </Field>
              )}
              <RequestPreview code={call.code} hint={false} />
            </AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>
    </aside>
  );
}

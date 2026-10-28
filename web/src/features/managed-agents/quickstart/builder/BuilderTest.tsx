import { useEffect, useRef } from 'react';
import { Button } from '../../../../shared/ui/button';
import { useI18n } from '../../../../shared/i18n';
import { useQuickstartConversation } from '../useQuickstartConversation';
import { QuickstartChat } from '../QuickstartChat';
import { builderTestBinding } from './model';
import {
  sessionEventType,
  sessionEventTranscriptText,
  sessionToolDisplayName,
  sessionToolResultText,
} from '../../sessions/sessionTraceModel';
import type { Builder } from './useBuilder';

export function BuilderTest({
  builder,
  workspaceId,
  accountId,
}: {
  builder: Builder;
  workspaceId: string;
  accountId: string;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const conversation = useQuickstartConversation(
    {
      progress: { ...state, pending: state.operation },
      busy: builder.busy,
      createSession: async () => undefined,
      patchSession: (patch) =>
        builder.update((value) => ({
          ...value,
          sessionID: patch.sessionID ?? value.sessionID,
          sessionBinding: patch.sessionBinding ?? value.sessionBinding,
          unconfirmedMessage: patch.unconfirmedMessage ?? value.unconfirmedMessage,
        })),
    },
    workspaceId,
    accountId,
    builderTestBinding(state),
  );
  const initialized = useRef(false);
  useEffect(() => {
    if (!initialized.current) {
      if (!conversation.unconfirmed) conversation.setMessage(state.suggestedMessage);
      initialized.current = true;
    }
  }, [conversation, state.suggestedMessage]);
  const first = conversation.data.events.find((event) => sessionEventType(event) === 'user.message');
  const firstText = first ? sessionEventTranscriptText(first) : '';
  useEffect(() => {
    if (firstText && !state.firstMessage) builder.update((value) => ({ ...value, firstMessage: firstText }));
    if (
      state.gate?.name === 'await_test_run' &&
      state.gate.input.until === 'first_message' &&
      firstText &&
      !builder.busy
    )
      void builder.resolve({ session_id: state.sessionID, first_message: firstText });
  }, [firstText, state.firstMessage, state.gate, builder.busy]);
  const relevantEvents = conversation.data.events.filter((event) =>
    [
      'user.message',
      'agent.message',
      'assistant.message',
      'agent.tool_use',
      'agent.tool_result',
      'session.status_idle',
      'session.status_terminated',
      'session.thread_status_terminated',
      'session.deleted',
    ].includes(sessionEventType(event)),
  );
  const summary = relevantEvents.slice(-40).map((event) => {
    const type = sessionEventType(event);
    const content = type === 'agent.tool_result' ? sessionToolResultText(event) : sessionEventTranscriptText(event);
    return {
      type,
      text: content.slice(0, 1000) || undefined,
      text_truncated: content.length > 1000 || undefined,
      text_missing:
        (['user.message', 'agent.message', 'assistant.message', 'agent.tool_result'].includes(type) && !content) ||
        undefined,
      id: typeof event.id === 'string' ? event.id.slice(0, 128) : undefined,
      tool_use_id: typeof event.tool_use_id === 'string' ? event.tool_use_id.slice(0, 128) : undefined,
      processed_at: typeof event.processed_at === 'string' ? event.processed_at.slice(0, 64) : undefined,
      stop_reason:
        typeof event.stop_reason === 'object' && event.stop_reason && 'type' in event.stop_reason
          ? String(event.stop_reason.type).slice(0, 128)
          : undefined,
      tool: sessionToolDisplayName(event).slice(0, 128),
      is_error: typeof event.is_error === 'boolean' ? event.is_error : undefined,
    };
  });
  const omitted = relevantEvents.length - summary.length;
  const evidence = JSON.stringify(
    omitted ? [{ type: 'evidence.truncated', omitted_events: omitted }, ...summary] : summary,
  );
  useEffect(() => {
    const metadata = summary.map(({ text: _text, ...event }) => event);
    builder.observeTest(
      JSON.stringify(omitted ? [{ type: 'evidence.truncated', omitted_events: omitted }, ...metadata] : metadata),
    );
  }, [evidence]);
  const end = () => {
    void builder.closeTest(evidence);
  };
  return (
    <section className="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-3">
      <QuickstartChat conversation={conversation} workspaceID={workspaceId} className="h-full" />
      <div className="flex shrink-0 justify-end gap-2">
        <Button
          disabled={
            builder.busy ||
            conversation.running ||
            Boolean(conversation.action) ||
            conversation.sending ||
            conversation.unconfirmed ||
            !conversation.historyReady
          }
          onClick={end}
        >
          {zh ? '结束试跑并复盘' : 'End test and review'}
        </Button>
      </div>
    </section>
  );
}

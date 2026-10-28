import { useEffect, useRef } from 'react';
import { Button } from '../../../../shared/ui/button';
import { useI18n } from '../../../../shared/i18n';
import { useQuickstartConversation } from '../useQuickstartConversation';
import { QuickstartChat } from '../QuickstartChat';
import { builderTestBinding } from './model';
import { sessionEventType, sessionEventTranscriptText, sessionToolDisplayName } from '../../sessions/sessionTraceModel';
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
      conversation.setMessage(state.suggestedMessage);
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
  const summary = conversation.data.events.slice(-20).map((event) => ({
    type: sessionEventType(event),
    text: ['user.message', 'agent.message', 'assistant.message'].includes(sessionEventType(event))
      ? sessionEventTranscriptText(event).slice(0, 1000)
      : undefined,
    stop_reason:
      typeof event.stop_reason === 'object' && event.stop_reason && 'type' in event.stop_reason
        ? String(event.stop_reason.type)
        : undefined,
    tool: sessionToolDisplayName(event),
    is_error: event.is_error === true,
  }));
  const evidence = JSON.stringify(summary);
  useEffect(() => {
    builder.observeTest(JSON.stringify(summary.map(({ text: _text, ...metadata }) => metadata)));
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
            !conversation.bindingValid ||
            conversation.data.loading ||
            !conversation.data.connected
          }
          onClick={end}
        >
          {zh ? '结束试跑并复盘' : 'End test and review'}
        </Button>
      </div>
    </section>
  );
}

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
  const end = () => {
    const summary = conversation.data.events.slice(-100).map((event) => ({
      type: sessionEventType(event),
      text: ['user.message', 'agent.message', 'assistant.message'].includes(sessionEventType(event))
        ? sessionEventTranscriptText(event).slice(0, 3000)
        : undefined,
      stop_reason: event.stop_reason,
      tool: sessionToolDisplayName(event),
      is_error: event.is_error === true,
    }));
    void builder.closeTest(JSON.stringify(summary));
  };
  return (
    <section className="flex h-[600px] min-h-0 min-w-0 flex-1 xl:h-full flex-col gap-3">
      <QuickstartChat conversation={conversation} workspaceID={workspaceId} />
      <div className="flex justify-end gap-2">
        {conversation.running || conversation.action ? (
          <Button variant="outline" disabled={conversation.sending} onClick={() => void conversation.stop()}>
            {zh ? '中断试跑' : 'Interrupt test'}
          </Button>
        ) : (
          <Button
            disabled={
              builder.busy ||
              conversation.sending ||
              conversation.unconfirmed ||
              !conversation.bindingValid ||
              conversation.data.loading ||
              !conversation.data.connected
            }
            onClick={end}
          >
            {zh ? '结束试跑' : 'End test'}
          </Button>
        )}
      </div>
    </section>
  );
}

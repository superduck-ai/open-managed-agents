import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { ArrowUp, Square } from 'lucide-react';
import { useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { interruptQuickstartSession, postQuickstartSessionMessage } from '../api';
import { type QuickstartSessionEvent } from '../types';
import {
  quickstartComposerFrameClassName,
  quickstartComposerSendButtonClassName,
  quickstartComposerTextareaClassName,
} from '../components/composerStyles';
import { errorMessage } from '../utils';

export function SessionMessageComposer({
  awaitingAction = false,
  disabled,
  live,
  onError,
  onEventsChanged,
  onMessageSent,
  sessionId,
  workspaceId,
}: {
  awaitingAction?: boolean;
  disabled: boolean;
  live: boolean;
  onError: (error: string | null) => void;
  onEventsChanged: () => void;
  onMessageSent: (events: QuickstartSessionEvent[]) => void;
  sessionId: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  // In-flight guards are refs: state updates only apply after a re-render, so a
  // second Enter (or Enter + send click) in the same frame would pass a state
  // guard and double-post the message.
  const sendingRef = useRef(false);
  const interruptingRef = useRef(false);
  const trimmedDraft = draft.trim();
  const composerDisabled = disabled || awaitingAction;

  const submit = async () => {
    if (!trimmedDraft || composerDisabled || sendingRef.current) {
      return;
    }
    sendingRef.current = true;
    setSending(true);
    onError(null);
    try {
      const response = await postQuickstartSessionMessage(sessionId, trimmedDraft, workspaceId);
      setDraft('');
      // Always mark the session running: even with no returned events the stream
      // loop's forced tail sync backfills the message, so no history-refresh
      // fallback is needed.
      onMessageSent(response.data ?? []);
    } catch (error) {
      onError(errorMessage(error));
    } finally {
      sendingRef.current = false;
      setSending(false);
    }
  };

  const interrupt = async () => {
    if (interruptingRef.current) return;
    interruptingRef.current = true;
    setInterrupting(true);
    onError(null);
    try {
      await interruptQuickstartSession(sessionId, workspaceId);
      onEventsChanged();
    } catch (error) {
      onError(errorMessage(error));
    } finally {
      interruptingRef.current = false;
      setInterrupting(false);
    }
  };

  return (
    <div className="px-3 pb-3 pt-2" data-testid="session-message-composer">
      <InputGroup
        data-disabled={composerDisabled || sending ? 'true' : undefined}
        className={`${quickstartComposerFrameClassName} items-stretch gap-0 p-0 pl-0 shadow-none`}
      >
        <InputGroupTextarea
          value={draft}
          rows={2}
          aria-label={msg('managedAgents.sessions.detail.messageLabel', 'Message')}
          placeholder={
            disabled
              ? msg('managedAgents.sessions.detail.messageDisabled', 'This session can no longer receive messages.')
              : awaitingAction
                ? msg(
                    'managedAgents.sessions.detail.awaitingActionComposerPlaceholder',
                    'Please respond to the pending tool permission or questionnaire above...',
                  )
                : msg('managedAgents.sessions.detail.messagePlaceholder', 'Send a message to this session...')
          }
          className={`${quickstartComposerTextareaClassName} subtle-scrollbar-auto block max-h-40 min-h-[52px] overflow-y-auto px-4 pb-2 pt-3 text-[15px] leading-6`}
          disabled={composerDisabled || sending}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (
              event.key !== 'Enter' ||
              event.shiftKey ||
              event.repeat ||
              event.nativeEvent.isComposing ||
              composerDisabled ||
              sending
            ) {
              return;
            }
            event.preventDefault();
            void submit();
          }}
        />
        <InputGroupAddon align="block-end" className="justify-end px-3 pb-3 pt-0">
          <div className="flex shrink-0 items-center gap-1.5">
            {live ? (
              <InputGroupButton
                type="button"
                variant="ghost"
                size="icon-sm"
                className="rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
                aria-label={
                  interrupting
                    ? msg('managedAgents.sessions.detail.stopping', 'Stopping session')
                    : msg('managedAgents.sessions.detail.stop', 'Stop session')
                }
                disabled={interrupting}
                onClick={() => void interrupt()}
              >
                <Square className="size-3.5 fill-current" aria-hidden />
              </InputGroupButton>
            ) : null}
            <InputGroupButton
              type="button"
              variant="ghost"
              size="icon-sm"
              className={quickstartComposerSendButtonClassName}
              aria-label={
                sending
                  ? msg('managedAgents.sessions.detail.sending', 'Sending message')
                  : msg('managedAgents.sessions.detail.send', 'Send message')
              }
              disabled={composerDisabled || sending || !trimmedDraft}
              onClick={() => void submit()}
            >
              <ArrowUp className="size-4" aria-hidden />
            </InputGroupButton>
          </div>
        </InputGroupAddon>
      </InputGroup>
    </div>
  );
}

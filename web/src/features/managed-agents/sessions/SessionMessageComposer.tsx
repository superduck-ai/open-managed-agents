import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { ArrowUp, Square } from 'lucide-react';
import { useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { interruptQuickstartSession, postQuickstartSessionMessage } from '../api';
import { type QuickstartSessionEvent } from '../types';
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
    <form
      className="flex-none px-4 pb-3 pt-2"
      data-testid="session-message-composer"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="mx-auto w-full max-w-[720px]" data-testid="session-message-composer-column">
        <InputGroup
          data-disabled={composerDisabled || sending ? 'true' : undefined}
          className="min-h-14 items-end gap-0 overflow-hidden rounded-[22px] border-[0.5px] border-session-border bg-background p-0 shadow-none transition-[border-color,box-shadow,background-color] duration-150 hover:border-border/70 has-[[data-slot=input-group-control]:focus-visible]:border-border has-[[data-slot=input-group-control]:focus-visible]:ring-0 has-[[data-slot=input-group-control]:focus-visible]:shadow-[0_8px_24px_-22px_var(--foreground)] dark:bg-session-surface"
        >
          <InputGroupTextarea
            value={draft}
            rows={1}
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
            className="subtle-scrollbar-auto field-sizing-content block max-h-40 min-h-[54px] flex-1 resize-none overflow-y-auto border-0 bg-transparent px-4 py-[17px] text-sm leading-5 shadow-none outline-none placeholder:text-muted-foreground/75 focus-visible:ring-0"
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
          <InputGroupAddon align="inline-end" className="shrink-0 justify-end pb-[11px] pr-2.5 pt-0">
            <div className="flex shrink-0 items-center gap-1.5">
              {live ? (
                <InputGroupButton
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="size-7 rounded-md text-muted-foreground hover:bg-session-hover hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring/35"
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
                type="submit"
                variant="default"
                size="icon-sm"
                className="size-7 rounded-md shadow-none focus-visible:ring-1 focus-visible:ring-ring/40 disabled:bg-muted disabled:text-muted-foreground disabled:opacity-100"
                aria-label={
                  sending
                    ? msg('managedAgents.sessions.detail.sending', 'Sending message')
                    : msg('managedAgents.sessions.detail.send', 'Send message')
                }
                disabled={composerDisabled || sending || !trimmedDraft}
              >
                <ArrowUp className="size-4" aria-hidden />
              </InputGroupButton>
            </div>
          </InputGroupAddon>
        </InputGroup>
      </div>
    </form>
  );
}

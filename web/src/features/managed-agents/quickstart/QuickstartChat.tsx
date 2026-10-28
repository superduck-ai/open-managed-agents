import { ArrowDown, ArrowUp, Square } from 'lucide-react';
import { cn } from '../../../shared/lib/utils';
import { useI18n } from '../../../shared/i18n';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Textarea } from '../../../shared/ui/textarea';
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '../../../shared/ui/message-scroller';
import { ManagedErrorAlert } from '../components/common';
import { errorMessage } from '../utils';
import { FollowSentSessionMessage } from '../sessions/FollowSentSessionMessage';
import { SessionDetailDeltaFramesContext } from '../sessions/sessionDetailData';
import { SessionTranscriptView } from '../sessions/SessionTranscriptView';
import { SessionRequiresActionCard } from '../sessions/SessionRequiresActionCard';
import { quickstartCopy } from './copy';
import type { useQuickstartConversation } from './useQuickstartConversation';

type QuickstartConversation = ReturnType<typeof useQuickstartConversation>;
const noThreadNames = new Map<string, string>();

export function QuickstartChat({
  conversation,
  workspaceID,
  className,
}: {
  conversation: QuickstartConversation;
  workspaceID: string;
  className?: string;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <div
      className={cn(
        'flex h-[380px] min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl border-[0.5px] border-border bg-card min-[860px]:h-full',
        className,
      )}
    >
      <div className="flex shrink-0 flex-wrap items-start justify-between gap-2 border-b-[0.5px] border-border px-4 py-3">
        <div>
          <h2 className="font-semibold">{text.test}</h2>
          <p className="mt-1 text-xs text-muted-foreground">{text.testHint}</p>
        </div>
        {conversation.sessionID && (
          <ButtonLink
            variant="ghost"
            size="sm"
            href={`/workspaces/${encodeURIComponent(workspaceID)}/sessions/${encodeURIComponent(conversation.sessionID)}`}
          >
            {text.viewSession}
          </ButtonLink>
        )}
      </div>
      <SessionDetailDeltaFramesContext.Provider value={conversation.data.deltaFrames}>
        <MessageScrollerProvider key={conversation.sessionID || 'new'} autoScroll defaultScrollPosition="end">
          <FollowSentSessionMessage version={conversation.sentVersion} />
          <MessageScroller className="min-h-0 flex-1">
            <MessageScrollerViewport className="px-4 py-3">
              <MessageScrollerContent className="space-y-3">
                {!conversation.entries.length && (
                  <div className="flex min-h-28 items-center justify-center text-sm text-muted-foreground">
                    {conversation.sending || conversation.data.loading ? text.sending : text.firstMessage}
                  </div>
                )}
                <SessionTranscriptView
                  entries={conversation.entries}
                  selectedEntryId={null}
                  onSelectEntry={() => {}}
                  threadNameById={noThreadNames}
                  onThreadClick={() => {}}
                  openModelRequest={conversation.openModelRequest}
                />
                {conversation.action && (
                  <SessionRequiresActionCard
                    toolCall={conversation.action}
                    onConfirm={conversation.confirm}
                    disabled={conversation.sending || conversation.unconfirmed}
                  />
                )}
                <QuickstartConversationAlerts conversation={conversation} />
              </MessageScrollerContent>
            </MessageScrollerViewport>
            <MessageScrollerButton
              render={
                <Button
                  variant="outline"
                  size="icon-sm"
                  className="absolute right-4 bottom-3 rounded-full"
                  aria-label={text.received}
                />
              }
            >
              <ArrowDown />
            </MessageScrollerButton>
          </MessageScroller>
        </MessageScrollerProvider>
      </SessionDetailDeltaFramesContext.Provider>
      <QuickstartComposer conversation={conversation} />
    </div>
  );
}

function QuickstartComposer({ conversation }: { conversation: QuickstartConversation }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <form
      className="shrink-0 space-y-2 border-t-[0.5px] border-border p-3"
      onSubmit={(event) => {
        event.preventDefault();
        void conversation.send();
      }}
    >
      <Field>
        <FieldLabel htmlFor="quickstart-message" className="sr-only">
          {text.message}
        </FieldLabel>
        <Textarea
          id="quickstart-message"
          value={conversation.message}
          disabled={conversation.sending || conversation.disabled || conversation.unconfirmed}
          rows={2}
          className="max-h-28 min-h-14 resize-none"
          onChange={(event) => conversation.setMessage(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && !event.repeat) {
              event.preventDefault();
              void conversation.send();
            }
          }}
        />
      </Field>
      <div className="flex min-w-0 items-center justify-between gap-2">
        <p role="status" className="min-w-0 text-xs text-muted-foreground">
          {conversation.sending
            ? text.sending
            : conversation.action
              ? text.awaiting
              : conversation.failedTurn
                ? text.failedTurn
                : conversation.waiting
                  ? text.waiting
                  : conversation.complete
                    ? text.complete
                    : conversation.stopped
                      ? text.stopped
                      : conversation.running
                        ? text.running
                        : conversation.sessionID
                          ? text.saved
                          : text.firstMessage}
        </p>
        {conversation.running || conversation.action ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={conversation.sending}
            onClick={() => void conversation.stop()}
          >
            <Square />
            {text.stop}
          </Button>
        ) : (
          <Button
            type="submit"
            size="sm"
            disabled={
              !conversation.message.trim() || conversation.sending || conversation.disabled || conversation.unconfirmed
            }
          >
            <ArrowUp />
            {conversation.sending ? text.sending : text.send}
          </Button>
        )}
      </div>
    </form>
  );
}

function QuickstartConversationAlerts({ conversation }: { conversation: QuickstartConversation }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <>
      {(conversation.error || conversation.data.error || conversation.session.error) && (
        <ManagedErrorAlert>
          {conversation.error ?? conversation.data.error ?? errorMessage(conversation.session.error)}
        </ManagedErrorAlert>
      )}
      {(conversation.data.error || conversation.session.error) && (
        <Button variant="outline" size="sm" onClick={() => void conversation.refresh()}>
          {locale === 'zh-CN' ? '重试加载试跑' : 'Retry loading test'}
        </Button>
      )}
      {conversation.failedTurn && (
        <p role="status" className="text-sm text-destructive">
          {text.failedTurn}
        </p>
      )}
      {conversation.unconfirmed && !conversation.sending && (
        <div className="space-y-2">
          <p role="alert" className="text-sm text-muted-foreground">
            {text.unknownMessage}
          </p>
          <Button variant="outline" size="sm" onClick={() => void conversation.refresh()}>
            {text.refresh}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={conversation.data.loading || !conversation.data.connected}
            onClick={conversation.dismissUnconfirmed}
          >
            {text.reviewed}
          </Button>
        </div>
      )}
      {conversation.sessionID && conversation.session.data && !conversation.bindingValid && (
        <p role="alert" className="text-sm text-destructive">
          {text.restoreFailed}
        </p>
      )}
    </>
  );
}

import { createContext, type ReactNode, useContext } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Bubble, BubbleContent } from '../../../shared/ui/bubble';
import { Message, MessageAvatar, MessageContent, MessageHeader } from '../../../shared/ui/message';
import clsx from 'clsx';
import { AlertCircle, Loader2, Check } from 'lucide-react';
import Avatar from 'boring-avatars';
import { useAuth } from '../../../shared/auth/context';
import { Marker, MarkerContent, MarkerIcon } from '../../../shared/ui/marker';

const BOT_AVATAR_COLORS = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];
const USER_AVATAR_COLORS = ['var(--chart-5)', 'var(--chart-4)', 'var(--chart-3)', 'var(--chart-2)', 'var(--chart-1)'];

const QuickstartTurnGroupContext = createContext(false);

export function QuickstartTurnGroup({ continued, children }: { continued: boolean; children: ReactNode }) {
  return <QuickstartTurnGroupContext.Provider value={continued}>{children}</QuickstartTurnGroupContext.Provider>;
}

function useQuickstartTurnContinuation() {
  return useContext(QuickstartTurnGroupContext);
}

function TurnAvatar({ isUser = false }: { isUser?: boolean }) {
  const { account } = useAuth();
  const { msg } = useI18n();
  const label = isUser
    ? msg('managedAgents.quickstart.chat.youLabel', 'You')
    : msg('managedAgents.quickstart.chat.quickstartLabel', 'Quickstart');

  return (
    <MessageAvatar
      aria-label={label}
      className={clsx(
        'mt-5 size-8 shrink-0 self-start overflow-hidden rounded-full shadow-xs transition-transform duration-200 hover:scale-105',
        isUser ? 'border border-border' : 'border border-primary/10',
      )}
    >
      {isUser ? (
        <Avatar
          size={32}
          name={account?.email_address || account?.uuid || 'user'}
          variant="beam"
          colors={USER_AVATAR_COLORS}
        />
      ) : (
        <Avatar size={32} name="Quickstart-Bot" variant="marble" colors={BOT_AVATAR_COLORS} />
      )}
    </MessageAvatar>
  );
}

export function QuickstartTextTurn({ content, role }: { content: string; role: 'assistant' | 'user' }) {
  const { msg } = useI18n();
  const continued = useQuickstartTurnContinuation();
  const isUser = role === 'user';
  const label = isUser
    ? msg('managedAgents.quickstart.chat.youLabel', 'You')
    : msg('managedAgents.quickstart.chat.quickstartLabel', 'Quickstart');

  return (
    <Message align={isUser ? 'end' : 'start'} className={clsx(continued ? 'mt-1.5' : 'mt-6', 'items-start')}>
      {!continued && <TurnAvatar isUser={isUser} />}
      <MessageContent className={clsx('w-auto max-w-[78%] gap-1.5', continued && (isUser ? 'mr-10' : 'ml-10'))}>
        {!continued && <MessageHeader className={clsx('px-1 pb-0.5', isUser && 'justify-end')}>{label}</MessageHeader>}
        <Bubble align={isUser ? 'end' : 'start'} variant={isUser ? 'default' : 'outline'} className="max-w-full">
          <BubbleContent
            className={clsx(
              'border-border/80 px-4 py-3 text-[15px] leading-6 shadow-xs',
              isUser
                ? 'rounded-2xl rounded-tr-sm bg-primary text-primary-foreground'
                : 'rounded-2xl rounded-tl-sm bg-card',
            )}
          >
            {content}
          </BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  );
}

export function QuickstartAssistantTurn({
  children,
  className,
  contentClassName,
}: {
  children: ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  const { msg } = useI18n();
  const continued = useQuickstartTurnContinuation();
  return (
    <Message className={clsx(continued ? 'mt-1.5' : 'mt-6', 'items-start', className)}>
      {!continued && <TurnAvatar />}
      <MessageContent className={clsx('w-[calc(100%-2.5rem)] gap-1.5', continued && 'ml-10')}>
        {!continued && (
          <MessageHeader className="px-1 pb-0.5">
            {msg('managedAgents.quickstart.chat.quickstartLabel', 'Quickstart')}
          </MessageHeader>
        )}
        <Bubble variant="ghost" className="w-full max-w-full">
          <BubbleContent
            className={clsx(
              'w-full overflow-visible rounded-none border-none bg-transparent px-0 py-0',
              contentClassName,
            )}
          >
            {children}
          </BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  );
}

export function QuickstartStreamingTurn() {
  const { msg } = useI18n();
  return (
    <QuickstartAssistantTurn>
      <div
        role="status"
        className="inline-flex w-fit items-center gap-2 rounded-xl border border-border bg-muted/50 px-3 py-2 text-sm text-muted-foreground shadow-xs"
      >
        <Loader2 className="size-3.5 animate-spin" aria-hidden />
        <span>{msg('managedAgents.quickstart.thinking', 'Thinking')}</span>
      </div>
    </QuickstartAssistantTurn>
  );
}

export function QuickstartErrorTurn({ children }: { children: ReactNode }) {
  return (
    <QuickstartAssistantTurn>
      <div
        role="alert"
        className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
      >
        <AlertCircle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
        <span className="min-w-0 break-words">{children}</span>
      </div>
    </QuickstartAssistantTurn>
  );
}

export function StatusLine({
  children,
  className,
  tone = 'muted',
}: {
  children: ReactNode;
  className?: string;
  tone?: 'muted' | 'success' | 'error';
}) {
  return (
    <Marker
      className={clsx(
        'gap-2 text-sm',
        tone === 'error'
          ? 'text-destructive'
          : tone === 'success'
            ? 'text-muted-foreground'
            : 'text-muted-foreground/70',
        className,
      )}
    >
      <MarkerIcon>
        {tone === 'error' ? (
          <AlertCircle className="size-3.5" aria-hidden />
        ) : (
          <Check className="size-3.5" aria-hidden />
        )}
      </MarkerIcon>
      <MarkerContent>{children}</MarkerContent>
    </Marker>
  );
}

export function ToolRunningTurn({ message }: { message: string }) {
  return (
    <QuickstartAssistantTurn>
      <div className="flex items-center gap-2 text-sm text-muted-foreground/70">
        <Loader2 className="size-3.5 animate-spin" aria-hidden />
        <span>{message}</span>
      </div>
    </QuickstartAssistantTurn>
  );
}

export function ToolFailedTurn({ error, fallbackMessage }: { error?: string; fallbackMessage: string }) {
  return (
    <QuickstartAssistantTurn>
      <StatusLine tone="error">{error ?? fallbackMessage}</StatusLine>
    </QuickstartAssistantTurn>
  );
}

import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Badge } from '../../../shared/ui/badge';
import { Bubble, BubbleContent } from '../../../shared/ui/bubble';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Card, CardContent, CardDescription, CardTitle } from '../../../shared/ui/card';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { Input } from '../../../shared/ui/input';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { Label } from '../../../shared/ui/label';
import { Marker, MarkerContent, MarkerIcon } from '../../../shared/ui/marker';
import { Message, MessageAvatar, MessageContent, MessageHeader } from '../../../shared/ui/message';
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport
} from '../../../shared/ui/message-scroller';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import clsx from 'clsx';
import { AlertCircle, ArrowUp, ArrowUpRight, CalendarClock, Check, ChevronDown, ChevronLeft, ChevronRight, Cloud, Copy, FileText, Loader2, LockKeyhole, Pencil, Play, Plus, Search, Sparkles, Terminal, TriangleAlert, UserRound } from 'lucide-react';
import { type Dispatch, type KeyboardEvent as ReactKeyboardEvent, type ReactNode, type RefObject, type SetStateAction, useEffect, useMemo, useRef, useState } from 'react';
import { codeForTemplate, createDialogAgentConfig, displayAgentConfig, yamlStringify } from '../agentConfig';
import { quickstartComposerFrameClassName, quickstartComposerSendButtonClassName, quickstartComposerTextareaClassName } from '../components/composerStyles';
import { listSessionEvents, quickstartEnvironmentRequestBody, streamQuickstartSessionEvents } from '../api';
import { CopyButton, FormatSelect, MiniCodeBlock, NumberedCodeBlock, ScrollableCodeBlock, TemplateCard } from '../components/CodeBlocks';
import { templateTitle } from '../labels';
import { mergeSessionEvents, QuickstartSessionComposer, SessionTracePanel } from '../sessions/SessionDetailPage';
import { type AgentApiResponse, type AgentPanelTab, type AgentTemplate, type CodeFormat, type CreateAgentInput, type EnvironmentApiResponse, type HighlightLanguage, type I18nMsg, type IconComponent, type IntegrationSnippetLanguage, type QuickstartChatItem, type QuickstartQuestion, type QuickstartSessionEvent, type QuickstartToolCall, type QuickstartToolExecutionResult, type SessionApiResponse } from '../types';
import { copyText, errorMessage, managedEntityDetailHref, titleCase, toRecord } from '../utils';

export const quickstartEnvironmentSearchQuery =
  "An environment is a reusable template for the container where your agent's tools execute — things like networking policy and package access. Let me check what environments you already have.";

type PromptComposerSubmitShortcut = 'enter' | 'mod-enter';

export function InitialPromptPane({
  prompt,
  isCreating,
  onPromptChange,
  onSubmit
}: {
  prompt: string;
  isCreating: boolean;
  onPromptChange: (value: string) => void;
  onSubmit: () => void;
}) {
  const { msg } = useI18n();

  return (
    <div className="relative flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
      <div className="absolute inset-x-0 top-[39%] -translate-y-1/2 text-center">
        <h1 className="text-[19px] font-semibold leading-tight text-foreground">
          {msg('managedAgents.quickstart.initial.title', 'What do you want to build?')}
        </h1>
        <p className="mt-3 text-[15px] text-muted-foreground">
          {msg('managedAgents.quickstart.initial.description', 'Describe your agent or start with a template.')}
        </p>
      </div>

      <PromptComposer
        value={prompt}
        label={msg('managedAgents.quickstart.initial.inputLabel', 'Describe your agent')}
        placeholder={msg('managedAgents.quickstart.initial.placeholder', 'Describe your agent…')}
        isBusy={isCreating}
        submitShortcut="mod-enter"
        onChange={onPromptChange}
        onSubmit={onSubmit}
      />
    </div>
  );
}

export function QuickstartChatPane({
  agent,
  agentConfig,
  environment,
  session,
  chatItems,
  isStreaming,
  error,
  reply,
  onReplyChange,
  onSubmitReply,
  onCompleteTool,
  onCompleteEnvironmentTool,
  onConfirmVaultSelection,
  onOfferNextStep,
  onCreateAgentFromConfig,
  onAuthorizeCredential,
  onSkipCredential,
  onCreateSession,
  onSendTestRunMessage,
  onIntegrationExit,
  onStartEnvironmentStep,
  offerNextStepLabel,
  showCreateAgentNext
}: {
  agent: AgentApiResponse | null;
  agentConfig: CreateAgentInput | null;
  environment: EnvironmentApiResponse | null;
  session: SessionApiResponse | null;
  chatItems: QuickstartChatItem[];
  isStreaming: boolean;
  error: string | null;
  reply: string;
  onReplyChange: (value: string) => void;
  onSubmitReply: () => void;
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
  onCompleteEnvironmentTool: (call: QuickstartToolCall) => Promise<void>;
  onConfirmVaultSelection: (call: QuickstartToolCall) => Promise<void>;
  onOfferNextStep: (call: QuickstartToolCall) => Promise<void>;
  onCreateAgentFromConfig: (call: QuickstartToolCall) => Promise<void>;
  onAuthorizeCredential: (call: QuickstartToolCall, auth?: Record<string, unknown>, displayName?: string) => Promise<void>;
  onSkipCredential: (call: QuickstartToolCall) => Promise<void>;
  onCreateSession: (call: QuickstartToolCall) => Promise<void>;
  onSendTestRunMessage: (call: QuickstartToolCall, message: string) => Promise<void>;
  onIntegrationExit: (call: QuickstartToolCall, exit: 'scaffold' | 'go_to_agent') => Promise<void>;
  onStartEnvironmentStep: () => Promise<void>;
  offerNextStepLabel?: string;
  showCreateAgentNext: boolean;
}) {
  const { msg } = useI18n();
  const pinnedInteractionItem = [...chatItems].reverse().find(isPinnedQuickstartInteraction);
  const streamItems = pinnedInteractionItem ? chatItems.filter((item) => item.id !== pinnedInteractionItem.id) : chatItems;

  const renderToolCard = (item: Extract<QuickstartChatItem, { type: 'tool' }>, pinned = false) => (
    <QuickstartToolCard
      key={item.id}
      call={item.call}
      agent={agent}
      agentConfig={agentConfig}
      environment={environment}
      session={session}
      pinned={pinned}
      onCompleteTool={onCompleteTool}
      onCompleteEnvironmentTool={onCompleteEnvironmentTool}
      onConfirmVaultSelection={onConfirmVaultSelection}
      onOfferNextStep={onOfferNextStep}
      onCreateAgentFromConfig={onCreateAgentFromConfig}
      onAuthorizeCredential={onAuthorizeCredential}
      onSkipCredential={onSkipCredential}
      onCreateSession={onCreateSession}
      onSendTestRunMessage={onSendTestRunMessage}
      onIntegrationExit={onIntegrationExit}
      offerNextStepLabel={offerNextStepLabel}
    />
  );

  return (
    <div className="relative flex h-full min-w-0 flex-col overflow-hidden">
      <MessageScrollerProvider autoScroll defaultScrollPosition="end">
        <MessageScroller className="min-h-0 flex-1">
          <MessageScrollerViewport data-testid="quickstart-chat-stream" className="pb-6">
            <MessageScrollerContent data-testid="quickstart-chat-content" className="mt-8 w-full gap-0 px-4">
              {streamItems.map((item, index) => {
                if (item.type === 'message') {
                  return (
                    <MessageScrollerItem
                      key={item.id}
                      messageId={item.id}
                      scrollAnchor={!isStreaming && !error && index === streamItems.length - 1}
                    >
                      <QuickstartMessageBubble item={item} />
                    </MessageScrollerItem>
                  );
                }
                if (item.type === 'status') {
                  return (
                    <MessageScrollerItem
                      key={item.id}
                      messageId={item.id}
                      scrollAnchor={!isStreaming && !error && index === streamItems.length - 1}
                    >
                      <StatusLine className="mt-5" tone={item.tone}>{item.content}</StatusLine>
                    </MessageScrollerItem>
                  );
                }
                if (item.type === 'create_agent_result') {
                  return (
                    <MessageScrollerItem
                      key={item.id}
                      messageId={item.id}
                      scrollAnchor={!isStreaming && !error && index === streamItems.length - 1}
                    >
                      <CreateAgentResultCard
                        agentConfig={item.agentConfig}
                        isStreaming={isStreaming}
                        showNext={showCreateAgentNext}
                        onNext={onStartEnvironmentStep}
                      />
                    </MessageScrollerItem>
                  );
                }
                if (isHiddenQuickstartTool(item.call.name)) {
                  return null;
                }
                return (
                  <MessageScrollerItem
                    key={item.id}
                    messageId={item.id}
                    scrollAnchor={!isStreaming && !error && index === streamItems.length - 1}
                  >
                    {renderToolCard(item)}
                  </MessageScrollerItem>
                );
              })}

              {isStreaming ? (
                <MessageScrollerItem key="quickstart-streaming" messageId="quickstart-streaming" scrollAnchor>
                  <Marker className="mt-5 text-muted-foreground/70">
                    <MarkerIcon>
                      <Loader2 className="size-3.5 animate-spin" aria-hidden />
                    </MarkerIcon>
                    <MarkerContent>{msg('managedAgents.quickstart.thinking', 'Thinking')}</MarkerContent>
                  </Marker>
                </MessageScrollerItem>
              ) : null}

              {error ? (
                <MessageScrollerItem key="quickstart-error" messageId="quickstart-error" scrollAnchor>
                  <Marker className="mt-5 text-destructive">
                    <MarkerIcon>
                      <AlertCircle className="size-4" aria-hidden />
                    </MarkerIcon>
                    <MarkerContent>{error}</MarkerContent>
                  </Marker>
                </MessageScrollerItem>
              ) : null}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>

      {pinnedInteractionItem && !isHiddenQuickstartTool(pinnedInteractionItem.call.name) ? (
        <div data-testid="quickstart-pinned-interaction" className="shrink-0 px-3 pb-3">
          {renderToolCard(pinnedInteractionItem, true)}
        </div>
      ) : null}

      <PromptComposer
        value={reply}
        label={msg('managedAgents.quickstart.reply.label', 'Reply…')}
        placeholder={msg('managedAgents.quickstart.reply.placeholder', 'Reply…')}
        isBusy={isStreaming}
        submitShortcut="enter"
        onChange={onReplyChange}
        onSubmit={onSubmitReply}
        formClassName="shrink-0 p-3 pt-0"
      />
    </div>
  );
}

export function QuickstartMessageBubble({ item }: { item: Extract<QuickstartChatItem, { type: 'message' }> }) {
  const { msg } = useI18n();
  const content = item.role === 'assistant' ? cleanQuickstartAssistantText(item.content) : item.content;
  if (!content.trim()) {
    return null;
  }
  const isUser = item.role === 'user';
  const label = isUser
    ? msg('managedAgents.quickstart.chat.youLabel', 'You')
    : msg('managedAgents.quickstart.chat.quickstartLabel', 'Quickstart');
  const avatar = isUser ? (
    <UserRound className="size-3.5" aria-hidden />
  ) : (
    <Sparkles className="size-3.5" aria-hidden />
  );
  if (item.role === 'user') {
    return (
      <Message align="end" className="mt-5">
        <MessageAvatar className="size-7 border border-border/70 bg-secondary text-secondary-foreground shadow-sm">
          {avatar}
        </MessageAvatar>
        <MessageContent className="gap-1.5">
          <MessageHeader className="justify-end pb-0.5">{label}</MessageHeader>
          <Bubble align="end" variant="secondary" className="max-w-[85%]">
            <BubbleContent className="text-[15px] leading-6 text-foreground">{content}</BubbleContent>
          </Bubble>
        </MessageContent>
      </Message>
    );
  }
  return (
    <Message className="mt-5">
      <MessageAvatar className="size-7 border border-primary/15 bg-primary/10 text-primary shadow-sm">
        {avatar}
      </MessageAvatar>
      <MessageContent className="gap-1.5">
        <MessageHeader className="pb-0.5">{label}</MessageHeader>
        <Bubble variant="outline" className="max-w-[85%]">
          <BubbleContent className="text-[15px] leading-6 text-foreground">{content}</BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  );
}

function QuickstartAssistantTurn({
  children,
  className,
  contentClassName
}: {
  children: ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  const { msg } = useI18n();
  return (
    <Message className={clsx('mt-5', className)}>
      <MessageAvatar className="size-7 border border-primary/15 bg-primary/10 text-primary shadow-sm">
        <Sparkles className="size-3.5" aria-hidden />
      </MessageAvatar>
      <MessageContent className="gap-1.5">
        <MessageHeader className="pb-0.5">
          {msg('managedAgents.quickstart.chat.quickstartLabel', 'Quickstart')}
        </MessageHeader>
        <Bubble variant="ghost" className="w-full max-w-full">
          <BubbleContent className={clsx('w-full overflow-visible rounded-none border-none bg-transparent px-0 py-0', contentClassName)}>
            {children}
          </BubbleContent>
        </Bubble>
      </MessageContent>
    </Message>
  );
}

export function isPinnedQuickstartInteraction(item: QuickstartChatItem): item is Extract<QuickstartChatItem, { type: 'tool' }> {
  return item.type === 'tool' && item.call.name === 'ask_user_questions' && item.call.status === 'awaiting_user';
}

export function cleanQuickstartAssistantText(content: string) {
  return stripQuickstartInternalNarration(stripQuickstartThinking(content))
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

export function stripQuickstartThinking(content: string) {
  return content
    .replace(/<think\b[^>]*>[\s\S]*?<\/think>/gi, '')
    .replace(/<think\b[^>]*>[\s\S]*$/gi, '')
    .replace(/<\/think>/gi, '');
}

export function stripQuickstartInternalNarration(content: string) {
  const trimmed = content.trim();
  if (!trimmed) {
    return '';
  }
  const searchResult = normalizeQuickstartSearchResultText(trimmed);
  if (searchResult !== null) {
    return searchResult;
  }
  return stripQuickstartInternalSentences(trimmed);
}

export function normalizeQuickstartSearchResultText(content: string) {
  const prefixMatch = /^(?:(?:Search results for query:\s*)+)/i.exec(content);
  if (!prefixMatch) {
    return null;
  }
  const query = content.slice(prefixMatch[0].length).trim();
  return query ? `Search results for query: ${query}` : '';
}

export function assistantTextWithQuickstartToolContext(content: string, call: QuickstartToolCall) {
  const trimmed = content.trimEnd();
  if (call.name === 'list_environments' && /Search results for query:\s*$/i.test(trimmed)) {
    return `${trimmed} ${quickstartEnvironmentSearchQuery}`;
  }
  if (call.name !== 'web_search') {
    return content;
  }
  const query = quickstartWebSearchQuery(call.input);
  if (!query) {
    return content;
  }
  if (!trimmed) {
    return `Search results for query: ${query}`;
  }
  if (/Search results for query:\s*$/i.test(trimmed)) {
    return `${trimmed} ${query}`;
  }
  return content;
}

export function quickstartWebSearchQuery(input: Record<string, unknown>) {
  return typeof input.query === 'string' ? input.query.trim() : '';
}

export function stripQuickstartInternalSentences(content: string) {
  const trimmed = content.trim();
  if (!trimmed) {
    return '';
  }
  return trimmed
    .replace(
      /(?:^|[.!?]\s+|\n+)(?:Thinking\b|The user\b|User (?:wants|chose|selected|asked|said|is|has)\b|I see\b|I (?:need|should|will|can)\b|I'll\b|Let me\b|Creating\b|Now\b)[\s\S]*$/i,
      ''
    )
    .trim();
}

export function StatusLine({
  children,
  className,
  tone = 'muted'
}: {
  children: ReactNode;
  className?: string;
  tone?: 'muted' | 'success' | 'error';
}) {
  return (
    <Marker
      className={clsx(
        'gap-2 text-sm',
        tone === 'error' ? 'text-destructive' : tone === 'success' ? 'text-muted-foreground' : 'text-muted-foreground/70',
        className
      )}
    >
      <MarkerIcon>
        {tone === 'error' ? <AlertCircle className="size-3.5" aria-hidden /> : <Check className="size-3.5" aria-hidden />}
      </MarkerIcon>
      <MarkerContent>{children}</MarkerContent>
    </Marker>
  );
}

export function QuickstartToolCard({
  call,
  agent,
  agentConfig,
  environment,
  session,
  onCompleteTool,
  onCompleteEnvironmentTool,
  onConfirmVaultSelection,
  onOfferNextStep,
  onCreateAgentFromConfig,
  onAuthorizeCredential,
  onSkipCredential,
  onCreateSession,
  onSendTestRunMessage,
  onIntegrationExit,
  offerNextStepLabel,
  pinned = false
}: {
  call: QuickstartToolCall;
  agent: AgentApiResponse | null;
  agentConfig: CreateAgentInput | null;
  environment: EnvironmentApiResponse | null;
  session: SessionApiResponse | null;
  pinned?: boolean;
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
  onCompleteEnvironmentTool: (call: QuickstartToolCall) => Promise<void>;
  onConfirmVaultSelection: (call: QuickstartToolCall) => Promise<void>;
  onOfferNextStep: (call: QuickstartToolCall) => Promise<void>;
  onCreateAgentFromConfig: (call: QuickstartToolCall) => Promise<void>;
  onAuthorizeCredential: (call: QuickstartToolCall, auth?: Record<string, unknown>, displayName?: string) => Promise<void>;
  onSkipCredential: (call: QuickstartToolCall) => Promise<void>;
  onCreateSession: (call: QuickstartToolCall) => Promise<void>;
  onSendTestRunMessage: (call: QuickstartToolCall, message: string) => Promise<void>;
  onIntegrationExit: (call: QuickstartToolCall, exit: 'scaffold' | 'go_to_agent') => Promise<void>;
  offerNextStepLabel?: string;
}) {
  if (call.name === 'ask_user_questions') {
    return <AskUserQuestionsCard call={call} pinned={pinned} onCompleteTool={onCompleteTool} />;
  }
  if (call.name === 'build_agent_config') {
    return (
      <BuildAgentConfigCard
        call={call}
        agent={agent}
        onCompleteTool={onCompleteTool}
        onCreateAgent={onCreateAgentFromConfig}
      />
    );
  }
  if (call.name === 'list_environments' || call.name === 'list_vaults') {
    return <QuickstartStatusToolLine call={call} />;
  }
  if (call.name === 'agent_ready') {
    return (
      <AgentReadyCard
        call={call}
        agent={agent}
        environment={environment}
        session={session}
        onCreateSession={onCreateSession}
        onCompleteTool={onCompleteTool}
      />
    );
  }
  if (call.name === 'await_test_run') {
    return <AwaitTestRunCard call={call} onSendTestRunMessage={onSendTestRunMessage} />;
  }
  if (call.name === 'create_environment') {
    return <EnvironmentStepCard call={call} onNext={onCompleteEnvironmentTool} />;
  }
  if (call.name === 'offer_next_step') {
    return <OfferNextStepCard call={call} labelOverride={offerNextStepLabel} onNext={onOfferNextStep} />;
  }
  if (call.name === 'select_vault') {
    return <SelectVaultAckCard call={call} onConfirm={onConfirmVaultSelection} />;
  }
  if (call.name === 'vault_sharing_notice') {
    return <VaultSharingNoticeCard call={call} />;
  }
  if (call.name === 'create_vault') {
    return <CreateVaultResultCard call={call} />;
  }
  if (call.name === 'create_vault_credential') {
    return <VaultCredentialCard call={call} agentConfig={agentConfig} onAuthorize={onAuthorizeCredential} onSkip={onSkipCredential} />;
  }
  if (call.name === 'create_deployment') {
    return <CreateDeploymentResultCard call={call} />;
  }
  if (call.name === 'show_integration_exits') {
    return (
      <IntegrationExitsCard
        call={call}
        agent={agent}
        environment={environment}
        onSelectExit={onIntegrationExit}
      />
    );
  }
  return <GenericQuickstartToolCard call={call} />;
}

export function isHiddenQuickstartTool(name: string) {
  return name === 'flag_schedule_intent';
}

export function QuickstartStatusToolLine({ call }: { call: QuickstartToolCall }) {
  const { msg } = useI18n();
  const fallback = call.name === 'list_vaults'
    ? msg('managedAgents.quickstart.vaultsLoaded', 'Vaults loaded')
    : msg('managedAgents.quickstart.environmentsLoaded', 'Environments loaded');
  const text = (call.error || call.result || fallback).replace(/\.$/, '');
  const tone = call.status === 'failed' ? 'error' : 'muted';

  if (call.status === 'running') {
    return (
      <div className="mt-2 flex h-6 items-center gap-3 text-sm leading-5 text-muted-foreground/70">
        <Loader2 className="size-3.5 shrink-0 animate-spin" aria-hidden />
        <span className="truncate">
          {call.name === 'list_vaults'
            ? msg('managedAgents.credentialVaults.loading', 'Loading vaults')
            : msg('managedAgents.environments.loading', 'Loading environments')}
        </span>
      </div>
    );
  }

  return (
    <StatusLine className="mt-2 h-6 text-muted-foreground/70" tone={tone}>
      <span className="truncate">{text}</span>
    </StatusLine>
  );
}

function QuickstartWarningAlert({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <Alert className={clsx('border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400', className)}>
      <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
      <AlertDescription className="text-inherit">{children}</AlertDescription>
    </Alert>
  );
}

export function VaultSharingNoticeCard({ call: _call }: { call: QuickstartToolCall }) {
  const { msg } = useI18n();
  return (
    <QuickstartWarningAlert className="my-2">
      <p>
        {msg('managedAgents.quickstart.vaultSharingNotice', 'Vaults are shared across this workspace. Credentials added to a vault will be usable by anyone with API key access. Learn more')}{' '}
        <a
          className="underline underline-offset-2 hover:text-amber-600 dark:text-amber-400"
          href="/docs/en/managed-agents/vaults"
          target="_blank"
          rel="noreferrer"
        >
          {msg('managedAgents.quickstart.here', 'here')}<span className="sr-only">{msg('managedAgents.common.opensInNewTabParen', '(opens in new tab)')}</span>
        </a>
        .
      </p>
    </QuickstartWarningAlert>
  );
}

export function SelectVaultAckCard({ call, onConfirm }: { call: QuickstartToolCall; onConfirm: (call: QuickstartToolCall) => Promise<void> }) {
  const { msg } = useI18n();
  const [acknowledged, setAcknowledged] = useState(false);
  const vaultNames = quickstartVaultLabelsFromInput(call.input);
  const label = vaultNames.length ? vaultNames.join(', ') : msg('managedAgents.quickstart.noVaultSelected', 'No vault selected');
  const ackLabel = msg('managedAgents.quickstart.vaultAck', 'I own or am authorized to use this vault. I understand this means this agent can assume the identity granted by this vault.');

  if (call.status !== 'awaiting_user') {
    return (
      <div className="mt-5">
        <ToolStatus call={call} />
      </div>
    );
  }

  return (
    <div className="mt-5 flex w-full flex-col gap-3 rounded-xl bg-secondary/80 p-4 shadow-sm">
      <p className="text-sm text-foreground">
        {msg('managedAgents.quickstart.selected', 'Selected')}: <span className="font-semibold text-foreground">{label}</span>
      </p>
      <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-3">
        <Label className="cursor-pointer items-start gap-3 text-sm leading-5 font-normal text-amber-600 dark:text-amber-400">
          <Checkbox
            aria-label={ackLabel}
            className="mt-0.5 border-amber-500/40 data-checked:border-amber-500/40 data-checked:bg-amber-500/10 data-checked:text-amber-600 dark:text-amber-400"
            checked={acknowledged}
            onCheckedChange={(checked) => setAcknowledged(checked === true)}
          />
          <span>{ackLabel}</span>
        </Label>
      </div>
      <div className="flex justify-end">
        <Button
          type="button"
          size="sm"
          className="disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground/70"
          disabled={!acknowledged}
          onClick={() => onConfirm(call)}
        >
          {msg('common.confirm', 'Confirm')}
        </Button>
      </div>
    </div>
  );
}

export function VaultCredentialCard({
  call,
  agentConfig,
  onAuthorize,
  onSkip
}: {
  call: QuickstartToolCall;
  agentConfig: CreateAgentInput | null;
  onAuthorize: (call: QuickstartToolCall, auth?: Record<string, unknown>, displayName?: string) => Promise<void>;
  onSkip: (call: QuickstartToolCall) => Promise<void>;
}) {
  const { msg } = useI18n();
  const serverSlug = typeof call.input.mcp_server_name === 'string' ? call.input.mcp_server_name : 'MCP server';
  const serverName = titleCase(serverSlug);
  const reason = typeof call.input.reason === 'string' ? call.input.reason : msg('managedAgents.quickstart.authorizeServerReason', 'Authorize this server so the test session can use it.');
  const serverUrl = quickstartMcpServerUrl(agentConfig, serverSlug);
  const [accessToken, setAccessToken] = useState('');
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
  const [accessTokenOpen, setAccessTokenOpen] = useState(false);
  const [oauthOpen, setOauthOpen] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const displayName = `${serverName} credential`;
  const busy = call.status === 'running';
  const credentialAckLabel = msg('managedAgents.quickstart.credentialAck', 'I acknowledge this credential is shared and that I am responsible for its storage and use.');

  if (call.status === 'completed' && call.result) {
    const compactResult = call.result.split(' — ')[0];
    return (
      <div className="mt-5 flex w-full items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm text-foreground">
        <Check className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <LockKeyhole className="size-3.5 shrink-0 text-muted-foreground/70" aria-hidden />
        <span className="min-w-0 flex-1 truncate">{compactResult}</span>
      </div>
    );
  }

  const credentialAuth = () => {
    if (!serverUrl) {
      return null;
    }
    const token = accessToken.trim();
    const oauthClientId = clientId.trim();
    const oauthClientSecret = clientSecret.trim();
    if (token) {
      return { type: 'static_bearer', mcp_server_url: serverUrl, token };
    }
    if (oauthClientId && oauthClientSecret) {
      return {
        type: 'mcp_oauth',
        mcp_server_url: serverUrl,
        client_id: oauthClientId,
        client_secret: oauthClientSecret
      };
    }
    return null;
  };

  const auth = credentialAuth();
  const canConnect = Boolean(auth && acknowledged && !busy);

  const submitCredential = () => {
    if (auth) {
      void onAuthorize(call, auth, displayName);
    }
  };

  return (
    <div className="mt-5 flex w-full flex-col gap-2 rounded-xl border border-border bg-card p-3 text-sm text-foreground">
      <p className="text-sm font-medium leading-5 text-foreground">
        {msg('managedAgents.quickstart.authorizationRequired', 'Authorization required to use this MCP')}
      </p>
      <div className="rounded-lg border border-border bg-secondary p-3">
        <div className="flex items-center gap-2">
          <div className="grid size-8 shrink-0 place-items-center rounded-md border border-border bg-accent text-foreground">
            <FileText className="size-4" aria-hidden />
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm leading-5 text-foreground">
              {msg('managedAgents.quickstart.addCredentialFor', 'Add credential for')} <span className="font-semibold text-foreground">{serverName}</span>
            </p>
            <p className="mt-0.5 text-xs leading-4 text-muted-foreground">{reason}</p>
          </div>
        </div>
      </div>
      <CredentialDisclosure
        title={msg('managedAgents.quickstart.accessToken', 'Access token')}
        open={accessTokenOpen}
        onOpenChange={setAccessTokenOpen}
      >
        <Input
          type="password"
          value={accessToken}
          placeholder={msg('managedAgents.quickstart.oauthAccessToken', 'OAuth access token')}
          className="h-7 border-transparent bg-secondary text-sm placeholder:text-muted-foreground/70 focus-visible:border-ring"
          onChange={(event) => setAccessToken(event.target.value)}
        />
      </CredentialDisclosure>
      <CredentialDisclosure
        title={msg('managedAgents.quickstart.oauthClientCredentials', 'OAuth client credentials')}
        open={oauthOpen}
        onOpenChange={setOauthOpen}
      >
        <Input
          type="text"
          value={clientId}
          placeholder={msg('managedAgents.quickstart.clientId', 'Client ID')}
          className="h-7 border-transparent bg-secondary text-sm placeholder:text-muted-foreground/70 focus-visible:border-ring"
          onChange={(event) => setClientId(event.target.value)}
        />
        <Input
          type="password"
          value={clientSecret}
          placeholder={msg('managedAgents.quickstart.clientSecret', 'Client secret')}
          className="mt-2 h-7 border-border bg-secondary text-sm placeholder:text-muted-foreground/70 focus-visible:border-ring"
          onChange={(event) => setClientSecret(event.target.value)}
        />
      </CredentialDisclosure>
      <QuickstartWarningAlert>
        <p>
          {msg('managedAgents.quickstart.credentialSharingNotice', 'This credential will be shared across this workspace. Anyone with API key access can use this credential in an agent session to access the service associated with the credential - including reading data and taking actions on behalf of the credential owner. Learn more')}{' '}
          <a className="underline underline-offset-2 hover:text-amber-600 dark:text-amber-400" href="/docs/en/managed-agents/vaults" target="_blank" rel="noreferrer">
            {msg('managedAgents.quickstart.here', 'here')}<span className="sr-only">{msg('managedAgents.common.opensInNewTabParen', '(opens in new tab)')}</span>
          </a>
        </p>
      </QuickstartWarningAlert>
      <Label className="cursor-pointer items-start gap-2 text-sm leading-5 font-normal text-foreground">
        <Checkbox
          aria-label={credentialAckLabel}
          className="mt-0.5 size-5 rounded border-border"
          checked={acknowledged}
          onCheckedChange={(checked) => setAcknowledged(checked === true)}
        />
        <span>{credentialAckLabel}</span>
      </Label>
      {call.error ? <p className="text-sm text-destructive">{call.error}</p> : null}
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          aria-label={msg('managedAgents.quickstart.authorizeCredential', 'Authorize {name} credential', { name: serverName })}
          size="sm"
          className="disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground/70"
          disabled={!canConnect}
          onClick={submitCredential}
        >
          {busy ? msg('managedAgents.quickstart.connecting', 'Connecting') : msg('managedAgents.quickstart.connect', 'Connect')}
        </Button>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="disabled:cursor-wait"
          disabled={busy}
          onClick={() => void onSkip(call)}
        >
          {msg('managedAgents.quickstart.skipForNow', 'Skip for now')}
        </Button>
      </div>
    </div>
  );
}

export function CredentialDisclosure({
  title,
  open,
  onOpenChange,
  children
}: {
  title: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  return (
    <Collapsible
      open={open}
      onOpenChange={(nextOpen) => onOpenChange(nextOpen)}
      className="overflow-hidden rounded-lg border border-border bg-card"
    >
      <CollapsibleTrigger
        type="button"
        className="flex h-[46px] w-full items-center justify-start gap-2 rounded-lg px-3 text-left text-sm leading-5 text-muted-foreground transition-colors hover:bg-transparent hover:text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none motion-reduce:transition-none"
      >
        <ChevronDown className={clsx('size-4 shrink-0 transition-transform duration-200 ease-snappy-out motion-reduce:transition-none', open ? '' : '-rotate-90')} aria-hidden />
        <span className="font-medium text-foreground">{title}</span>
        <Badge variant="secondary" className="h-5 text-[11px] font-medium text-muted-foreground">
          {msg('managedAgents.common.optional', 'Optional')}
        </Badge>
        <AlertCircle className="ml-auto size-3.5 text-muted-foreground/70" aria-hidden />
      </CollapsibleTrigger>
      <CollapsibleContent className="border-t border-border px-3 pb-3 pt-1">
        {children}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function EnvironmentStepCard({ call, onNext }: { call: QuickstartToolCall; onNext: (call: QuickstartToolCall) => Promise<void> }) {
  const { msg } = useI18n();
  const isReuse = typeof call.input.reuse_environment_id === 'string' && call.input.reuse_environment_id.trim();
  if (call.status === 'running') {
    return (
      <QuickstartAssistantTurn>
        <div className="flex items-center gap-2 text-sm text-muted-foreground/70">
          <Loader2 className="size-3.5 animate-spin" aria-hidden />
          {isReuse ? msg('managedAgents.quickstart.selectingEnvironment', 'Selecting environment') : msg('managedAgents.quickstart.creatingEnvironment', 'Creating environment')}
        </div>
      </QuickstartAssistantTurn>
    );
  }
  if (call.status === 'failed') {
    return (
      <QuickstartAssistantTurn>
        <StatusLine tone="error">{call.error ?? msg('managedAgents.quickstart.environmentSetupFailed', 'Environment setup failed.')}</StatusLine>
      </QuickstartAssistantTurn>
    );
  }
  const status = isReuse ? msg('managedAgents.quickstart.environmentSelected', 'Environment selected') : msg('managedAgents.quickstart.environmentCreated', 'Environment created');
  return (
    <QuickstartAssistantTurn>
      {isReuse ? (
        <StatusLine>{status}</StatusLine>
      ) : (
        <>
          <StatusLine tone="success">{status}</StatusLine>
          <QuickstartApiCallCard
            method="POST"
            path="/v1/environments"
            code={shellYamlCommand('environments', quickstartEnvironmentRequestBody(call.input))}
            maxLines={10}
          />
        </>
      )}
      {call.status === 'awaiting_user' ? (
        <Button
          type="button"
          className="mt-5"
          onClick={() => onNext(call)}
        >
          Next: Start session
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      ) : null}
    </QuickstartAssistantTurn>
  );
}

export function AskUserQuestionsCard({
  call,
  pinned = false,
  onCompleteTool
}: {
  call: QuickstartToolCall;
  pinned?: boolean;
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
}) {
  const { msg } = useI18n();
  const questions = parseQuestionInput(call.input);
  const [questionIndex, setQuestionIndex] = useState(0);
  const [selected, setSelected] = useState<Record<number, string[]>>({});
  const [otherValues, setOtherValues] = useState<Record<number, string>>({});
  const activeQuestion = questions[questionIndex];
  const activeSelected = selected[questionIndex] ?? [];
  const submitted = call.status === 'completed';
  const immediateSingleChoice = Boolean(activeQuestion && !activeQuestion.multiSelect && questions.length === 1);
  const submittedAnswers = submitted ? parseSubmittedQuestionAnswers(call.result) : [];
  const activeSubmittedAnswer = submittedAnswers[questionIndex] ?? submittedAnswers[0];

  const completeWithAnswers = async (answersByQuestion: Record<number, string[]>, otherAnswers: Record<number, string> = otherValues) => {
    const answers = questions.map((question, index) => {
      const labels = answersByQuestion[index] ?? [];
      const other = otherAnswers[index]?.trim();
      return {
        header: question.header,
        question: question.question,
        answers: other ? [...labels, other] : labels
      };
    });
    await onCompleteTool(call, { content: JSON.stringify({ answers }) });
  };

  const toggleOption = (label: string) => {
    if (submitted || !activeQuestion) {
      return;
    }
    if (immediateSingleChoice) {
      void completeWithAnswers({ [questionIndex]: [label] });
      return;
    }
    setSelected((current) => {
      const values = current[questionIndex] ?? [];
      if (!activeQuestion.multiSelect) {
        return { ...current, [questionIndex]: [label] };
      }
      return {
        ...current,
        [questionIndex]: values.includes(label) ? values.filter((value) => value !== label) : [...values, label]
      };
    });
  };

  const setMultiSelectOption = (label: string, nextChecked: boolean) => {
    if (submitted || !activeQuestion?.multiSelect) {
      return;
    }
    setSelected((current) => {
      const values = current[questionIndex] ?? [];
      const nextValues = nextChecked
        ? values.includes(label)
          ? values
          : [...values, label]
        : values.filter((value) => value !== label);
      return { ...current, [questionIndex]: nextValues };
    });
  };

  const submit = async () => {
    await completeWithAnswers(selected);
  };

  const questionControlName = `quickstart-question-${call.id}-${questionIndex}`;
  const questionGroupLabel = activeQuestion.question;
  const activeRadioValue = activeSelected[0] ?? '';

  if (!activeQuestion) {
    return <GenericQuickstartToolCard call={call} />;
  }

  return (
    <div
      data-testid="quickstart-question-card"
      className={clsx(
        'flex flex-col gap-3 rounded-lg border border-border bg-card p-3 shadow-sm outline-none',
        pinned ? 'mx-auto w-full' : 'mt-5'
      )}
    >
      <div className="flex items-center justify-between gap-3 px-2">
        <p className="text-[15px] font-medium text-foreground">{activeQuestion.question}</p>
        {questions.length > 1 ? (
          <div className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground/70">
            {questionIndex + 1}/{questions.length}
          </div>
        ) : null}
      </div>

      {submitted ? (
        <p className="px-2 text-sm text-muted-foreground">
          {activeSubmittedAnswer ? activeSubmittedAnswer.answers.join(', ') || msg('managedAgents.quickstart.skipped', 'Skipped') : call.result}
        </p>
      ) : (
        <>
          {activeQuestion.multiSelect ? (
            <div role="group" aria-label={questionGroupLabel} className="-my-1">
              {activeQuestion.options.map((option, index) => {
                const checked = activeSelected.includes(option.label);
                const optionId = `${questionControlName}-checkbox-${index}`;
                return (
                  <div key={option.label} className="relative">
                    {index > 0 ? <span className="absolute inset-x-2 top-0 h-px bg-secondary" /> : null}
                    <div
                      className={clsx(
                        'flex items-start gap-3 rounded-lg px-2 py-3 text-left hover:bg-accent',
                        checked && 'bg-accent'
                      )}
                    >
                      <Checkbox
                        id={optionId}
                        checked={checked}
                        onCheckedChange={(nextChecked) => setMultiSelectOption(option.label, nextChecked === true)}
                        className="mt-1 size-5 rounded-md border-border bg-accent text-primary"
                      />
                      <Label htmlFor={optionId} className="min-w-0 cursor-pointer items-start leading-6 font-normal">
                        <span className="block text-[15px] text-foreground">{option.label}</span>
                        <span className="mt-0.5 block text-sm text-muted-foreground">{option.description}</span>
                      </Label>
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <RadioGroup
              aria-label={questionGroupLabel}
              name={questionControlName}
              value={activeRadioValue}
              onValueChange={(value) => toggleOption(value)}
              className="-my-1 gap-0"
            >
              {activeQuestion.options.map((option, index) => {
                const checked = activeRadioValue === option.label;
                const optionId = `${questionControlName}-radio-${index}`;
                return (
                  <div key={option.label} className="relative">
                    {index > 0 ? <span className="absolute inset-x-2 top-0 h-px bg-secondary" /> : null}
                    <Label
                      htmlFor={optionId}
                      className={clsx(
                        'w-full cursor-pointer items-start gap-3 rounded-lg px-2 py-3 text-left leading-6 font-normal hover:bg-accent',
                        checked && 'bg-accent'
                      )}
                    >
                      <RadioGroupItem
                        id={optionId}
                        value={option.label}
                        className="mt-1 size-5 border-border bg-accent text-primary"
                      />
                      <span className="min-w-0">
                        <span className="block text-[15px] text-foreground">{option.label}</span>
                        <span className="mt-0.5 block text-sm text-muted-foreground">{option.description}</span>
                      </span>
                    </Label>
                  </div>
                );
              })}
            </RadioGroup>
          )}
          <div className="relative -my-1">
            <span className="absolute inset-x-2 top-0 h-px bg-secondary" />
            <Label
              htmlFor={`${questionControlName}-other`}
              className="w-full items-center gap-3 rounded-lg px-2 py-3 text-left leading-6 font-normal"
            >
              <span className="grid size-6 shrink-0 place-items-center rounded-md border border-border bg-accent text-muted-foreground">
                <Pencil className="size-3.5" aria-hidden />
              </span>
              <Input
                id={`${questionControlName}-other`}
                value={otherValues[questionIndex] ?? ''}
                placeholder={msg('managedAgents.quickstart.somethingElse', 'Something else')}
                className="h-auto rounded-none border-none bg-transparent p-0 text-[15px] placeholder:text-muted-foreground/70 focus-visible:ring-0"
                onChange={(event) => setOtherValues((current) => ({ ...current, [questionIndex]: event.target.value }))}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && immediateSingleChoice) {
                    event.preventDefault();
                    const value = event.currentTarget.value.trim();
                    if (value) {
                      void completeWithAnswers({ [questionIndex]: [] }, { ...otherValues, [questionIndex]: value });
                    }
                  }
                }}
              />
            </Label>
          </div>
          <div className="flex items-center gap-2 px-2">
            <div className="flex items-center gap-2">
              {questions.length > 1 ? (
                <>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="text-foreground hover:bg-accent"
                    disabled={questionIndex === 0}
                    onClick={() => setQuestionIndex((index) => Math.max(0, index - 1))}
                  >
                    <ChevronLeft className="size-4" aria-hidden />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="text-foreground hover:bg-accent"
                    disabled={questionIndex === questions.length - 1}
                    onClick={() => setQuestionIndex((index) => Math.min(questions.length - 1, index + 1))}
                  >
                    <ChevronRight className="size-4" aria-hidden />
                  </Button>
                </>
              ) : null}
              {!immediateSingleChoice ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={msg('managedAgents.quickstart.submitAnswer', 'Submit answer')}
                  className="bg-primary text-primary-foreground hover:bg-primary/80"
                  onClick={submit}
                >
                  <ArrowUp className="size-4" aria-hidden />
                </Button>
              ) : null}
            </div>
            <Button
              type="button"
              variant="secondary"
              className="ml-auto hover:bg-popover"
              onClick={() => onCompleteTool(call, { content: 'Skipped.' })}
            >
              {msg('managedAgents.quickstart.skip', 'Skip')}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

export function BuildAgentConfigCard({
  call,
  agent,
  onCompleteTool,
  onCreateAgent
}: {
  call: QuickstartToolCall;
  agent: AgentApiResponse | null;
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
  onCreateAgent: (call: QuickstartToolCall) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [isCreating, setIsCreating] = useState(false);
  const createButtonLabel = agent
    ? msg('managedAgents.quickstart.useThisConfig', 'Use this config')
    : msg('managedAgents.quickstart.createThisAgent', 'Create this agent');
  if (call.status === 'running') {
    return (
      <QuickstartAssistantTurn>
        <div className="flex items-center gap-2 text-sm text-muted-foreground/70">
          <Loader2 className="size-3.5 animate-spin" aria-hidden />
          {msg('managedAgents.quickstart.updatingAgent', 'Updating agent...')}
        </div>
      </QuickstartAssistantTurn>
    );
  }
  if (call.status === 'failed') {
    return (
      <QuickstartAssistantTurn>
        <StatusLine tone="error">{call.error ?? msg('managedAgents.quickstart.agentCreationFailed', 'Agent creation failed.')}</StatusLine>
      </QuickstartAssistantTurn>
    );
  }
  if (call.status !== 'awaiting_user') {
    return null;
  }

  return (
    <QuickstartAssistantTurn>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          className="disabled:cursor-wait disabled:opacity-80"
          disabled={isCreating}
          onClick={async () => {
            if (agent) {
              await onCompleteTool(call, { content: 'Agent created.' });
              return;
            }
            setIsCreating(true);
            try {
              await onCreateAgent(call);
            } finally {
              setIsCreating(false);
            }
          }}
        >
          {isCreating ? msg('common.creating', 'Creating...') : createButtonLabel}
        </Button>
        <Button
          type="button"
          variant="secondary"
          className="disabled:cursor-wait disabled:opacity-70"
          disabled={isCreating}
          onClick={() => onCompleteTool(call, { content: "I'd like to keep refining the config before creating." })}
        >
          {msg('managedAgents.quickstart.keepRefining', 'Keep refining')}
        </Button>
      </div>
    </QuickstartAssistantTurn>
  );
}

export function CreateAgentResultCard({
  agentConfig,
  isStreaming,
  showNext,
  onNext
}: {
  agentConfig: CreateAgentInput;
  isStreaming: boolean;
  showNext: boolean;
  onNext: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const cli = shellYamlCommand('agents', displayAgentConfig(agentConfig));
  return (
    <QuickstartAssistantTurn>
      <StatusLine tone="success">{msg('managedAgents.quickstart.agentCreated', 'Agent created')}</StatusLine>
      <p className="mt-5 text-[15px] leading-6 text-foreground">
        {msg('managedAgents.quickstart.agentCreatedBody', 'Your agent is created. Here’s the call that made it:')}
      </p>
      <QuickstartApiCallCard method="POST" path="/v1/agents" code={cli} maxLines={12} />
      {showNext ? (
        <Button
          type="button"
          disabled={isStreaming}
          size="lg"
          className="mt-4 text-[15px] disabled:cursor-not-allowed disabled:opacity-70"
          onClick={() => void onNext()}
        >
          {msg('managedAgents.quickstart.next.configureEnvironment', 'Next: Configure environment')}
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      ) : null}
    </QuickstartAssistantTurn>
  );
}

export function QuickstartApiCallCard({ method, path, code, maxLines }: { method: string; path: string; code: string; maxLines: number }) {
  const { msg } = useI18n();
  return (
    <div className="mt-3 overflow-hidden rounded-lg border border-border bg-popover shadow-sm">
      <div className="flex h-11 items-center gap-2 border-b border-border px-3">
        <span className="rounded bg-secondary px-2 py-0.5 text-[11px] font-semibold uppercase tracking-[0.02em] text-secondary-foreground">
          {method}
        </span>
        <span className="font-mono text-[13px] text-foreground">{path}</span>
        <div className="ml-auto flex items-center gap-1">
          <Badge
            variant="outline"
            className="h-7 rounded-md px-2 text-[11px] font-semibold uppercase tracking-[0.02em] text-muted-foreground"
          >
            CLI
          </Badge>
          <CopyButton value={code} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
        </div>
      </div>
      <MiniCodeBlock code={code} maxLines={maxLines} />
    </div>
  );
}

export function shellYamlCommand(resource: string, body: unknown) {
  return `ant beta:${resource} create <<YAML\n${yamlStringify(body)}\nYAML`;
}

export function CreateVaultResultCard({ call }: { call: QuickstartToolCall }) {
  const { msg } = useI18n();
  if (call.status === 'running') {
    return (
      <QuickstartAssistantTurn>
        <div className="flex items-center gap-2 text-sm text-muted-foreground/70">
          <Loader2 className="size-3.5 animate-spin" aria-hidden />
          {msg('managedAgents.quickstart.creatingVault', 'Creating vault')}
        </div>
      </QuickstartAssistantTurn>
    );
  }
  if (call.status === 'failed') {
    return (
      <QuickstartAssistantTurn>
        <StatusLine tone="error">{call.error ?? msg('managedAgents.quickstart.vaultCreationFailed', 'Vault creation failed.')}</StatusLine>
      </QuickstartAssistantTurn>
    );
  }
  const displayName =
    typeof call.input.display_name === 'string' && call.input.display_name.trim()
      ? call.input.display_name.trim()
      : typeof call.input.name === 'string' && call.input.name.trim()
        ? call.input.name.trim()
        : 'Quickstart vault';
  return (
    <QuickstartAssistantTurn>
      <StatusLine tone="success">{msg('managedAgents.quickstart.vaultCreated', 'Vault created')}</StatusLine>
      <QuickstartApiCallCard method="POST" path="/v1/vaults" code={shellYamlCommand('vaults', { display_name: displayName })} maxLines={6} />
    </QuickstartAssistantTurn>
  );
}

export function AgentReadyCard({
  call,
  agent,
  environment,
  session,
  onCreateSession,
  onCompleteTool
}: {
  call: QuickstartToolCall;
  agent: AgentApiResponse | null;
  environment: EnvironmentApiResponse | null;
  session: SessionApiResponse | null;
  onCreateSession: (call: QuickstartToolCall) => Promise<void>;
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
}) {
  const { msg } = useI18n();
  if (call.status === 'completed' && session) {
    const cli = shellYamlCommand('sessions', {
      environment_id: environment?.id ?? session.environment_id,
      agent: {
        type: 'agent',
        id: agent?.id ?? 'agent_id'
      }
    });
    return (
      <QuickstartAssistantTurn>
        <StatusLine tone="success">{msg('managedAgents.quickstart.sessionCreated', 'Session created')}</StatusLine>
        <QuickstartApiCallCard method="POST" path="/v1/sessions" code={cli} maxLines={8} />
      </QuickstartAssistantTurn>
    );
  }
  return (
    <QuickstartAssistantTurn>
      {call.status === 'awaiting_user' ? (
        <div className="flex items-center gap-2">
          <Button
            type="button"
            onClick={() => onCreateSession(call)}
          >
            <Play className="size-3.5 fill-current" aria-hidden />
            {msg('managedAgents.quickstart.testRun', 'Test run')}
          </Button>
          <Button
            type="button"
            variant="secondary"
            onClick={() => onCompleteTool(call, { content: 'Keep refining.' })}
          >
            {msg('managedAgents.quickstart.keepRefining', 'Keep refining')}
          </Button>
        </div>
      ) : (
        <ToolStatus call={call} />
      )}
    </QuickstartAssistantTurn>
  );
}

export function CreateDeploymentResultCard({ call }: { call: QuickstartToolCall }) {
  const { msg } = useI18n();
  if (call.status === 'running') {
    return (
      <QuickstartAssistantTurn>
        <div className="flex items-center gap-2 text-sm text-muted-foreground/70">
          <Loader2 className="size-3.5 animate-spin" aria-hidden />
          {msg('managedAgents.quickstart.creatingDeployment', 'Creating deployment')}
        </div>
      </QuickstartAssistantTurn>
    );
  }
  if (call.status === 'failed') {
    return (
      <QuickstartAssistantTurn>
        <StatusLine tone="error">{call.error ?? msg('managedAgents.quickstart.deploymentCreationFailed', 'Deployment creation failed.')}</StatusLine>
      </QuickstartAssistantTurn>
    );
  }
  const deploymentYaml = {
    name: typeof call.input.name === 'string' ? call.input.name : 'Quickstart deployment',
    schedule: {
      type: 'cron',
      expression: typeof call.input.cron_expression === 'string' ? call.input.cron_expression : '0 9 * * 1',
      ...(typeof call.input.timezone === 'string' && call.input.timezone.trim() ? { timezone: call.input.timezone.trim() } : {})
    },
    initial_events: [
      {
        type: 'user.message',
        content: [
          {
            type: 'text',
            text: typeof call.input.initial_message === 'string' ? call.input.initial_message : 'Run the scheduled quickstart task.'
          }
        ]
      }
    ]
  };
  return (
    <QuickstartAssistantTurn>
      <StatusLine tone="success">{msg('managedAgents.quickstart.deploymentCreated', 'Deployment created')}</StatusLine>
      <QuickstartApiCallCard method="POST" path="/v1/deployments" code={shellYamlCommand('deployments', deploymentYaml)} maxLines={10} />
      {call.result ? <p className="mt-3 text-sm text-muted-foreground">{call.result}</p> : null}
    </QuickstartAssistantTurn>
  );
}

export function AwaitTestRunCard({
  call
}: {
  call: QuickstartToolCall;
  onSendTestRunMessage: (call: QuickstartToolCall, message: string) => Promise<void>;
}) {
  const { msg } = useI18n();
  const until = typeof call.input.until === 'string' ? call.input.until : 'first_message';
  return (
    <QuickstartAssistantTurn>
      <div className="flex items-center gap-2 text-sm text-muted-foreground/70">
        {call.status === 'awaiting_user' ? <Loader2 className="size-3.5 animate-spin" aria-hidden /> : null}
        {call.status === 'awaiting_user' ? (
          until === 'session_closed'
            ? msg('managedAgents.quickstart.waitingForSessionClose', 'Waiting for session to close...')
            : msg('managedAgents.quickstart.waitingForFirstMessage', 'Waiting for first message...')
        ) : (
          <ToolStatus call={call} />
        )}
      </div>
    </QuickstartAssistantTurn>
  );
}

export function OfferNextStepCard({
  call,
  labelOverride,
  onNext
}: {
  call: QuickstartToolCall;
  labelOverride?: string;
  onNext: (call: QuickstartToolCall) => Promise<void>;
}) {
  const { msg } = useI18n();
  const label = labelOverride ?? nextStepButtonLabel(call, msg);
  return (
    <QuickstartAssistantTurn>
      {call.status === 'awaiting_user' ? (
        <Button
          type="button"
          onClick={() => onNext(call)}
        >
          {label}
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      ) : (
        <ToolStatus call={call} />
      )}
    </QuickstartAssistantTurn>
  );
}

export function nextStepButtonLabel(call: QuickstartToolCall, msg?: I18nMsg) {
  const explicitLabel = typeof call.input.button_label === 'string' ? call.input.button_label.trim() : '';
  const nextStep = typeof call.input.next_step === 'string' ? call.input.next_step.trim().toLowerCase() : '';
  if (explicitLabel) {
    return explicitLabel;
  }
  if (nextStep === 'session' || nextStep === 'start_session') {
    return msg ? msg('managedAgents.quickstart.next.startSession', 'Next: Start session') : 'Next: Start session';
  }
  if (nextStep === 'integrate' || nextStep === 'integration' || nextStep === 'show_integration_exits') {
    return msg ? msg('managedAgents.quickstart.next.integrate', 'Next: Integrate') : 'Next: Integrate';
  }
  return msg ? msg('managedAgents.quickstart.next.next', 'Next') : 'Next';
}

export const integrationSnippetLanguages: IntegrationSnippetLanguage[] = ['cli', 'python', 'typescript', 'curl'];

export function isIntegrationSnippetLanguage(value: unknown): value is IntegrationSnippetLanguage {
  return integrationSnippetLanguages.some((item) => item === value);
}

export const integrationLanguageLabels: Record<IntegrationSnippetLanguage, string> = {
  cli: 'CLI',
  python: 'Python',
  typescript: 'TypeScript',
  curl: 'Curl'
};

export const integrationHighlightLanguage: Record<IntegrationSnippetLanguage, HighlightLanguage> = {
  cli: 'bash',
  python: 'python',
  typescript: 'typescript',
  curl: 'bash'
};

export function shellSingleQuoteEscape(value: string) {
  return value.replace(/'/g, "'\\''");
}

export function integrationSnippets({
  agentId,
  environmentId,
  prompt
}: {
  agentId: string;
  environmentId: string;
  prompt: string;
}): Record<IntegrationSnippetLanguage, string> {
  const promptJson = JSON.stringify(prompt);
  const shellPromptJson = shellSingleQuoteEscape(promptJson);
  const headers = `-H "x-api-key: $ANTHROPIC_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "anthropic-beta: managed-agents-2026-04-01"`;
  return {
    cli: `ant beta:sessions create \\
  --agent '${shellSingleQuoteEscape(agentId)}' \\
  --environment-id '${shellSingleQuoteEscape(environmentId)}'

# Replace SESSION_ID below with the id from the create output above
ant beta:sessions:events stream --session-id SESSION_ID

# In another shell:
ant beta:sessions:events send --session-id SESSION_ID \\
  --event '{"type":"user.message","content":[{"type":"text","text":${shellPromptJson}}]}'
`,
    python: `from anthropic import Anthropic

client = Anthropic()

session = client.beta.sessions.create(
    agent={"type": "agent", "id": "${agentId}"},
    environment_id="${environmentId}",
)

with client.beta.sessions.events.stream(
    session_id=session.id,
) as stream:
    client.beta.sessions.events.send(
        session_id=session.id,
        events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": ${promptJson}}],
            },
        ],
    )

    for event in stream:
        if event.type == "agent.message":
            for block in event.content:
                print(block.text, end="")
        elif event.type == "agent.tool_use":
            print(f"\\n[Using tool: {event.name}]")
        elif event.type == "session.status_idle":
            print("\\n\\nAgent finished.")
            break
`,
    typescript: `import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic();

const session = await client.beta.sessions.create({
  agent: { type: "agent", id: "${agentId}" },
  environment_id: "${environmentId}",
});

const stream = client.beta.sessions.events.stream(session.id);

await client.beta.sessions.events.send(session.id, {
  events: [
    { type: "user.message", content: [{ type: "text", text: ${promptJson} }] },
  ],
});

for await (const event of stream) {
  if (event.type === "agent.message") {
    for (const block of event.content) {
      process.stdout.write(block.text);
    }
  } else if (event.type === "agent.tool_use") {
    console.log(\`\\n[Using tool: ${'${event.name}'}]\`);
  } else if (event.type === "session.status_idle") {
    console.log("\\n\\nAgent finished.");
    break;
  }
}
`,
    curl: `# Create the session
curl "https://api.anthropic.com/v1/sessions?beta=true" \\
  ${headers} \\
  -H "content-type: application/json" \\
  -d '{
    "agent": {"type": "agent", "id": "${agentId}"},
    "environment_id": "${environmentId}"
  }'

# Send a user event (replace SESSION_ID from the response above)
curl "https://api.anthropic.com/v1/sessions/SESSION_ID/events?beta=true" \\
  ${headers} \\
  -H "content-type: application/json" \\
  -d '{
    "events": [{"type": "user.message", "content": [{"type": "text", "text": ${shellPromptJson}}]}]
  }'

# Stream events (SSE)
curl -N "https://api.anthropic.com/v1/sessions/SESSION_ID/events/stream?beta=true" \\
  ${headers}
`
  };
}

export function integrationScaffoldPrompt(agentId: string, environmentId: string) {
  return `Use the managed-agents skill (invoke /managed-agents first). Scaffold a minimal app that talks to Anthropic agent ${agentId} via the Anthropic SDK. Create a session with client.beta.sessions.create (environment_id ${environmentId}), open client.beta.sessions.events.stream, send a user.message event via client.beta.sessions.events.send, and print agent.message text as it streams. Handle session.status_idle to finish and errors to exit cleanly.`;
}

export function IntegrationExitsCard({
  call,
  agent,
  environment,
  onSelectExit
}: {
  call: QuickstartToolCall;
  agent: AgentApiResponse | null;
  environment: EnvironmentApiResponse | null;
  onSelectExit: (call: QuickstartToolCall, exit: 'scaffold' | 'go_to_agent') => Promise<void>;
}) {
  const { msg } = useI18n();
  const agentId = agent?.id || (typeof call.input.agent_id === 'string' ? call.input.agent_id : 'agent_id');
  const environmentId = environment?.id || (typeof call.input.environment_id === 'string' ? call.input.environment_id : 'env_...');
  const prompt = typeof call.input.user_prompt === 'string' ? call.input.user_prompt : 'Hello! What can you help me with?';
  const snippets = useMemo(() => integrationSnippets({ agentId, environmentId, prompt }), [agentId, environmentId, prompt]);
  const [language, setLanguage] = useState<IntegrationSnippetLanguage>('cli');
  const sampleCode = snippets[language];
  const scaffoldPrompt = useMemo(() => integrationScaffoldPrompt(agentId, environmentId), [agentId, environmentId]);
  const [copiedScaffold, setCopiedScaffold] = useState(false);
  const chooseScaffold = async () => {
    await copyText(scaffoldPrompt);
    setCopiedScaffold(true);
    window.setTimeout(() => setCopiedScaffold(false), 1200);
  };

  return (
    <QuickstartAssistantTurn>
      <div className="flex flex-col gap-4">
        <Tabs
          value={language}
          onValueChange={(nextValue) => {
            if (isIntegrationSnippetLanguage(nextValue)) {
              setLanguage(nextValue);
            }
          }}
          className="gap-0"
        >
          <div className="overflow-hidden rounded-lg border border-border bg-popover">
            <div className="flex min-h-11 items-center justify-between gap-3 border-b border-border px-3 py-1.5">
              <div className="text-sm font-medium text-foreground">
                {msg('managedAgents.quickstart.sampleCode', 'Sample code')}
              </div>
              <div className="flex min-w-0 items-center gap-1">
                <TabsList
                  aria-label={msg('managedAgents.quickstart.selectLanguage', 'Select language')}
                  className="subtle-scrollbar h-7 max-w-[238px] gap-0.5 overflow-x-auto bg-secondary p-0.5"
                >
                  {integrationSnippetLanguages.map((item) => (
                    <TabsTrigger
                      key={item}
                      value={item}
                      className="h-6 shrink-0 px-2 text-xs font-medium"
                    >
                      {integrationLanguageLabels[item]}
                    </TabsTrigger>
                  ))}
                </TabsList>
                <CopyButton value={sampleCode} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
              </div>
            </div>
            {integrationSnippetLanguages.map((item) => (
              <TabsContent key={item} value={item} className="mt-0">
                {item === language ? (
                  <ScrollableCodeBlock code={snippets[item]} language={integrationHighlightLanguage[item]} />
                ) : null}
              </TabsContent>
            ))}
          </div>
        </Tabs>
        <div className="flex flex-wrap items-center gap-2">
          {call.status === 'awaiting_user' ? (
            <>
              <Button
                type="button"
                size="sm"
                onClick={() => {
                  void onSelectExit(call, 'go_to_agent');
                }}
              >
                {msg('managedAgents.quickstart.exitQuickstart', 'Exit quickstart')}
                <ArrowUpRight className="size-4" aria-hidden />
              </Button>
              <Button
                type="button"
                variant="secondary"
                onClick={() => void chooseScaffold()}
              >
                {copiedScaffold ? null : <Copy className="size-3.5" aria-hidden />}
                {copiedScaffold
                  ? msg('managedAgents.quickstart.promptCopied', 'Prompt copied')
                  : msg('managedAgents.quickstart.scaffoldInClaudeCode', 'Scaffold in Claude Code')}
              </Button>
            </>
          ) : (
            <ToolStatus call={call} />
          )}
        </div>
      </div>
    </QuickstartAssistantTurn>
  );
}

export function GenericQuickstartToolCard({ call }: { call: QuickstartToolCall }) {
  const meta = quickstartToolMeta(call.name);
  return (
    <QuickstartAssistantTurn>
      <div className="rounded-lg bg-card p-3 shadow-sm">
        <div className="flex items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <meta.icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <div className="min-w-0">
              <p className="truncate text-[15px] font-medium text-foreground">{meta.label}</p>
              <p className="truncate text-sm text-muted-foreground">{call.name}</p>
            </div>
          </div>
          <ToolStatus call={call} />
        </div>
        {Object.keys(call.input).length ? (
          <pre className="mt-3 max-h-28 overflow-auto rounded-md bg-card-raised p-2 font-mono text-xs leading-5 text-foreground">
            {JSON.stringify(call.input, null, 2)}
          </pre>
        ) : null}
        {call.result || call.error ? <p className="mt-3 text-sm text-muted-foreground">{call.result ?? call.error}</p> : null}
      </div>
    </QuickstartAssistantTurn>
  );
}

export function ToolStatus({ call }: { call: QuickstartToolCall }) {
  const { msg } = useI18n();
  if (call.status === 'running') {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground/70">
        <Loader2 className="size-3 animate-spin" aria-hidden />
        {msg('managedAgents.quickstart.toolStatus.running', 'Running')}
      </span>
    );
  }
  if (call.status === 'awaiting_user') {
    return <span className="text-xs text-foreground">{msg('managedAgents.quickstart.toolStatus.awaitingInput', 'Awaiting input')}</span>;
  }
  if (call.status === 'failed') {
    return (
      <span className="inline-flex items-center gap-1.5 text-xs text-destructive">
        <AlertCircle className="size-3" aria-hidden />
        {msg('managedAgents.quickstart.toolStatus.failed', 'Failed')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
      <Check className="size-3" aria-hidden />
      {msg('managedAgents.quickstart.toolStatus.done', 'Done')}
    </span>
  );
}

export function parseQuestionInput(input: Record<string, unknown>): QuickstartQuestion[] {
  const questions = Array.isArray(input.questions) ? input.questions : [];
  return questions
    .map((item): QuickstartQuestion | null => {
      const question = toRecord(item);
      if (!question) {
        return null;
      }
      const options = Array.isArray(question.options)
        ? question.options
            .map((option) => {
              const typedOption = toRecord(option);
              if (!typedOption || typeof typedOption.label !== 'string') {
                return null;
              }
              return {
                label: typedOption.label,
                description: typeof typedOption.description === 'string' ? typedOption.description : ''
              };
            })
            .filter((option): option is { label: string; description: string } => Boolean(option))
        : [];
      return {
        header: typeof question.header === 'string' ? question.header : 'Question',
        question: typeof question.question === 'string' ? question.question : 'Choose an option.',
        multiSelect: question.multiSelect === true,
        options
      };
    })
    .filter((question): question is QuickstartQuestion => Boolean(question));
}

export function parseSubmittedQuestionAnswers(result?: string) {
  if (!result) {
    return [];
  }
  try {
    const parsed = JSON.parse(result);
    const answers = toRecord(parsed)?.answers;
    if (!Array.isArray(answers)) {
      return [];
    }
    return answers
      .map((item) => {
        const answer = toRecord(item);
        if (!answer) {
          return null;
        }
        const labels = Array.isArray(answer.answers)
          ? answer.answers.filter((label): label is string => typeof label === 'string' && Boolean(label.trim()))
          : [];
        return {
          question: typeof answer.question === 'string' ? answer.question : '',
          answers: labels
        };
      })
      .filter((answer): answer is { question: string; answers: string[] } => Boolean(answer));
  } catch {
    return [];
  }
}

export function quickstartItemId(prefix: string) {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}

export function appendQuickstartStatus(
  setChatItems: Dispatch<SetStateAction<QuickstartChatItem[]>>,
  content: string,
  tone: 'muted' | 'success' | 'error' = 'muted'
) {
  setChatItems((current) => [...current, { id: quickstartItemId('status'), type: 'status', content, tone }]);
}

export function updateQuickstartMessage(items: QuickstartChatItem[], itemId: string, content: string): QuickstartChatItem[] {
  return items.map((item) => (item.id === itemId && item.type === 'message' ? { ...item, content } : item));
}

export function updateQuickstartTool(
  setChatItems: Dispatch<SetStateAction<QuickstartChatItem[]>>,
  toolUseId: string,
  patch: Partial<QuickstartToolCall>
) {
  setChatItems((current) =>
    current.map((item) =>
      item.type === 'tool' && item.call.id === toolUseId
        ? { ...item, call: { ...item.call, ...patch } }
        : item
    )
  );
}

export function awaitingQuickstartToolCalls(items: QuickstartChatItem[]) {
  return items
    .filter((item): item is Extract<QuickstartChatItem, { type: 'tool' }> =>
      item.type === 'tool' && item.call.status === 'awaiting_user'
    )
    .map((item) => item.call);
}

export function quickstartChatReplyToolResult(call: QuickstartToolCall, reply: string) {
  if (call.name === 'build_agent_config') {
    return `User sent a message instead: "${reply}"`;
  }
  return `User replied in chat: ${reply}`;
}

export function toolResultBlock(toolUseId: string, result: QuickstartToolExecutionResult) {
  return {
    type: 'tool_result',
    tool_use_id: toolUseId,
    content: result.content,
    ...(result.isError ? { is_error: true } : {})
  };
}

export function quickstartVaultIdsFromInput(input: Record<string, unknown>) {
  if (Array.isArray(input.vault_ids)) {
    return input.vault_ids.filter((id): id is string => typeof id === 'string' && Boolean(id.trim())).map((id) => id.trim());
  }
  if (typeof input.vault_id === 'string' && input.vault_id.trim()) {
    return [input.vault_id.trim()];
  }
  if (typeof input.id === 'string' && input.id.trim()) {
    return [input.id.trim()];
  }
  return [];
}

export function quickstartVaultLabelsFromInput(input: Record<string, unknown>) {
  if (Array.isArray(input.vault_names)) {
    const names = input.vault_names.filter((name): name is string => typeof name === 'string' && Boolean(name.trim())).map((name) => name.trim());
    if (names.length) {
      return names;
    }
  }
  if (Array.isArray(input.vaults)) {
    const names = input.vaults
      .map((vault) => toRecord(vault))
      .map((vault) => {
        if (!vault) {
          return '';
        }
        if (typeof vault.display_name === 'string' && vault.display_name.trim()) {
          return vault.display_name.trim();
        }
        if (typeof vault.name === 'string' && vault.name.trim()) {
          return vault.name.trim();
        }
        return typeof vault.id === 'string' ? vault.id.trim() : '';
      })
      .filter(Boolean);
    if (names.length) {
      return names;
    }
  }
  return quickstartVaultIdsFromInput(input);
}

export function quickstartMcpServerUrl(agentConfig: CreateAgentInput | null, serverName: string) {
  const servers = Array.isArray(agentConfig?.mcp_servers) ? agentConfig.mcp_servers : [];
  for (const server of servers) {
    const record = toRecord(server);
    if (record?.name === serverName && typeof record.url === 'string') {
      return record.url;
    }
  }
  return '';
}

export function quickstartToolMeta(name: string): { label: string; icon: IconComponent } {
  switch (name) {
    case 'list_environments':
      return { label: 'List environments', icon: Cloud };
    case 'create_environment':
      return { label: 'Create environment', icon: Cloud };
    case 'vault_sharing_notice':
      return { label: 'Vault sharing notice', icon: LockKeyhole };
    case 'list_vaults':
      return { label: 'List vaults', icon: LockKeyhole };
    case 'select_vault':
      return { label: 'Select vault', icon: LockKeyhole };
    case 'create_vault':
      return { label: 'Create vault', icon: LockKeyhole };
    case 'create_vault_credential':
      return { label: 'Create credential', icon: LockKeyhole };
    case 'flag_schedule_intent':
      return { label: 'Schedule intent', icon: CalendarClock };
    case 'create_deployment':
      return { label: 'Create deployment', icon: Play };
    case 'web_search':
      return { label: 'Search web', icon: Search };
    default:
      return { label: name.replace(/_/g, ' '), icon: Terminal };
  }
}

export function PromptComposer({
  value,
  label,
  placeholder,
  isBusy = false,
  submitShortcut,
  formClassName = 'absolute inset-x-0 bottom-0 p-3 pt-0',
  onChange,
  onSubmit
}: {
  value: string;
  label: string;
  placeholder: string;
  isBusy?: boolean;
  submitShortcut?: PromptComposerSubmitShortcut;
  formClassName?: string;
  onChange: (value: string) => void;
  onSubmit?: () => void;
}) {
  const { msg } = useI18n();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const promptId = `${label.toLowerCase().replace(/\s+/g, '-')}-prompt`;
  const canSubmit = !isBusy && value.trim().length > 0;

  useEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) {
      return;
    }
    textarea.style.height = 'auto';
    textarea.style.height = `${textarea.scrollHeight}px`;
  }, [value]);

  const submitPrompt = () => {
    if (canSubmit) {
      onSubmit?.();
    }
  };

  const handleTextareaKeyDown = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (!submitShortcut || event.key !== 'Enter') {
      return;
    }
    const nativeEvent = event.nativeEvent as KeyboardEvent;
    // Respect IME composition so Enter confirms text instead of sending.
    if (nativeEvent.isComposing || nativeEvent.keyCode === 229) {
      return;
    }
    if (submitShortcut === 'enter') {
      if (event.shiftKey || isBusy) {
        return;
      }
    } else if ((!event.metaKey && !event.ctrlKey) || isBusy) {
      return;
    }
    event.preventDefault();
    if (event.repeat) {
      return;
    }
    submitPrompt();
  };

  return (
    <form
      className={formClassName}
      onSubmit={(event) => {
        event.preventDefault();
        submitPrompt();
      }}
    >
      <label htmlFor={promptId} className="sr-only">
        {label}
      </label>
      <InputGroup
        className={clsx(quickstartComposerFrameClassName, 'items-stretch gap-0 p-0 pl-0')}
      >
        <InputGroupTextarea
          ref={textareaRef}
          id={promptId}
          value={value}
          rows={1}
          placeholder={placeholder}
          className={clsx(quickstartComposerTextareaClassName, 'subtle-scrollbar block max-h-52 overflow-y-auto px-4 pb-2 pt-4 text-[15px] leading-6')}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleTextareaKeyDown}
        />
        <InputGroupAddon align="block-end" className="justify-end px-3 pb-3 pt-0">
          <InputGroupButton
            type="submit"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('managedAgents.quickstart.sendMessage', 'Send message')}
            disabled={!canSubmit}
            className={quickstartComposerSendButtonClassName}
          >
            {isBusy ? <Loader2 className="size-4 animate-spin" aria-hidden /> : <ArrowUp className="size-4" aria-hidden />}
          </InputGroupButton>
        </InputGroupAddon>
      </InputGroup>
    </form>
  );
}

export function BrowseTemplatesPanel({
  query,
  searchRef,
  visibleTemplates,
  onQueryChange,
  onTemplateClick
}: {
  query: string;
  searchRef: RefObject<HTMLInputElement | null>;
  visibleTemplates: AgentTemplate[];
  onQueryChange: (value: string) => void;
  onTemplateClick: (template: AgentTemplate) => void;
}) {
  const { msg } = useI18n();
  const listRef = useRef<HTMLDivElement>(null);
  const [isSearchFocused, setIsSearchFocused] = useState(false);
  const [showScrollCue, setShowScrollCue] = useState(false);

  useEffect(() => {
    const list = listRef.current;
    if (!list) {
      return;
    }

    const updateScrollCue = () => {
      setShowScrollCue(list.scrollHeight - list.clientHeight - list.scrollTop > 1);
    };

    updateScrollCue();
    list.addEventListener('scroll', updateScrollCue, { passive: true });
    window.addEventListener('resize', updateScrollCue);
    const resizeObserver = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(updateScrollCue) : null;
    resizeObserver?.observe(list);

    return () => {
      list.removeEventListener('scroll', updateScrollCue);
      window.removeEventListener('resize', updateScrollCue);
      resizeObserver?.disconnect();
    };
  }, [visibleTemplates]);

  const scrollMoreTemplates = () => {
    const list = listRef.current;
    if (!list) {
      return;
    }
    list.scrollBy({ top: Math.max(160, list.clientHeight * 0.72), behavior: 'smooth' });
  };

  return (
    <Card className="relative h-full min-h-0 overflow-hidden py-0 shadow-sm">
      <CardContent className="flex h-full min-h-0 flex-col p-4">
        <div className="mb-4 flex items-center justify-between gap-4">
          <h2 className="text-[18px] font-semibold leading-none text-foreground">
            {msg('managedAgents.quickstart.browseTemplates', 'Browse templates')}
          </h2>
        </div>

        <label className="relative block" htmlFor="agent-template-search">
          <Search className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            ref={searchRef}
            id="agent-template-search"
            value={query}
            placeholder={msg('managedAgents.quickstart.searchTemplates', 'Search templates')}
            className="quickstart-focus-frame h-10 border-border bg-accent pl-9 pr-3 text-sm placeholder:text-muted-foreground data-[focused=true]:border-primary"
            data-focused={isSearchFocused ? 'true' : undefined}
            onFocus={() => setIsSearchFocused(true)}
            onBlur={() => setIsSearchFocused(false)}
            onChange={(event) => onQueryChange(event.target.value)}
          />
        </label>

        {visibleTemplates.length > 0 ? (
          <div
            ref={listRef}
            className="subtle-scrollbar mt-4 grid min-h-0 flex-1 auto-rows-[136px] content-start items-stretch grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3 overflow-y-auto pr-1"
          >
            {visibleTemplates.map((template) => (
              <TemplateCard key={template.id} template={template} onClick={() => onTemplateClick(template)} />
            ))}
          </div>
        ) : (
          <div ref={listRef} className="subtle-scrollbar mt-4 min-h-0 flex-1 overflow-y-auto pr-1">
            <Card size="sm" className="py-0">
              <CardContent className="grid min-h-[240px] place-items-center px-4 py-12 text-center">
                <div>
                  <Search className="mx-auto mb-3 size-6 text-muted-foreground/70" aria-hidden />
                  <div className="text-sm font-medium text-foreground">
                    {msg('managedAgents.quickstart.noTemplatesFound', 'No templates found')}
                  </div>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {msg('managedAgents.quickstart.tryDifferentSearch', 'Try a different search.')}
                  </p>
                </div>
              </CardContent>
            </Card>
          </div>
        )}

        {showScrollCue ? (
          <Button
            type="button"
            aria-label={msg('managedAgents.quickstart.scrollMoreTemplates', 'Scroll to see more templates')}
            variant="outline"
            size="icon"
            className="absolute bottom-7 left-1/2 size-8 -translate-x-1/2 rounded-full bg-accent text-foreground shadow-lg hover:bg-accent"
            onClick={scrollMoreTemplates}
          >
            <ChevronDown className="size-4" aria-hidden />
          </Button>
        ) : null}
      </CardContent>
    </Card>
  );
}

export function TemplateDetailPanel({
  template,
  format,
  onBack,
  onFormatChange,
  onUseTemplate,
  isUsing
}: {
  template: AgentTemplate;
  format: CodeFormat;
  onBack: () => void;
  onFormatChange: (format: CodeFormat) => void;
  onUseTemplate: () => void;
  isUsing: boolean;
}) {
  const { msg } = useI18n();
  const code = codeForTemplate(template, format);
  const title = templateTitle(template, msg);
  return (
    <Card className="h-full min-h-0 overflow-hidden py-0 shadow-sm">
      <CardContent className="flex h-full min-h-0 flex-col p-0">
        <div className="flex h-11 items-center gap-2 border-b border-border px-3">
          <Button
            type="button"
            aria-label={msg('managedAgents.quickstart.backToTemplates', 'Back to templates')}
            variant="ghost"
            size="icon-sm"
            className="text-foreground hover:bg-secondary"
            onClick={onBack}
          >
            <ChevronLeft className="size-4" aria-hidden />
          </Button>
          <h2 className="min-w-0 flex-1 truncate text-[15px] font-medium text-foreground">
            {title}
            <span className="ml-1 text-muted-foreground/70">· {msg('managedAgents.quickstart.templateSuffix', 'Template')}</span>
          </h2>
          <FormatSelect value={format} onChange={onFormatChange} />
          <CopyButton value={code} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
          <Button
            type="button"
            disabled={isUsing}
            size="sm"
            className="disabled:cursor-not-allowed disabled:opacity-70"
            onClick={onUseTemplate}
          >
            {isUsing ? <Loader2 className="size-3.5 animate-spin" aria-hidden /> : null}
            {isUsing ? msg('common.creating', 'Creating...') : msg('managedAgents.quickstart.useTemplate', 'Use template')}
          </Button>
        </div>
        <div className="subtle-scrollbar min-h-0 flex-1 overflow-auto px-5 py-4">
          <NumberedCodeBlock code={code} format={format} />
        </div>
      </CardContent>
    </Card>
  );
}

export function CreatedAgentConfigPanel({
  template,
  agent,
  agentConfig,
  environment,
  session,
  testRunStopped,
  workspaceId,
  testRunMessage,
  format,
  tab,
  canSendTestRunMessage,
  onTestRunMessageChange,
  onSendTestRunMessage,
  onRerunTestRun,
  onConfigureEnvironment,
  onFormatChange,
  onTabChange
}: {
  template: AgentTemplate;
  agent: AgentApiResponse | null;
  agentConfig: CreateAgentInput | null;
  environment: EnvironmentApiResponse | null;
  session: SessionApiResponse | null;
  testRunStopped: boolean;
  workspaceId: string;
  testRunMessage: string;
  format: CodeFormat;
  tab: AgentPanelTab;
  canSendTestRunMessage: boolean;
  onTestRunMessageChange: (value: string) => void;
  onSendTestRunMessage: () => Promise<void>;
  onRerunTestRun: () => Promise<void>;
  onConfigureEnvironment: () => Promise<void>;
  onFormatChange: (format: CodeFormat) => void;
  onTabChange: (tab: AgentPanelTab) => void;
}) {
  const { msg } = useI18n();
  const displayedConfig = displayAgentConfig(agentConfig ?? createDialogAgentConfig(template));
  const code = format === 'YAML'
    ? yamlStringify(displayedConfig)
    : JSON.stringify(displayedConfig, null, 2);
  return (
    <aside className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-popover shadow-sm">
      <Tabs
        value={tab}
        onValueChange={(nextValue) => onTabChange(nextValue as AgentPanelTab)}
        className="h-full min-h-0 gap-0"
      >
        <div className="flex h-11 items-center justify-between border-b border-border px-4">
          <TabsList
            variant="line"
            aria-label={msg('managedAgents.quickstart.panelViews', 'Agent panel views')}
            className="h-full gap-5 p-0"
          >
            <TabsTrigger
              value="config"
              className="h-full rounded-none border-0 px-0 text-sm font-normal text-muted-foreground hover:bg-transparent hover:text-foreground data-active:bg-transparent data-active:text-foreground data-active:shadow-none after:bottom-0 after:h-px"
            >
              {msg('managedAgents.quickstart.config', 'Config')}
            </TabsTrigger>
            <TabsTrigger
              value="preview"
              className="h-full rounded-none border-0 px-0 text-sm font-normal text-muted-foreground hover:bg-transparent hover:text-foreground data-active:bg-transparent data-active:text-foreground data-active:shadow-none after:bottom-0 after:h-px"
            >
              {msg('managedAgents.quickstart.preview', 'Preview')}
            </TabsTrigger>
          </TabsList>
          {tab === 'config' ? (
            <div className="flex items-center gap-2">
              <FormatSelect value={format} onChange={onFormatChange} compact />
              <CopyButton value={code} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
            </div>
          ) : null}
        </div>

        <TabsContent value="config" className="subtle-scrollbar min-h-0 overflow-auto px-5 py-4">
          <NumberedCodeBlock code={code} format={format} />
        </TabsContent>
        <TabsContent value="preview" className="min-h-0">
          <PreviewEnvironmentPanel
            agent={agent}
            environment={environment}
            session={session}
            isStopped={testRunStopped}
            workspaceId={workspaceId}
            testRunMessage={testRunMessage}
            canSendTestRunMessage={canSendTestRunMessage}
            onTestRunMessageChange={onTestRunMessageChange}
            onSendTestRunMessage={onSendTestRunMessage}
            onRerunTestRun={onRerunTestRun}
            onConfigureEnvironment={onConfigureEnvironment}
          />
        </TabsContent>
      </Tabs>
    </aside>
  );
}

export function PreviewEnvironmentPanel({
  agent,
  environment,
  session,
  isStopped,
  workspaceId,
  testRunMessage,
  canSendTestRunMessage,
  onTestRunMessageChange,
  onSendTestRunMessage,
  onRerunTestRun,
  onConfigureEnvironment
}: {
  agent: AgentApiResponse | null;
  environment: EnvironmentApiResponse | null;
  session: SessionApiResponse | null;
  isStopped: boolean;
  workspaceId: string;
  testRunMessage: string;
  canSendTestRunMessage: boolean;
  onTestRunMessageChange: (value: string) => void;
  onSendTestRunMessage: () => Promise<void>;
  onRerunTestRun: () => Promise<void>;
  onConfigureEnvironment: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const [eventState, setEventState] = useState<{
    loading: boolean;
    error: string | null;
    events: QuickstartSessionEvent[];
  }>({ loading: false, error: null, events: [] });
  const [eventRefreshKey, setEventRefreshKey] = useState(0);
  const [sendingMessage, setSendingMessage] = useState(false);
  const [rerunning, setRerunning] = useState(false);
  const environmentHref = environment
    ? managedEntityDetailHref(workspaceId, 'environments', environment.id)
    : null;
  const refreshLatestEvents = async () => {
    if (!session?.id) {
      return;
    }
    try {
      const page = await listSessionEvents(session.id, workspaceId, 'asc');
      setEventState((current) => ({
        loading: false,
        error: null,
        events: mergeSessionEvents(current.events, page.data)
      }));
    } catch (error) {
      setEventState((current) => ({
        ...current,
        loading: false,
        error: errorMessage(error)
      }));
    }
  };

  useEffect(() => {
    if (!session?.id) {
      setEventState({ loading: false, error: null, events: [] });
      return;
    }

    let active = true;
    const streamController = new AbortController();

    const refreshEvents = async (loading = false) => {
      if (loading) {
        setEventState((current) => ({ ...current, loading: true, error: null }));
      }
      try {
        const page = await listSessionEvents(session.id, workspaceId, 'asc');
        if (!active) {
          return;
        }
        setEventState((current) => ({
          loading: false,
          error: null,
          events: mergeSessionEvents(current.events, page.data)
        }));
      } catch (error) {
        if (!active) {
          return;
        }
        setEventState((current) => ({
          ...current,
          loading: false,
          error: errorMessage(error)
        }));
      }
    };

    void refreshEvents(true);
    const interval = window.setInterval(() => void refreshEvents(false), 2500);
    void streamQuickstartSessionEvents({
      sessionId: session.id,
      workspaceId,
      signal: streamController.signal,
      onEvent: (event) => {
        if (!active) {
          return;
        }
        setEventState((current) => ({
          ...current,
          loading: false,
          error: null,
          events: mergeSessionEvents(current.events, [event])
        }));
      }
    }).catch((error) => {
      if (!active || (error as DOMException).name === 'AbortError') {
        return;
      }
      setEventState((current) => ({
        ...current,
        loading: false
      }));
    });

    return () => {
      active = false;
      window.clearInterval(interval);
      streamController.abort();
    };
  }, [eventRefreshKey, session?.id, workspaceId]);

  if (session && environment) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <div className="flex h-11 shrink-0 items-center justify-between gap-3 border-b border-border px-2">
          <ButtonLink
            href={environmentHref ?? undefined}
            variant="ghost"
            size="sm"
            className="min-w-0 justify-start gap-2 px-2 text-sm font-normal text-foreground"
          >
            <Cloud className="size-4 shrink-0 text-foreground" aria-hidden />
            <span className="truncate">{environment.name}</span>
          </ButtonLink>
          <ButtonLink
            href={`/workspaces/${workspaceId}/sessions/${session.id}`}
            target="_blank"
            rel="noreferrer"
            variant="ghost"
            className="shrink-0 gap-1 px-2 font-semibold text-foreground"
          >
            {msg('managedAgents.quickstart.viewSession', 'View session')}
            <ArrowUpRight className="size-4 text-foreground" aria-hidden />
          </ButtonLink>
        </div>
        <SessionTracePanel
          events={eventState.events}
          loading={eventState.loading}
          error={eventState.error}
          sessionStartedAt={session.created_at}
        />
        <div className="shrink-0 border-t border-border px-4 py-3">
          {isStopped ? (
            <div className="flex justify-end">
              <Button
                type="button"
                aria-label={msg('managedAgents.quickstart.rerun', 'Rerun')}
                disabled={rerunning}
                variant="outline"
                size="icon-sm"
                className="bg-accent text-foreground hover:bg-accent disabled:cursor-wait disabled:opacity-70"
                onClick={() => {
                  setRerunning(true);
                  void onRerunTestRun()
                    .finally(() => {
                      setRerunning(false);
                      void refreshLatestEvents();
                      window.setTimeout(() => void refreshLatestEvents(), 0);
                      setEventRefreshKey((value) => value + 1);
                    });
                }}
              >
                {rerunning ? <Loader2 className="size-3.5 animate-spin" aria-hidden /> : <Play className="size-3.5" aria-hidden />}
              </Button>
            </div>
          ) : (
            <QuickstartSessionComposer
              value={testRunMessage}
              placeholder={msg('managedAgents.quickstart.sendMessageToAgent', 'Send a message to the agent')}
              disabled={!canSendTestRunMessage}
              loading={sendingMessage}
              onChange={onTestRunMessageChange}
              onSubmit={() => {
                if (!testRunMessage.trim() || !canSendTestRunMessage || sendingMessage) {
                  return;
                }
                setSendingMessage(true);
                void onSendTestRunMessage()
                  .finally(() => {
                    setSendingMessage(false);
                    void refreshLatestEvents();
                    window.setTimeout(() => void refreshLatestEvents(), 0);
                    setEventRefreshKey((value) => value + 1);
                  });
              }}
            />
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-11 items-center border-b border-border px-4">
        {environment && environmentHref ? (
          <ButtonLink
            href={environmentHref ?? undefined}
            variant="ghost"
            size="sm"
            className="min-w-0 justify-start gap-2 px-2 text-sm font-normal text-foreground"
          >
            <Cloud className="size-4 shrink-0 text-foreground" aria-hidden />
            <span className="truncate">{environment.name}</span>
          </ButtonLink>
        ) : (
          <Button
            type="button"
            variant="ghost"
            className="gap-2 px-2 text-sm font-normal text-foreground"
            onClick={() => {
              void onConfigureEnvironment();
            }}
          >
            <Cloud className="size-4 text-foreground" aria-hidden />
            {msg('managedAgents.quickstart.selectEnvironment', 'Select an environment')}
            <ChevronRight className="size-4 text-muted-foreground/70" aria-hidden />
          </Button>
        )}
      </div>
      {environment ? (
        <div className="subtle-scrollbar flex-1 overflow-auto p-4">
          <div className="rounded-lg border border-border bg-secondary p-4">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-sm font-semibold text-foreground">{agent?.name ?? msg('managedAgents.common.agent', 'Agent')}</p>
                <p className="mt-1 text-sm text-muted-foreground">{environment.name}</p>
              </div>
              <span className="rounded bg-emerald-500/10 px-2 py-0.5 text-xs font-semibold text-emerald-600 dark:text-emerald-400">
                {session?.status ?? 'ready'}
              </span>
            </div>
            <dl className="mt-4 grid gap-3 text-sm">
              <div className="flex items-center justify-between gap-4">
                <dt className="text-muted-foreground">{msg('managedAgents.quickstart.environmentId', 'Environment ID')}</dt>
                <dd className="font-mono text-xs text-foreground">{environment.id}</dd>
              </div>
              <div className="flex items-center justify-between gap-4">
                <dt className="text-muted-foreground">{msg('managedAgents.common.session', 'Session')}</dt>
                <dd className="font-mono text-xs text-foreground">{session?.id ?? msg('managedAgents.quickstart.notStarted', 'Not started')}</dd>
              </div>
            </dl>
          </div>
        </div>
      ) : (
        <div className="grid flex-1 place-items-center px-4">
          <Card size="sm" className="w-full max-w-[368px] py-0 shadow-sm">
            <CardContent className="flex flex-col items-center px-6 py-8 text-center">
            <Cloud className="mx-auto mb-5 size-16 stroke-[1.2] text-foreground" aria-hidden />
            <CardTitle className="text-lg">
              {msg('managedAgents.quickstart.noEnvironmentsYet', 'No environments yet')}
            </CardTitle>
            <CardDescription className="mt-3">
              {msg('managedAgents.quickstart.needEnvironment', "You'll need an environment to run a test session.")}
            </CardDescription>
            <Button
              type="button"
              size="lg"
              className="mt-5"
              onClick={() => {
                void onConfigureEnvironment();
              }}
            >
              <Plus className="size-4" aria-hidden />
              {msg('managedAgents.quickstart.configureEnvironment', 'Configure environment')}
            </Button>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}

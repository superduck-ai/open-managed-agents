import { useState } from 'react';
import { ArrowUp, Square, Sparkles } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button, ButtonLink } from '../../../../shared/ui/button';
import { Textarea } from '../../../../shared/ui/textarea';
import { Card, CardContent, CardHeader, CardTitle } from '../../../../shared/ui/card';
import {
  MessageScroller,
  MessageScrollerContent,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '../../../../shared/ui/message-scroller';
import { ManagedErrorAlert } from '../../components/common';
import { errorMessage } from '../../utils';
import { builderHistoryText, type BuilderMessage, type BuilderState } from './model';
import { useBuilder, type Builder } from './useBuilder';
import { BuilderGate } from './BuilderGate';
import { BuilderTest } from './BuilderTest';
import { BuilderIntegration } from './BuilderIntegration';
import { BuilderStart } from './BuilderStart';

export function BuilderPage({
  accountId,
  workspaceId,
  orgUuid,
  models,
}: {
  accountId: string;
  workspaceId: string;
  orgUuid: string;
  models: string[];
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const builder = useBuilder({ accountId, workspaceId, orgUuid, models });
  const { state } = builder;
  if (builder.catalog.isPending) return <section className="min-h-96" aria-busy="true" />;
  if (builder.catalog.isError)
    return (
      <div className="mx-auto max-w-xl space-y-4 py-12">
        <ManagedErrorAlert>{errorMessage(builder.catalog.error)}</ManagedErrorAlert>
        <Button onClick={() => void builder.catalog.refetch()}>{zh ? '重试' : 'Retry'}</Button>
      </div>
    );
  if (!state.initialized)
    return <BuilderStart builder={builder} composer={<BuilderComposer builder={builder} entry />} />;
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col gap-4">
      <div className={`grid min-h-0 min-w-0 flex-1 gap-6 ${builderColumns(state)}`}>
        <div className="flex min-h-0 min-w-0 flex-col">
          <MessageScrollerProvider autoScroll defaultScrollPosition="end">
            <MessageScroller className="min-h-0 flex-1">
              <MessageScrollerViewport className="px-1 py-6">
                <MessageScrollerContent className="mx-auto w-full max-w-3xl space-y-6">
                  {state.messages.map((message, index) => (
                    <BuilderMessageView key={index} message={message} pendingToolId={state.gate?.id} />
                  ))}
                  <BuilderStatus builder={builder} />
                  {state.gate && !state.operation && (
                    <Card>
                      <CardContent>
                        <BuilderGate
                          key={state.gate.id}
                          builder={builder}
                          workspaceId={workspaceId}
                          orgUuid={orgUuid}
                        />
                      </CardContent>
                    </Card>
                  )}
                  {state.step === 'integrate' && state.gate?.name === 'show_integration_exits' && (
                    <BuilderIntegration state={state} workspaceId={workspaceId} accountId={accountId} />
                  )}
                  {state.initialized && !state.gate && !state.operation && !builder.busy && (
                    <Button variant="ghost" onClick={() => void builder.resume()}>
                      {zh ? '继续' : 'Continue'}
                    </Button>
                  )}
                </MessageScrollerContent>
              </MessageScrollerViewport>
            </MessageScroller>
          </MessageScrollerProvider>
          <BuilderComposer builder={builder} />
        </div>
        {state.sessionID && !state.testClosed && (
          <BuilderTest key={state.sessionID} builder={builder} workspaceId={workspaceId} accountId={accountId} />
        )}
        {state.initialized && <BuilderInspector builder={builder} workspaceId={workspaceId} />}
      </div>
    </section>
  );
}
function BuilderMessageView({ message, pendingToolId }: { message: BuilderMessage; pendingToolId?: string }) {
  const { locale } = useI18n();
  const text = builderHistoryText(message, locale === 'zh-CN', pendingToolId);
  if (!text) return null;
  return (
    <div
      className={
        message.role === 'user'
          ? 'ml-auto w-fit max-w-[90%] rounded-xl bg-muted px-4 py-3'
          : 'whitespace-pre-wrap leading-7'
      }
    >
      <p className="break-words whitespace-pre-wrap">{text}</p>
    </div>
  );
}
function BuilderInspector({ builder, workspaceId }: { builder: Builder; workspaceId: string }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const steps = [
    { id: 'agent', label: zh ? '创建智能体' : 'Create agent' },
    { id: 'environment', label: zh ? '配置环境' : 'Environment' },
    { id: 'session', label: zh ? '试跑' : 'Test' },
    ...(state.wantsSchedule ? [{ id: 'deploy', label: zh ? '定时运行' : 'Schedule' }] : []),
    { id: 'integrate', label: zh ? '接入' : 'Integrate' },
  ];
  const resources = [
    ...(state.environment ? [{ ...state.environment, section: 'environments' }] : []),
    ...state.vaults.map((vault) => ({ ...vault, section: 'credential-vaults' })),
    ...(state.deployment ? [{ ...state.deployment, section: 'deployments' }] : []),
  ];
  return (
    <aside className="space-y-4 overflow-y-auto py-6">
      <Card>
        <CardHeader>
          <CardTitle>{zh ? '进度' : 'Progress'}</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="space-y-4">
            {steps.map((step, index) => (
              <li
                key={step.id}
                aria-current={state.step === step.id ? 'step' : undefined}
                className={`flex items-center gap-3 ${state.step === step.id ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
              >
                <span
                  className={`grid size-6 place-items-center rounded-full border-[0.5px] border-border text-xs ${state.step === step.id ? 'bg-muted' : ''}`}
                >
                  {index + 1}
                </span>
                {step.label}
              </li>
            ))}
          </ol>
        </CardContent>
      </Card>
      {state.agent && (
        <Card>
          <CardHeader>
            <CardTitle>{state.agent.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-xs text-muted-foreground">v{state.agent.version}</p>{' '}
            {(state.savedConfig?.skills ?? []).map((skill) => (
              <p key={skill.skill_id} className="truncate text-sm">
                {builder.catalog.data?.skills.find((item) => item.id === skill.skill_id)?.displayTitle ??
                  skill.skill_id}
              </p>
            ))}
            <ButtonLink
              variant="ghost"
              size="sm"
              href={`/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(state.agent.id)}`}
            >
              {zh ? '查看配置' : 'View configuration'}
            </ButtonLink>
          </CardContent>
        </Card>
      )}
      {resources.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>{zh ? '资源' : 'Resources'}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {resources.map((resource) => (
              <a
                key={resource.id}
                className="block truncate text-sm hover:underline"
                title={resource.name}
                href={`/workspaces/${encodeURIComponent(workspaceId)}/${resource.section}/${encodeURIComponent(resource.id)}`}
              >
                {resource.name}
              </a>
            ))}
          </CardContent>
        </Card>
      )}
    </aside>
  );
}

function BuilderStatus({ builder }: { builder: Builder }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  return (
    <>
      {' '}
      {builder.partial && (
        <p aria-live="polite" className="whitespace-pre-wrap">
          {builder.partial}
        </p>
      )}
      {builder.error && <ManagedErrorAlert>{builder.error}</ManagedErrorAlert>}
      {state.operation && (
        <div className="space-y-3">
          <p>{zh ? '正在核对保存结果，请勿重复创建。' : 'Check the pending save before creating another resource.'}</p>
          <Button disabled={builder.busy} onClick={() => void builder.recover()}>
            {zh ? '查询保存结果' : 'Check saved result'}
          </Button>
        </div>
      )}
    </>
  );
}

function BuilderComposer({ builder, entry = false }: { builder: Builder; entry?: boolean }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  const [draft, setDraft] = useState('');
  const send = () => {
    const text = draft.trim();
    if (!text) return;
    setDraft('');
    void builder.reply(text);
  };
  return (
    <form
      className={`mx-auto flex w-full max-w-3xl gap-2 border-[0.5px] border-input bg-card p-3 focus-within:border-foreground ${entry ? 'flex-col rounded-2xl' : 'items-end rounded-xl'}`}
      onSubmit={(event) => {
        event.preventDefault();
        send();
      }}
    >
      <Textarea
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        placeholder={
          entry
            ? zh
              ? '一个客服助手，根据我们的文档回答问题，遇到无法解决的问题时交给人工…'
              : 'A customer support agent that answers from our docs and hands unresolved questions to a person…'
            : zh
              ? '描述你的需求…'
              : 'Describe what you need…'
        }
        aria-label={zh ? '构建消息' : 'Builder message'}
        className="max-h-48 min-h-16 resize-none border-0 bg-transparent px-1 pt-1 pb-3 text-sm leading-6 shadow-none focus-visible:ring-0 dark:bg-transparent"
        disabled={Boolean(state.operation)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            if (!builder.busy) send();
          }
        }}
      />
      <div className={entry ? 'flex w-full items-center justify-between text-muted-foreground' : 'contents'}>
        {entry && <Sparkles aria-hidden="true" className="ml-1 size-4" />}
        {builder.busy ? (
          <Button
            type="button"
            size="icon"
            aria-label={zh ? '停止' : 'Stop'}
            variant="outline"
            onClick={builder.cancel}
          >
            <Square />
          </Button>
        ) : (
          <Button
            type="submit"
            size="icon"
            aria-label={zh ? '发送' : 'Send'}
            disabled={!draft.trim() || Boolean(state.operation)}
          >
            <ArrowUp />
          </Button>
        )}
      </div>
    </form>
  );
}

function builderColumns(state: BuilderState) {
  if (!state.initialized) return '';
  return state.sessionID && !state.testClosed
    ? 'xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_240px]'
    : 'xl:grid-cols-[minmax(0,1fr)_280px]';
}

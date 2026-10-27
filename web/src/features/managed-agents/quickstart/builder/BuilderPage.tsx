import { useState } from 'react';
import { ArrowUp, Square } from 'lucide-react';
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
import { agentTemplates } from '../../agentConfig';
import { templateTitle } from '../../labels';
import { errorMessage } from '../../utils';
import { builderHistoryText, type BuilderMessage, type BuilderState } from './model';
import { useBuilder, type Builder } from './useBuilder';
import { BuilderGate } from './BuilderGate';
import { BuilderTest } from './BuilderTest';
import { BuilderIntegration } from './BuilderIntegration';

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
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className={`grid min-h-0 min-w-0 flex-1 gap-6 ${builderColumns(state)}`}>
        <div className="flex min-h-0 min-w-0 flex-col">
          <MessageScrollerProvider autoScroll defaultScrollPosition="end">
            <MessageScroller className="min-h-0 flex-1">
              <MessageScrollerViewport className="px-1 py-6">
                <MessageScrollerContent className="mx-auto w-full max-w-3xl space-y-6">
                  {!state.initialized && <BuilderEntry builder={builder} />}
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

function BuilderEntry({ builder }: { builder: Builder }) {
  const { locale, msg } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  return (
    <>
      {' '}
      {!state.initialized && (
        <>
          <h1 className="pt-8 text-2xl font-semibold">{zh ? '你想构建什么？' : 'What would you like to build?'}</h1>
          <div className="grid gap-3 sm:grid-cols-2">
            {agentTemplates
              .filter((template) => template.id !== 'blank')
              .slice(0, 6)
              .map((template) => (
                <Button
                  key={template.id}
                  variant="outline"
                  className="h-auto justify-start px-4 py-5 text-left whitespace-normal"
                  disabled={builder.busy}
                  onClick={() => void builder.startTemplate(template)}
                >
                  {templateTitle(template, msg)}
                </Button>
              ))}
          </div>
          <Button
            variant="ghost"
            disabled={builder.busy}
            onClick={() =>
              void builder.reply(
                zh ? '从空白开始，先问我想实现什么。' : 'Start from scratch. Ask what I want to build first.',
              )
            }
          >
            {zh ? '从空白开始' : 'Start from scratch'}
          </Button>
        </>
      )}
    </>
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

function BuilderComposer({ builder }: { builder: Builder }) {
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
      className="mx-auto flex w-full max-w-3xl items-end gap-2 rounded-xl border-[0.5px] border-border bg-card p-3"
      onSubmit={(event) => {
        event.preventDefault();
        send();
      }}
    >
      <Textarea
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        placeholder={zh ? '描述你的需求…' : 'Describe what you need…'}
        aria-label={zh ? '构建消息' : 'Builder message'}
        className="max-h-48 min-h-16 resize-none border-0 bg-transparent shadow-none focus-visible:ring-0"
        disabled={Boolean(state.operation)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            if (!builder.busy) send();
          }
        }}
      />
      {builder.busy ? (
        <Button type="button" size="icon" aria-label={zh ? '停止' : 'Stop'} variant="outline" onClick={builder.cancel}>
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
    </form>
  );
}

function builderColumns(state: BuilderState) {
  if (!state.initialized) return '';
  return state.sessionID && !state.testClosed
    ? 'xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_240px]'
    : 'xl:grid-cols-[minmax(0,1fr)_280px]';
}

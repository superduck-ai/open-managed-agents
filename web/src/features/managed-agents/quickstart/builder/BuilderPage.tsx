import { useState, type ReactNode } from 'react';
import { ArrowLeft, ArrowRight, Settings2 } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { Card, CardContent } from '../../../../shared/ui/card';
import { ToggleGroup, ToggleGroupItem } from '../../../../shared/ui/toggle-group';
import {
  MessageScroller,
  MessageScrollerContent,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '../../../../shared/ui/message-scroller';
import { ManagedErrorAlert } from '../../components/common';
import { errorMessage } from '../../utils';
import { BuilderHistory } from './BuilderHistory';
import { BuilderAvatar } from './BuilderAvatar';
import { BuilderInspector } from './BuilderInspector';
import { useBuilder, type Builder } from './useBuilder';
import { BuilderGate } from './BuilderGate';
import { BuilderTest } from './BuilderTest';
import { BuilderIntegration } from './BuilderIntegration';
import { BuilderEntry } from './BuilderEntry';
import { BuilderPanels } from './BuilderPanels';
import { useBuilderPanel } from './panelContext';
import { BuilderComposer } from './BuilderComposer';

export function BuilderPage({
  accountId,
  workspaceId,
  orgUuid,
  models,
  startHeader,
}: {
  accountId: string;
  workspaceId: string;
  orgUuid: string;
  models: string[];
  startHeader?: ReactNode;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const builder = useBuilder({ accountId, workspaceId, orgUuid, models });
  const [active, setActive] = useState(false);
  if (builder.catalog.isPending)
    return (
      <p role="status" className="mx-auto py-12 text-sm text-muted-foreground">
        {zh ? '正在加载工作区资源…' : 'Loading workspace resources…'}
      </p>
    );
  if (builder.catalog.isError)
    return (
      <div className="mx-auto max-w-xl space-y-4 py-12">
        <ManagedErrorAlert>{errorMessage(builder.catalog.error)}</ManagedErrorAlert>
        <Button onClick={() => void builder.catalog.refetch()}>{zh ? '重试' : 'Retry'}</Button>
      </div>
    );
  if (!active) return <BuilderEntry builder={builder} startHeader={startHeader} onEnter={() => setActive(true)} />;
  return (
    <BuilderWorkspace
      builder={builder}
      workspaceId={workspaceId}
      orgUuid={orgUuid}
      accountId={accountId}
      onBack={() => {
        builder.cancel();
        setActive(false);
      }}
    />
  );
}

function BuilderWorkspace({
  builder,
  workspaceId,
  orgUuid,
  accountId,
  onBack,
}: {
  builder: Builder;
  workspaceId: string;
  orgUuid: string;
  accountId: string;
  onBack: () => void;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const [panel, setPanel] = useState('task');
  const testing = Boolean(state.sessionID && !state.testClosed);
  const integrating = state.step === 'integrate' && state.gate?.name === 'show_integration_exits';
  const task = testing || integrating;
  return (
    <BuilderPanels builder={builder} workspaceId={workspaceId}>
      <section className="mx-auto flex min-h-0 w-full min-w-0 max-w-[1440px] flex-1 flex-col gap-4">
        <BuilderHeader builder={builder} task={task} onBack={onBack} />
        {task && <BuilderPanelSwitch panel={panel} onChange={setPanel} testing={testing} />}
        <div
          className={`builder-content-grid grid min-h-0 min-w-0 flex-1 gap-6 ${task ? 'xl:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]' : 'xl:grid-cols-[minmax(0,1fr)_280px]'}`}
        >
          <div
            id="builder-build-panel"
            className={`${task && panel !== 'build' ? 'hidden xl:flex' : 'flex'} mx-auto w-full max-w-[880px] min-h-0 min-w-0 flex-col gap-4 pb-3`}
          >
            <BuilderConversation builder={builder} workspaceId={workspaceId} orgUuid={orgUuid} task={task} />
          </div>
          {testing && (
            <div
              id="builder-task-panel"
              className={`builder-secondary-panel ${panel === 'task' ? 'flex' : 'hidden xl:flex'} min-h-0 min-w-0 flex-col`}
            >
              <BuilderTest key={state.sessionID} builder={builder} workspaceId={workspaceId} accountId={accountId} />
            </div>
          )}
          {integrating && (
            <section
              id="builder-task-panel"
              aria-label={zh ? '应用接入' : 'Application integration'}
              className={`builder-secondary-panel ${panel === 'task' ? 'block' : 'hidden xl:block'} min-h-0 min-w-0 overflow-y-auto rounded-xl border-[0.5px] border-border bg-card p-4`}
            >
              <h2 className="mb-5 text-base font-medium">{zh ? '应用接入' : 'Application integration'}</h2>
              <BuilderIntegration state={state} workspaceId={workspaceId} accountId={accountId} />
            </section>
          )}
          {!task && state.initialized && (
            <div className="builder-secondary-panel hidden min-h-0 xl:block">
              <BuilderInspector builder={builder} workspaceId={workspaceId} />
            </div>
          )}
        </div>
      </section>
    </BuilderPanels>
  );
}

function BuilderHeader({ builder, task, onBack }: { builder: Builder; task: boolean; onBack: () => void }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const name = state.agent?.name ?? state.config.name;
  return (
    <header className="flex min-h-10 shrink-0 items-center gap-2">
      <Button
        variant="ghost"
        size="icon"
        disabled={builder.draftEditing}
        aria-label={zh ? '返回快速开始' : 'Back to quickstart'}
        onClick={onBack}
      >
        <ArrowLeft />
      </Button>
      <h1 title={name} className="min-w-0 flex-1 truncate text-base font-medium">
        {name}
      </h1>
      {state.initialized && <BuilderConfiguration task={task} />}
    </header>
  );
}

function BuilderConversation({
  builder,
  workspaceId,
  orgUuid,
  task,
}: {
  builder: Builder;
  workspaceId: string;
  orgUuid: string;
  task: boolean;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  return (
    <>
      {task && <h2 className="shrink-0 text-sm font-medium">{zh ? '构建对话' : 'Build conversation'}</h2>}
      <MessageScrollerProvider autoScroll defaultScrollPosition="end">
        <MessageScroller className="min-h-0 flex-1">
          <MessageScrollerViewport className="py-4">
            <MessageScrollerContent className="mx-auto w-full max-w-[760px] space-y-5">
              <BuilderHistory messages={state.messages} pendingToolId={state.gate?.id} busy={builder.busy} />
              <BuilderStatus builder={builder} />
              <BuilderActiveGate builder={builder} workspaceId={workspaceId} orgUuid={orgUuid} />
              {state.initialized && !state.gate && !state.operation && !builder.busy && (
                <div className="flex justify-start pb-4">
                  <Button variant="outline" onClick={() => void builder.resume()}>
                    {zh ? '继续' : 'Continue'}
                    <ArrowRight aria-hidden="true" />
                  </Button>
                </div>
              )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
        </MessageScroller>
      </MessageScrollerProvider>
      <BuilderComposer builder={builder} />
    </>
  );
}

function BuilderConfiguration({ task }: { task: boolean }) {
  const { locale } = useI18n();
  const openPanel = useBuilderPanel();
  return (
    <Button variant="outline" className={task ? '' : 'xl:hidden'} onClick={() => openPanel({ kind: 'overview' })}>
      <Settings2 aria-hidden="true" />
      {locale === 'zh-CN' ? '配置' : 'Configuration'}
    </Button>
  );
}

function BuilderActiveGate({
  builder,
  workspaceId,
  orgUuid,
}: {
  builder: Builder;
  workspaceId: string;
  orgUuid: string;
}) {
  const { gate, operation } = builder.state;
  if (!gate || operation || ['show_integration_exits', 'await_test_run'].includes(gate.name)) return null;
  const inline = gate.name === 'offer_next_step';
  return (
    <Card className={inline ? 'bg-transparent py-0 ring-0' : ''}>
      <CardContent className={inline ? 'px-0' : undefined}>
        <BuilderGate key={gate.id} builder={builder} workspaceId={workspaceId} orgUuid={orgUuid} />
      </CardContent>
    </Card>
  );
}

function BuilderStatus({ builder }: { builder: Builder }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  return (
    <>
      {builder.busy && !state.operation && (
        <div role="status" className="flex min-w-0 items-start gap-3 text-sm leading-6">
          <BuilderAvatar state={builder.partial ? 'speaking' : 'thinking'} />
          <p className="min-w-0 break-words whitespace-pre-wrap">
            {builder.partial || (zh ? '正在处理…' : 'Working…')}
          </p>
        </div>
      )}
      {builder.error && <ManagedErrorAlert>{builder.error}</ManagedErrorAlert>}
      {state.operation && (
        <div className="space-y-3 text-sm">
          <p>{zh ? '正在核对保存结果，请勿重复创建。' : 'Check the pending save before creating another resource.'}</p>
          <Button disabled={builder.busy} onClick={() => void builder.recover()}>
            {zh ? '查询保存结果' : 'Check saved result'}
          </Button>
          {builder.canAbandonPending && (
            <>
              <p className="text-muted-foreground">
                {zh
                  ? '本次查询未发现保存结果。放弃只解除本地等待，不取消服务端请求；重试前请先检查资源列表。'
                  : 'No saved result was found. Abandoning only clears the local wait; it does not cancel the server request. Check resources before retrying.'}
              </p>
              <Button variant="outline" onClick={builder.abandonPending}>
                {zh ? '放弃等待保存' : 'Abandon pending save'}
              </Button>
            </>
          )}
        </div>
      )}
    </>
  );
}

function BuilderPanelSwitch({
  panel,
  onChange,
  testing,
}: {
  panel: string;
  onChange: (value: string) => void;
  testing: boolean;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  return (
    <ToggleGroup
      value={[panel]}
      onValueChange={(value) => {
        if (value.length) onChange(value[0]);
      }}
      aria-label={zh ? '工作区域' : 'Workspace panels'}
      className="shrink-0 xl:hidden"
    >
      <ToggleGroupItem value="build" aria-controls="builder-build-panel">
        {zh ? '构建' : 'Build'}
      </ToggleGroupItem>
      <ToggleGroupItem value="task" aria-controls="builder-task-panel">
        {testing ? (zh ? '试跑' : 'Test') : zh ? '接入' : 'Integrate'}
      </ToggleGroupItem>
    </ToggleGroup>
  );
}

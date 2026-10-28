import { useState, type ReactNode } from 'react';
import { ArrowUp, ArrowRight, Square, Sparkles } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { Textarea } from '../../../../shared/ui/textarea';
import { Card, CardContent } from '../../../../shared/ui/card';
import {
  MessageScroller,
  MessageScrollerContent,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '../../../../shared/ui/message-scroller';
import { ManagedErrorAlert } from '../../components/common';
import { errorMessage } from '../../utils';
import type { BuilderState } from './model';
import { BuilderHistory } from './BuilderHistory';
import { BuilderInspector } from './BuilderInspector';
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
    return (
      <>
        {startHeader}
        <BuilderStart builder={builder} composer={<BuilderComposer builder={builder} entry />} />
      </>
    );
  return (
    <section className="mx-auto flex min-h-0 w-full min-w-0 max-w-[1440px] flex-1 flex-col gap-4">
      <div className={`grid min-h-0 min-w-0 flex-1 gap-8 ${builderColumns(state)}`}>
        <div className="flex min-h-0 min-w-0 flex-col pb-3">
          <MessageScrollerProvider autoScroll defaultScrollPosition="end">
            <MessageScroller className="min-h-0 flex-1">
              <MessageScrollerViewport className="px-1 pt-8 pb-6">
                <MessageScrollerContent className="mx-auto w-full max-w-[680px] space-y-6">
                  <BuilderHistory messages={state.messages} pendingToolId={state.gate?.id} />
                  <BuilderStatus builder={builder} />
                  <BuilderActiveGate builder={builder} workspaceId={workspaceId} orgUuid={orgUuid} />
                  {state.step === 'integrate' && state.gate?.name === 'show_integration_exits' && (
                    <BuilderIntegration state={state} workspaceId={workspaceId} accountId={accountId} />
                  )}
                  {state.initialized && !state.gate && !state.operation && !builder.busy && (
                    <div className="ml-7 flex justify-start pb-4">
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
        </div>
        {state.sessionID && !state.testClosed && (
          <BuilderTest key={state.sessionID} builder={builder} workspaceId={workspaceId} accountId={accountId} />
        )}
        {state.initialized && <BuilderInspector builder={builder} workspaceId={workspaceId} />}
      </div>
    </section>
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
  if (!gate || operation || gate.name === 'show_integration_exits') return null;
  const inline = gate.name === 'offer_next_step';
  return (
    <Card className={`ml-7 ${inline ? 'bg-transparent py-0 ring-0' : ''}`}>
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
      {' '}
      {builder.partial && (
        <div aria-live="polite" className="flex min-w-0 items-start gap-3 text-sm leading-6">
          <Sparkles aria-hidden="true" className="mt-1 size-4 shrink-0 text-muted-foreground" />
          <p className="min-w-0 whitespace-pre-wrap">{builder.partial}</p>
        </div>
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
      className={`mx-auto flex w-full gap-2 border-[0.5px] border-input bg-card p-3 focus-within:border-foreground ${entry ? 'max-w-[728px] flex-col rounded-2xl' : 'max-w-[680px] items-end rounded-2xl'}`}
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
    : 'lg:grid-cols-[minmax(0,1fr)_300px]';
}

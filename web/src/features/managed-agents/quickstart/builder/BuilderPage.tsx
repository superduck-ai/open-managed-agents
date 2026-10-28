import { useState, type ReactNode } from 'react';
import { ArrowLeft, ArrowRight, Sparkles } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
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
import { BuilderEntry } from './BuilderEntry';
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
  const { state } = builder;
  const [active, setActive] = useState(false);
  if (builder.catalog.isPending) return <section className="min-h-96" aria-busy="true" />;
  if (builder.catalog.isError)
    return (
      <div className="mx-auto max-w-xl space-y-4 py-12">
        <ManagedErrorAlert>{errorMessage(builder.catalog.error)}</ManagedErrorAlert>
        <Button onClick={() => void builder.catalog.refetch()}>{zh ? '重试' : 'Retry'}</Button>
      </div>
    );
  if (!active) return <BuilderEntry builder={builder} startHeader={startHeader} onEnter={() => setActive(true)} />;
  return (
    <section className="mx-auto flex min-h-0 w-full min-w-0 max-w-[1440px] flex-1 flex-col gap-6">
      <header className="flex h-12 shrink-0 items-center gap-2">
        <Button
          variant="ghost"
          size="icon"
          aria-label={zh ? '返回快速开始' : 'Back to quickstart'}
          onClick={() => {
            builder.cancel();
            setActive(false);
          }}
        >
          <ArrowLeft />
        </Button>
        <h1 className="min-w-0 truncate text-base font-medium">{state.agent?.name ?? state.config.name}</h1>
      </header>
      <div className={`grid min-h-0 min-w-0 flex-1 gap-8 ${builderColumns(state)}`}>
        <div className="flex min-h-0 min-w-0 flex-col pb-3">
          <MessageScrollerProvider autoScroll defaultScrollPosition="end">
            <MessageScroller className="min-h-0 flex-1">
              <MessageScrollerViewport className="px-1 pt-[clamp(1.5rem,6vh,4rem)] pb-6">
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

function builderColumns(state: BuilderState) {
  if (!state.initialized) return '';
  return state.sessionID && !state.testClosed
    ? 'xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_240px]'
    : 'lg:grid-cols-[minmax(0,1fr)_300px]';
}

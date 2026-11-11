import { useQuery } from '@tanstack/react-query';
import { ChevronDown, Info } from 'lucide-react';
import type { ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../shared/ui/collapsible';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import {
  evaluationError,
  formatScore,
  getEvaluationItemTrace,
  type EvaluationItem,
  type EvaluationTraceStep,
} from './api';
import { formatEvaluationTime, shortenId } from './evaluationChrome';
import { itemOutcomeLabel } from './evaluationLabels';
import { itemOutcome } from './taskReport';
import { useEvaluationScope } from './useEvaluationScope';

const stepLabels: Record<EvaluationTraceStep['type'], readonly [string, string]> = {
  user: ['evaluations.traceUser', 'User question'],
  answer: ['evaluations.traceAnswer', 'Agent answer'],
  tool_use: ['evaluations.traceToolUse', 'Tool call'],
  tool_result: ['evaluations.traceToolResult', 'Tool result'],
  error: ['evaluations.traceError', 'Run error'],
};

function TraceStep({ step, index }: { step: EvaluationTraceStep; index: number }) {
  const { msg } = useI18n();
  const label = stepLabels[step.type];
  const code = step.type === 'tool_use' || step.type === 'tool_result';
  return (
    <li className="grid grid-cols-[1.5rem_minmax(0,1fr)] gap-3">
      <span className="grid size-6 place-items-center rounded-full bg-muted text-xs tabular-nums">{index}</span>
      <div className="grid min-w-0 gap-1.5">
        <div className="flex flex-wrap items-center gap-x-2 text-sm">
          <span className={step.type === 'error' ? 'font-medium text-destructive' : 'font-medium'}>
            {msg(label[0], label[1])}
          </span>
          {step.name ? <span className="font-mono text-xs text-muted-foreground">{step.name}</span> : null}
          <span className="ml-auto text-xs text-muted-foreground tabular-nums">
            {formatEvaluationTime(step.created_at)}
          </span>
        </div>
        {step.text ? (
          <pre
            className={`max-h-64 overflow-auto rounded-lg bg-muted/60 px-3 py-2 text-sm break-words whitespace-pre-wrap ${
              code ? 'font-mono text-xs' : 'font-sans'
            }`}
          >
            {step.text}
          </pre>
        ) : null}
      </div>
    </li>
  );
}

function TraceSteps({ taskId, item }: { taskId: string; item: EvaluationItem }) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const trace = useQuery({
    queryKey: ['evaluation-trace', scope.orgUuid, scope.workspaceId, taskId, item.id],
    queryFn: () => getEvaluationItemTrace(scope.orgUuid, scope.workspaceId, taskId, item.id),
    enabled: scope.ready,
    refetchInterval: itemOutcome(item) === 'running' ? 3000 : false,
  });
  if (trace.isLoading) return <p className="text-sm text-muted-foreground">{msg('evaluations.loading', 'Loading')}</p>;
  if (trace.isError) return <p className="text-sm text-destructive">{evaluationError(trace.error)}</p>;
  const steps = trace.data?.steps ?? [];
  const answered = steps.some((step) => step.type === 'answer');
  return (
    <div className="grid gap-4">
      {steps.length > 0 ? (
        <ol className="grid gap-4">
          {steps.map((step, index) => (
            <TraceStep key={`${step.created_at}-${index}`} step={step} index={index + 1} />
          ))}
        </ol>
      ) : (
        <p className="text-sm text-muted-foreground">{msg('evaluations.traceEmpty', 'No run activity yet')}</p>
      )}
      {!answered ? (
        <p className="flex items-center gap-2 rounded-lg bg-muted/60 px-3 py-2 text-sm text-muted-foreground">
          <Info className="size-4 shrink-0" aria-hidden />
          {msg('evaluations.noFinalAnswer', 'No final answer in this run')}
        </p>
      ) : null}
    </div>
  );
}

function PromptBlock({ title, text }: { title: string; text: string }) {
  if (!text) return null;
  return (
    <Collapsible defaultOpen className="grid gap-1.5">
      <CollapsibleTrigger className="group flex w-full items-center justify-between text-sm font-medium">
        {title}
        <ChevronDown className="size-4 text-muted-foreground transition-transform group-data-[panel-open]:rotate-180" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="max-h-64 overflow-auto rounded-lg bg-muted/60 px-3 py-2 font-mono text-xs break-words whitespace-pre-wrap">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ScoringExchange({ item }: { item: EvaluationItem }) {
  const { msg } = useI18n();
  const score = item.score;
  if (!score?.judge_system && !score?.judge_prompt && !score?.judge_response) return null;
  return (
    <div className="grid gap-3">
      <h3 className="text-sm font-medium">{msg('evaluations.judgeExchange', 'Scoring call')}</h3>
      <PromptBlock title={msg('evaluations.judgeSystem', 'System prompt')} text={score.judge_system} />
      <PromptBlock title={msg('evaluations.judgePrompt', 'Scoring prompt')} text={score.judge_prompt} />
      <PromptBlock title={msg('evaluations.judgeResponse', 'Model response')} text={score.judge_response} />
    </div>
  );
}

function TraceFact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function TraceFacts({ item, onOpenSession }: { item: EvaluationItem; onOpenSession: (id: string) => void }) {
  const { msg } = useI18n();
  const scored = item.score?.status === 'completed';
  return (
    <dl className="grid grid-cols-3 gap-3 rounded-xl border border-border p-4 text-sm">
      <TraceFact label={msg('evaluations.score', 'Score')}>
        <span className="tabular-nums">{scored ? formatScore(item.score?.score) : '—'}</span>
      </TraceFact>
      <TraceFact label={msg('evaluations.result', 'Result')}>{itemOutcomeLabel(itemOutcome(item), msg)}</TraceFact>
      <TraceFact label={msg('evaluations.sessionId', 'Session ID')}>
        {item.session_id ? (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto px-0 font-mono text-xs"
            onClick={() => onOpenSession(item.session_id)}
          >
            {shortenId(item.session_id)}
          </Button>
        ) : (
          <span className="text-muted-foreground">{msg('evaluations.noSession', 'No session')}</span>
        )}
      </TraceFact>
      {item.score?.judge_model ? (
        <TraceFact label={msg('evaluations.judgeModel', 'Judge model')}>
          <span className="font-mono text-xs break-all">{item.score.judge_model}</span>
        </TraceFact>
      ) : null}
      {item.score?.judge_prompt_version ? (
        <TraceFact label={msg('evaluations.judgePromptVersion', 'Scoring prompt')}>
          <span className="font-mono text-xs">{item.score.judge_prompt_version}</span>
        </TraceFact>
      ) : null}
    </dl>
  );
}

export function TaskTraceSheet({
  taskId,
  item,
  onOpenChange,
  onOpenSession,
}: {
  taskId: string;
  item: EvaluationItem | null;
  onOpenChange: (open: boolean) => void;
  onOpenSession: (id: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <Sheet open={item !== null} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 overflow-hidden p-0">
        <SheetHeader className="border-b border-border px-6 py-4 pr-12">
          <SheetTitle className="text-base leading-6">{msg('evaluations.trace', 'Trace')}</SheetTitle>
          <SheetDescription className="font-mono text-xs">{item ? shortenId(item.id) : ''}</SheetDescription>
        </SheetHeader>
        {item ? (
          <div className="grid min-h-0 flex-1 content-start gap-5 overflow-y-auto px-6 py-5">
            <p className="text-base leading-6 font-medium break-words whitespace-pre-wrap">{item.query}</p>
            <TraceFacts item={item} onOpenSession={onOpenSession} />
            <ScoringExchange item={item} />
            {item.score?.explanation ? (
              <div className="grid gap-1">
                <h3 className="text-sm font-medium">{msg('evaluations.reason', 'Scoring rationale')}</h3>
                <p className="text-sm break-words whitespace-pre-wrap text-muted-foreground">
                  {item.score.explanation}
                </p>
              </div>
            ) : null}
            <div className="grid gap-3">
              <div>
                <h3 className="text-sm font-medium">{msg('evaluations.process', 'Run steps')}</h3>
                <p className="text-xs text-muted-foreground">
                  {msg('evaluations.traceHint', 'In chronological order')}
                </p>
              </div>
              <TraceSteps taskId={taskId} item={item} />
            </div>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

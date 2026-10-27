import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { listSessionEvents, retrieveSessionDetailSession } from '../managed-agents/api';
import { type EvaluationCase, type EvaluationExperimentScore } from './api';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import {
  caseResultTrace,
  checkpointLines,
  detailsSnippet,
  formatElapsed,
  scoreHundredLabel,
  type CaseResultTrace,
} from './caseResultView';
import { displayName, type ExperimentCaseRow, type ExperimentNames } from './experimentSummary';

export function CaseResultDrawer({
  row,
  caseItem,
  names,
  workspaceId,
  onOpenChange,
}: {
  row: ExperimentCaseRow | null;
  caseItem: EvaluationCase | undefined;
  names: ExperimentNames;
  workspaceId: string;
  onOpenChange: (open: boolean) => void;
}) {
  const { msg } = useI18n();
  const sessionId = row?.sessionId ?? '';
  const traceQuery = useQuery({
    queryKey: ['evaluation-case-result', workspaceId, sessionId],
    enabled: row !== null && sessionId.length > 0,
    queryFn: () => loadCaseResultSession(sessionId, workspaceId),
  });
  const trace = caseResultTrace(traceQuery.data?.session, traceQuery.data?.events ?? []);
  const caseName = caseItem?.name || (row ? displayName(names.cases, row.caseId) : '');
  const turns = caseItem?.turns ?? [];
  const checkpoints = checkpointLines(caseItem?.checkpoints);
  return (
    <Sheet open={row !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="gap-0 overflow-y-auto">
        <SheetHeader className="border-b px-5 py-4 pr-12">
          <SheetTitle>{msg('evaluations.caseDetail', 'Case')}</SheetTitle>
        </SheetHeader>
        {row ? (
          <div className="flex flex-col px-5 py-5">
            <ResultSection title={msg('evaluations.caseContent', 'Case')}>
              <ResultFacts>
                <ResultRow label={msg('evaluations.name', 'Name')}>{caseName || '—'}</ResultRow>
                <ResultRow label={msg('evaluations.descriptionField', 'Description')}>{caseItem?.desc || '—'}</ResultRow>
              </ResultFacts>
              <ProseBlock label={msg('evaluations.caseInput', 'Input')}>
                {turns.length === 0 ? (
                  '—'
                ) : (
                  <div className="flex flex-col gap-4">
                    {turns.map((turn, index) => (
                      <div key={`${index}-${turn}`}>
                        <p className="text-sm text-muted-foreground">
                          {msg('evaluations.caseTurn', 'Turn {index}', { index: index + 1 })}
                        </p>
                        <p className="mt-1 leading-6 whitespace-pre-wrap">{turn}</p>
                      </div>
                    ))}
                  </div>
                )}
              </ProseBlock>
              <ProseBlock label={msg('evaluations.checkpoints', 'Checkpoints')}>
                {checkpoints.length === 0 ? (
                  '—'
                ) : (
                  <ul className="list-disc space-y-1 pl-5 leading-6">
                    {checkpoints.map((line) => (
                      <li key={line}>{line}</li>
                    ))}
                  </ul>
                )}
              </ProseBlock>
            </ResultSection>
            <ResultSection title={msg('evaluations.caseResultAgent', 'Agent result')}>
              <ResultFacts>
                <ResultRow label={msg('evaluations.status', 'Status')}>
                  <CaseStatus status={row.status} />
                </ResultRow>
                <ResultRow label={msg('evaluations.session', 'Session')}>
                  {sessionId ? (
                    <span className="inline-flex max-w-full flex-wrap items-baseline gap-x-3 gap-y-1">
                      <a
                        className="truncate font-mono text-sm hover:underline"
                        href={`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(sessionId)}`}
                      >
                        {sessionId}
                      </a>
                      {trace.elapsedSeconds !== undefined ? (
                        <span className="text-muted-foreground">
                          {msg('evaluations.caseResultElapsed', 'Duration')} {formatElapsed(trace.elapsedSeconds)}
                        </span>
                      ) : null}
                    </span>
                  ) : (
                    '—'
                  )}
                </ResultRow>
                <ResultRow label={msg('evaluations.caseResultRounds', 'Rounds')}>{trace.rounds}</ResultRow>
                <ResultRow label={msg('evaluations.caseResultTools', 'Tool calls')}>{trace.toolCalls}</ResultRow>
                <ResultRow label={msg('evaluations.caseResultTokens', 'Tokens (input / output)')}>
                  {tokenLabel(trace)}
                </ResultRow>
              </ResultFacts>
              <ProseBlock label={msg('evaluations.caseResultOutput', 'Final output')}>
                <p className="max-w-[65ch] leading-6 whitespace-pre-wrap">{trace.output || row.error || '—'}</p>
              </ProseBlock>
            </ResultSection>
            <ResultSection title={msg('evaluations.caseResultTrace', 'Agent trace')}>
              {trace.messages.length === 0 ? (
                <p className="text-sm text-muted-foreground">—</p>
              ) : (
                <div className="flex flex-col gap-3">
                  {trace.messages.map((message, index) => (
                    <div
                      key={`${message.role}-${index}`}
                      className={
                        message.role === 'agent'
                          ? 'rounded-lg border bg-background px-3 py-3'
                          : 'rounded-lg bg-muted/50 px-3 py-3'
                      }
                    >
                      <p className="text-sm font-medium text-muted-foreground">{message.role}</p>
                      <p className="mt-1 max-w-[65ch] leading-6 whitespace-pre-wrap">{message.text}</p>
                    </div>
                  ))}
                </div>
              )}
            </ResultSection>
            {row.scores.map((score) => (
              <ScoreSection key={score.id} score={score} names={names} />
            ))}
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function ScoreSection({ score, names }: { score: EvaluationExperimentScore; names: ExperimentNames }) {
  const { msg } = useI18n();
  const hundred = scoreHundredLabel(score.score);
  const details = detailsSnippet(score.details);
  return (
    <ResultSection title={msg('evaluations.caseResultScore', 'Score')}>
      <div className="flex items-center justify-between gap-3">
        <CaseStatus status={score.status} />
        <p className="text-lg font-semibold tabular-nums tracking-tight">
          {msg('evaluations.caseResultScoreValue', 'Score {score}', { score: hundred || '—' })}
        </p>
      </div>
      {names.evaluators.size > 1 ? (
        <p className="text-sm text-muted-foreground">{displayName(names.evaluators, score.evaluator_id)}</p>
      ) : null}
      <ProseBlock label={msg('evaluations.caseResultReason', 'Reason')}>
        <p className="max-w-[65ch] leading-6 whitespace-pre-wrap">{score.explanation || score.error || '—'}</p>
      </ProseBlock>
      {details ? (
        <ProseBlock label={msg('evaluations.caseResultDetails', 'Details')}>
          <p className="font-mono text-sm leading-6 break-all text-muted-foreground">{details}</p>
        </ProseBlock>
      ) : null}
    </ResultSection>
  );
}

function CaseStatus({ status }: { status: string }) {
  const { msg } = useI18n();
  if (status === 'completed') {
    return (
      <span className="inline-flex h-5 items-center rounded-md border border-[var(--success)]/30 bg-[var(--success-bg)] px-1.5 text-xs font-medium text-[var(--success)]">
        {msg('evaluations.caseSucceeded', 'Succeeded')}
      </span>
    );
  }
  return <EvaluationStatusBadge status={status} />;
}

function ResultSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-4 border-t py-6 first:border-t-0 first:pt-0 last:pb-0">
      <h2 className="text-sm font-medium">{title}</h2>
      {children}
    </section>
  );
}

function ResultFacts({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-[max-content_minmax(0,1fr)] items-baseline gap-x-8 gap-y-2.5">{children}</dl>;
}

function ResultRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </>
  );
}

function ProseBlock({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <p className="text-sm text-muted-foreground">{label}</p>
      <div className="rounded-lg bg-muted/40 px-3 py-3 text-sm">{children}</div>
    </div>
  );
}

function tokenLabel(trace: CaseResultTrace) {
  if (trace.inputTokens === undefined && trace.outputTokens === undefined) {
    return '—';
  }
  return `${trace.inputTokens ?? '—'} / ${trace.outputTokens ?? '—'}`;
}

async function loadCaseResultSession(sessionId: string, workspaceId: string) {
  const [sessionResult, eventResult] = await Promise.allSettled([
    retrieveSessionDetailSession(sessionId, workspaceId),
    listSessionEvents(sessionId, workspaceId, 'asc', 100),
  ]);
  return {
    session: sessionResult.status === 'fulfilled' ? sessionResult.value : null,
    events: eventResult.status === 'fulfilled' ? eventResult.value.data : [],
  };
}

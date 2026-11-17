import { Download } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { ToggleGroup, ToggleGroupItem } from '../../shared/ui/toggle-group';
import { formatScore, type EvaluationItem, type EvaluationTask } from './api';
import { downloadBlob } from './datasetSample';
import { itemOutcomeLabel, OUTCOME_TONES } from './evaluationLabels';
import { TaskSection } from './TaskSection';
import { TaskTraceSheet } from './TaskTraceSheet';
import {
  filterItems,
  ITEM_OUTCOMES,
  itemOutcome,
  itemReason,
  itemsCsv,
  type ItemFilter,
  type ItemOutcome,
} from './taskReport';

const FILTERS: ItemFilter[] = ['all', ...ITEM_OUTCOMES];

function ClampText({ text, empty }: { text: string; empty: string }) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  if (!text) return <span className="text-muted-foreground">{empty}</span>;
  const long = text.length > 80 || text.includes('\n');
  return (
    <div className="grid justify-items-start gap-1">
      <p className={`text-sm break-words whitespace-pre-wrap ${open ? '' : 'line-clamp-2'}`}>{text}</p>
      {long ? (
        <button type="button" className="text-xs text-primary hover:underline" onClick={() => setOpen(!open)}>
          {open ? msg('evaluations.showLess', 'Show less') : msg('evaluations.showFull', 'Show all')}
        </button>
      ) : null}
    </div>
  );
}

function OutcomeBadge({ outcome }: { outcome: ItemOutcome }) {
  const { msg } = useI18n();
  return (
    <Badge variant="secondary" className={OUTCOME_TONES[outcome].text}>
      {itemOutcomeLabel(outcome, msg)}
    </Badge>
  );
}

function AnswerCell({ item, outcome }: { item: EvaluationItem; outcome: ItemOutcome }) {
  const { msg } = useI18n();
  if (item.score?.answer) return <ClampText text={item.score.answer} empty="" />;
  if (outcome === 'running') return <span className="text-muted-foreground">—</span>;
  return (
    <Badge variant="secondary" className={OUTCOME_TONES.failed.text}>
      {msg('evaluations.noAnswer', 'No final answer')}
    </Badge>
  );
}

function ItemRow({ item, outcome, onTrace }: { item: EvaluationItem; outcome: ItemOutcome; onTrace: () => void }) {
  const { msg } = useI18n();
  const dash = '—';
  return (
    <TableRow className="align-top">
      <TableCell className="max-w-[260px] px-3 py-3 whitespace-normal">
        <ClampText text={item.query} empty={dash} />
      </TableCell>
      <TableCell className="max-w-[260px] px-3 py-3 whitespace-normal">
        <AnswerCell item={item} outcome={outcome} />
      </TableCell>
      <TableCell className="px-3 py-3 tabular-nums">
        {item.score?.status === 'completed' ? formatScore(item.score.score) : dash}
      </TableCell>
      <TableCell className="px-3 py-3">
        <OutcomeBadge outcome={outcome} />
      </TableCell>
      <TableCell className="max-w-[280px] px-3 py-3 whitespace-normal">
        <ClampText text={itemReason(item)} empty={dash} />
      </TableCell>
      <TableCell className="max-w-[240px] px-3 py-3 whitespace-normal">
        <ClampText text={item.acceptance || item.expected_answer} empty={dash} />
      </TableCell>
      <TableCell className="px-3 py-3">
        <Button type="button" variant="link" size="sm" className="h-auto px-0" onClick={onTrace}>
          {msg('evaluations.viewTrace', 'View trace')}
        </Button>
      </TableCell>
    </TableRow>
  );
}

export function TaskItemsTable({
  task,
  counts,
  onOpenSession,
}: {
  task: EvaluationTask;
  counts: Record<ItemOutcome, number>;
  onOpenSession: (id: string) => void;
}) {
  const { msg } = useI18n();
  const [filter, setFilter] = useState<ItemFilter>('all');
  const [tracedId, setTracedId] = useState('');
  const traced = task.items?.find((item) => item.id === tracedId) ?? null;
  const rows = filterItems(task.items, filter, task.status);
  const total = task.items?.length ?? 0;
  const headers = [
    msg('evaluations.query', 'Query'),
    msg('evaluations.agentAnswer', 'Agent answer'),
    msg('evaluations.autoScore', 'Auto score'),
    msg('evaluations.result', 'Result'),
    msg('evaluations.reason', 'Scoring rationale'),
    msg('evaluations.acceptance', 'Acceptance'),
  ];
  function download() {
    const text = itemsCsv(
      task,
      [
        headers[0],
        headers[1],
        headers[2],
        headers[3],
        headers[4],
        msg('evaluations.expectedAnswer', 'Expected answer'),
        headers[5],
        msg('evaluations.sessionId', 'Session ID'),
      ],
      (outcome) => itemOutcomeLabel(outcome, msg),
    );
    downloadBlob(new Blob([`\uFEFF${text}`], { type: 'text/csv;charset=utf-8' }), `${task.id}.csv`);
  }
  return (
    <TaskSection
      index="03"
      title={msg('evaluations.details', 'Case details')}
      hint={msg('evaluations.detailsHint', 'Review each answer, score, and scoring rationale.')}
      actions={
        <Button type="button" variant="outline" size="sm" disabled={total === 0} onClick={download}>
          <Download data-icon="inline-start" />
          {msg('evaluations.downloadCsv', 'Download CSV')}
        </Button>
      }
    >
      <ToggleGroup
        aria-label={msg('evaluations.result', 'Result')}
        value={[filter]}
        onValueChange={(next) => next.length && setFilter(next[0] as ItemFilter)}
        size="sm"
      >
        {FILTERS.map((value) => (
          <ToggleGroupItem key={value} value={value} className="px-3">
            {value === 'all' ? msg('evaluations.all', 'All') : itemOutcomeLabel(value, msg)}
            <span className="text-muted-foreground tabular-nums">{value === 'all' ? total : counts[value]}</span>
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <div className="overflow-x-auto rounded-xl border border-border">
        <Table className="min-w-[1120px]">
          <TableHeader>
            <TableRow>
              {headers.map((header) => (
                <TableHead key={header} className="h-10 px-3">
                  {header}
                </TableHead>
              ))}
              <TableHead className="h-10 px-3">{msg('evaluations.actions', 'Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                outcome={itemOutcome(item, task.status)}
                onTrace={() => setTracedId(item.id)}
              />
            ))}
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={headers.length + 1} className="py-10 text-center text-sm text-muted-foreground">
                  {msg('evaluations.noItems', 'No matching cases')}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </div>
      <TaskTraceSheet
        taskId={task.id}
        taskStatus={task.status}
        item={traced}
        onOpenChange={(open) => !open && setTracedId('')}
        onOpenSession={onOpenSession}
      />
    </TaskSection>
  );
}

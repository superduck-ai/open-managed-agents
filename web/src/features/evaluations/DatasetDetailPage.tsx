import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { MoreHorizontal } from 'lucide-react';
import { useState, type FormEvent } from 'react';

import { listManagedEntities } from '../managed-agents/api';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { CopyButton } from '../../shared/ui/copy-button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Field, FieldDescription, FieldTitle } from '../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import {
  deleteEvaluationCase,
  evaluationError,
  getEvaluationDataset,
  importEvaluationSession,
  listEvaluationCases,
  listSessionTurns,
  type EvaluationCase,
} from './api';
import { AcceptanceChecklist } from './AcceptanceChecklist';
import { SessionTurnTransfer } from './SessionTurnTransfer';
import { CaseEditorDialog } from './CaseEditorDialog';
import { acceptanceItems, joinAcceptance } from './caseChecklist';
import { shortenId } from './evaluationChrome';
import { useEvaluationScope } from './useEvaluationScope';

export function DatasetDetailPage() {
  const { msg } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const scope = useEvaluationScope();
  const queryClient = useQueryClient();
  const datasetId = decodeURIComponent(location.pathname.match(/\/evaluations\/datasets\/([^/]+)/)?.[1] ?? '');
  const [importOpen, setImportOpen] = useState(false);
  const [editor, setEditor] = useState<EvaluationCase | null>(null);
  const [creating, setCreating] = useState(false);
  const dataset = useQuery({
    queryKey: ['evaluation-dataset', scope.orgUuid, scope.workspaceId, datasetId],
    queryFn: () => getEvaluationDataset(scope.orgUuid, scope.workspaceId, datasetId),
    enabled: scope.ready && Boolean(datasetId),
  });
  const remove = useMutation({
    mutationFn: (caseId: string) => deleteEvaluationCase(scope.orgUuid, scope.workspaceId, datasetId, caseId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['evaluation-cases', scope.orgUuid, scope.workspaceId, datasetId],
      });
      await queryClient.invalidateQueries({
        queryKey: ['evaluation-dataset', scope.orgUuid, scope.workspaceId, datasetId],
      });
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', scope.orgUuid, scope.workspaceId] });
    },
  });
  const cases = useQuery({
    queryKey: ['evaluation-cases', scope.orgUuid, scope.workspaceId, datasetId],
    queryFn: () => listEvaluationCases(scope.orgUuid, scope.workspaceId, datasetId),
    enabled: scope.ready && Boolean(datasetId),
  });
  const rows = cases.data?.data ?? [];
  const datasetName = dataset.data?.name || msg('evaluations.datasetDetail', 'Dataset');
  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <p className="mb-3 text-sm text-muted-foreground">
        <button
          type="button"
          className="hover:text-foreground"
          onClick={() => void navigate({ href: `/workspaces/${encodeURIComponent(scope.workspaceId)}/evaluations` })}
        >
          {msg('evaluations.datasets', 'Datasets')}
        </button>
        <span> / </span>
        <span className="text-foreground">{datasetName}</span>
      </p>
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl leading-8 font-medium">{datasetName}</h1>
          <div className="mt-2 inline-flex items-center gap-1 font-mono text-xs text-muted-foreground">
            {shortenId(datasetId)}
            <CopyButton value={datasetId} label={msg('evaluations.copyId', 'Copy ID')} />
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" onClick={() => setImportOpen(true)}>
            {msg('evaluations.importSession', 'Import session')}
          </Button>
          <Button type="button" onClick={() => setCreating(true)}>
            + {msg('evaluations.addCase', 'Add case')}
          </Button>
        </div>
      </div>
      <p className="mb-3 text-sm text-muted-foreground">
        {msg('evaluations.caseCount', '{count} cases', { count: rows.length })}
      </p>
      {cases.isError ? <p className="mb-4 text-sm text-destructive">{evaluationError(cases.error)}</p> : null}
      <div className="overflow-x-auto rounded-xl border border-border">
        <Table className="min-w-[1080px]">
          <TableHeader>
            <TableRow>
              <TableHead className="h-10 w-12 px-3">#</TableHead>
              <TableHead className="h-10 px-3">{msg('evaluations.query', 'Query')}</TableHead>
              <TableHead className="h-10 px-3">{msg('evaluations.expectedAnswer', 'Expected answer')}</TableHead>
              <TableHead className="h-10 px-3">{msg('evaluations.acceptance', 'What counts as fixed')}</TableHead>
              <TableHead className="h-10 px-3">{msg('evaluations.source', 'Source')}</TableHead>
              <TableHead className="h-10 px-3">{msg('evaluations.polarity', 'Label')}</TableHead>
              <TableHead className="h-10 px-3">{msg('evaluations.actions', 'Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((item, index) => (
              <CaseRow
                key={item.id}
                item={item}
                index={index + 1}
                unset={msg('evaluations.unsetAnswer', 'No expected answer')}
                noChecklist={msg('evaluations.noChecklist', 'No acceptance checks')}
                sessionLabel={msg('evaluations.sessionImport', 'Imported session')}
                manualLabel={msg('evaluations.manual', 'Manual')}
                editLabel={msg('evaluations.edit', 'Edit')}
                deleteLabel={msg('evaluations.delete', 'Delete')}
                moreLabel={msg('evaluations.moreActions', 'More actions')}
                positiveLabel={msg('evaluations.positive', 'Positive')}
                negativeLabel={msg('evaluations.negative', 'Negative')}
                onEdit={() => setEditor(item)}
                onDelete={() => {
                  if (
                    window.confirm(msg('evaluations.confirmDeleteCase', 'Delete this case? This cannot be undone.'))
                  ) {
                    remove.mutate(item.id);
                  }
                }}
              />
            ))}
          </TableBody>
        </Table>
      </div>
      <CaseEditorDialog
        open={creating || editor !== null}
        onOpenChange={(next) => {
          if (!next) {
            setCreating(false);
            setEditor(null);
          }
        }}
        orgUuid={scope.orgUuid}
        workspaceId={scope.workspaceId}
        datasetId={datasetId}
        item={editor}
      />
      <ImportSessionDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        orgUuid={scope.orgUuid}
        workspaceId={scope.workspaceId}
        datasetId={datasetId}
      />
    </section>
  );
}

function CaseRow({
  item,
  index,
  unset,
  noChecklist,
  sessionLabel,
  manualLabel,
  editLabel,
  deleteLabel,
  moreLabel,
  positiveLabel,
  negativeLabel,
  onEdit,
  onDelete,
}: {
  item: EvaluationCase;
  index: number;
  unset: string;
  noChecklist: string;
  sessionLabel: string;
  manualLabel: string;
  editLabel: string;
  deleteLabel: string;
  moreLabel: string;
  positiveLabel: string;
  negativeLabel: string;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const checkpoints = acceptanceItems(item.acceptance).filter(Boolean);
  return (
    <TableRow>
      <TableCell className="px-3 align-top tabular-nums">{index}</TableCell>
      <TableCell className="max-w-[280px] px-3 align-top whitespace-pre-wrap">{item.query}</TableCell>
      <TableCell className="max-w-[240px] px-3 align-top whitespace-pre-wrap text-muted-foreground">
        {item.expected_answer || unset}
      </TableCell>
      <TableCell className="max-w-[320px] px-3 align-top">
        {checkpoints.length === 0 ? (
          <p className="text-muted-foreground">{noChecklist}</p>
        ) : (
          <ol className="grid gap-1">
            {checkpoints.map((checkpoint, checkpointIndex) => (
              <li key={`${checkpointIndex}-${checkpoint}`} className="text-sm">
                {checkpointIndex + 1}. {checkpoint}
              </li>
            ))}
          </ol>
        )}
      </TableCell>
      <TableCell className="px-3 align-top">{item.source_session_id ? sessionLabel : manualLabel}</TableCell>
      <TableCell className="px-3 align-top">{polarityLabel(item.polarity, positiveLabel, negativeLabel)}</TableCell>
      <TableCell className="px-3 align-top">
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" aria-label={moreLabel} />}>
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}>{editLabel}</DropdownMenuItem>
            <DropdownMenuItem variant="destructive" onClick={onDelete}>
              {deleteLabel}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

function polarityLabel(polarity: string, positiveLabel: string, negativeLabel: string) {
  if (polarity === 'positive') return positiveLabel;
  if (polarity === 'negative') return negativeLabel;
  return polarity || '—';
}

function ImportSessionDialog({
  open,
  onOpenChange,
  orgUuid,
  workspaceId,
  datasetId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgUuid: string;
  workspaceId: string;
  datasetId: string;
}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const [sessionId, setSessionId] = useState('');
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [polarity, setPolarity] = useState('');
  const [checkpoints, setCheckpoints] = useState<string[]>(['']);
  const [error, setError] = useState('');
  const sessions = useInfiniteQuery({
    queryKey: ['evaluation-import-sessions', workspaceId],
    queryFn: ({ pageParam }) => listManagedEntities('sessions', workspaceId, pageParam),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_page,
    enabled: open && Boolean(workspaceId),
  });
  const turns = useQuery({
    queryKey: ['evaluation-session-turns', orgUuid, workspaceId, datasetId, sessionId],
    queryFn: () => listSessionTurns(orgUuid, workspaceId, datasetId, sessionId),
    enabled: open && sessionId.trim() !== '',
  });
  const turnRows = turns.data?.data ?? [];
  const importSession = useMutation({
    mutationFn: () =>
      importEvaluationSession(orgUuid, workspaceId, datasetId, {
        session_id: sessionId,
        turns: [...selected].sort((left, right) => left - right),
        polarity: polarity || undefined,
        acceptance: polarity === 'negative' ? joinAcceptance(checkpoints) : '',
      }),
    onSuccess: async () => {
      setSessionId('');
      setSelected(new Set());
      setPolarity('');
      setCheckpoints(['']);
      setError('');
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ['evaluation-cases', orgUuid, workspaceId, datasetId] });
      await queryClient.invalidateQueries({ queryKey: ['evaluation-datasets', orgUuid, workspaceId] });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });
  const sessionOptions = (sessions.data?.pages ?? [])
    .flatMap((page) => page.data)
    .map((item) => ({
      id: item.id,
      label: sessionOptionLabel(item),
    }));

  function chooseSession(next: string) {
    setSessionId(next);
    setSelected(new Set());
    setError('');
  }

  function choosePolarity(next: string) {
    setPolarity(next);
    if (next !== 'negative') setCheckpoints(['']);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (selected.size === 0) return;
    importSession.mutate();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100vh-4rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl">
        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={onSubmit}>
          <DialogHeader className="px-6 pt-6 pr-12">
            <DialogTitle>{msg('evaluations.importSession', 'Import session')}</DialogTitle>
            <DialogDescription>
              {msg('evaluations.importHint', 'Choose a session, then move the turns you want onto the right.')}
            </DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 flex-1 content-start gap-5 overflow-y-auto px-6 py-6">
            <Field>
              <FieldTitle>{msg('evaluations.chooseSession', 'Choose a session')}</FieldTitle>
              <Select
                value={sessionId || null}
                items={sessionOptions.map((option) => ({ value: option.id, label: option.label }))}
                onValueChange={(value) => chooseSession(value ?? '')}
                emptyLabel={msg('evaluations.emptySessions', 'No sessions yet')}
              >
                <SelectTrigger aria-label={msg('evaluations.chooseSession', 'Choose a session')} className="w-full">
                  <SelectValue placeholder={msg('evaluations.chooseSession', 'Choose a session')} />
                </SelectTrigger>
                <SelectContent>
                  {sessionOptions.map((option) => (
                    <SelectItem key={option.id} value={option.id}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {sessions.hasNextPage ? (
                <Button
                  type="button"
                  variant="outline"
                  disabled={sessions.isFetchingNextPage}
                  onClick={() => void sessions.fetchNextPage()}
                >
                  {msg('evaluations.loadMoreSessions', 'Load more sessions')}
                </Button>
              ) : null}
            </Field>
            <SessionTurnTransfer
              key={sessionId || 'none'}
              turns={turnRows}
              chosen={selected}
              onChosenChange={setSelected}
              status={transferStatus(sessionId, turns.isLoading, turns.isError, turnRows.length)}
              error={turns.isError ? evaluationError(turns.error) : ''}
            />
            {selected.size > 0 ? (
              <Field>
                <FieldTitle>{msg('evaluations.polarity', 'Label')}</FieldTitle>
                <FieldDescription>
                  {msg(
                    'evaluations.polarityHint',
                    'Only a negative example needs acceptance checks that say what counts as fixed.',
                  )}
                </FieldDescription>
                <Select
                  value={polarity || null}
                  items={[
                    { value: 'positive', label: msg('evaluations.positive', 'Positive') },
                    { value: 'negative', label: msg('evaluations.negative', 'Negative') },
                  ]}
                  onValueChange={(value) => choosePolarity(value ?? '')}
                >
                  <SelectTrigger aria-label={msg('evaluations.polarity', 'Label')} className="w-full">
                    <SelectValue placeholder={msg('evaluations.unlabeled', 'Unlabeled')} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="positive">{msg('evaluations.positive', 'Positive')}</SelectItem>
                    <SelectItem value="negative">{msg('evaluations.negative', 'Negative')}</SelectItem>
                  </SelectContent>
                </Select>
                {polarity === 'negative' ? <AcceptanceChecklist items={checkpoints} onChange={setCheckpoints} /> : null}
              </Field>
            ) : null}
          </div>
          {error ? <p className="px-6 pb-2 text-sm text-destructive">{error}</p> : null}
          <DialogFooter className="border-t border-border px-6 py-4">
            <DialogClose render={<Button type="button" variant="outline" disabled={importSession.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={importSession.isPending || selected.size === 0}>
              {msg('evaluations.importSession', 'Import session')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function transferStatus(sessionId: string, loading: boolean, failed: boolean, count: number) {
  if (!sessionId) return 'idle' as const;
  if (loading) return 'loading' as const;
  if (failed) return 'error' as const;
  if (count === 0) return 'empty' as const;
  return 'ready' as const;
}

function sessionOptionLabel(item: { id: string; title?: unknown }) {
  return typeof item.title === 'string' && item.title.trim() ? item.title.trim() : item.id;
}

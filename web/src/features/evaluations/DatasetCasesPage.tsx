import { ChevronDown, Search } from 'lucide-react';
import { useState, type FormEvent, type ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { CaseListPager, casePageSlice } from './CaseListPager';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import {
  evaluationErrorMessage,
  type EvaluationCase,
  type EvaluationCaseFromSessionsResult,
  type EvaluationDataset,
} from './api';
import { CaseDetailSheet } from './CaseDrawer';
import { PresetDatasetPage } from './PresetDatasetPage';
import { CopyIdButton } from './CopyIdButton';
import { DatasetVersionEditor } from './DatasetVersionEditor';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import { parseCheckpointsField, parseSessionIDs } from './sessionCase';
import { CheckpointsField, IncludeOutputFilesField, SessionCaseFields } from './SessionCaseFields';
import {
  datasetIdFromPath,
  evaluationsIndexHref,
  useCreateEvaluationCase,
  useCreateEvaluationCaseFromSession,
  useCreateEvaluationCasesFromSessions,
  useEvaluationCaseList,
  useImportEvaluationCases,
} from './useEvaluationDatasets';

type CaseAction = 'add' | 'jsonl' | 'session' | 'sessions';

export function DatasetCasesPage() {
  if (new URLSearchParams(window.location.search).get('preset') === '1') {
    return <PresetDatasetPage />;
  }
  return <CustomDatasetCasesPage />;
}

function CustomDatasetCasesPage() {
  const { msg } = useI18n();
  const datasetId = datasetIdFromPath();
  const { workspace, datasetQuery, casesQuery } = useEvaluationCaseList(datasetId);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);
  const cases = (casesQuery.data?.data ?? []).filter((item) =>
    matchesCaseQuery(search, [item.name, item.desc, ...item.turns]),
  );
  const visible = casePageSlice(cases, page);
  const version = datasetQuery.data?.version;
  const [openCase, setOpenCase] = useState<EvaluationCase | null>(null);
  const [editing, setEditing] = useState(false);
  const [action, setAction] = useState<CaseAction | null>(null);
  const dataset = datasetQuery.data;

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="datasets" />
      <header className="mb-3">
        <a
          className="text-sm text-muted-foreground hover:text-foreground"
          href={evaluationsIndexHref(workspace.workspaceId)}
        >
          {msg('evaluations.title', 'Datasets')}
        </a>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
          {dataset?.name ?? msg('evaluations.cases', 'Cases')}
        </h1>
      </header>
      <DatasetIdentity dataset={dataset} />
      <div className="flex flex-col gap-8">
        <DatasetFacts dataset={dataset} loadedCount={casesQuery.data?.data.length} />
        <VersionSection dataset={dataset} version={version} onEdit={() => setEditing(true)} />
        <section id="evaluation-cases" className="rounded-lg border">
          <div className="flex items-center justify-between gap-3 px-4 pt-4">
            <h2 className="text-sm font-medium">{msg('evaluations.caseList', 'Cases')}</h2>
            <AddCaseMenu onAction={setAction} />
          </div>
          <CurrentVersion dataset={dataset} version={version?.version} />
          <div className="px-4">
            <div className="relative mb-3 max-w-sm">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value);
                  setPage(0);
                }}
                placeholder={msg('evaluations.searchDatasetCases', 'Search cases')}
                aria-label={msg('evaluations.searchDatasetCases', 'Search cases')}
                className="h-9 bg-background pl-8"
              />
            </div>
          </div>
          <div className="max-h-80 overflow-auto">
          <Table className="table-fixed [&_td]:px-4 [&_td]:py-3 [&_th]:sticky [&_th]:top-0 [&_th]:z-10 [&_th]:bg-muted [&_th]:h-9 [&_th]:px-4">
            <TableHeader>
              <TableRow className="bg-muted hover:bg-muted">
                <TableHead className="w-40 text-xs font-normal text-muted-foreground">
                  {msg('evaluations.caseTitle', 'Name')}
                </TableHead>
                <TableHead className="text-xs font-normal text-muted-foreground">
                  {msg('evaluations.caseInput', 'Input')}
                </TableHead>
                <TableHead className="w-24 text-xs font-normal text-muted-foreground">
                  {msg('evaluations.caseMode', 'Mode')}
                </TableHead>
                <TableHead className="w-24 text-right text-xs font-normal text-muted-foreground">
                  {msg('evaluations.actions', 'Action')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <CaseRows
                cases={visible.items}
                isLoading={!workspace.workspaceReady || casesQuery.isLoading}
                errorMessage={casesQuery.error ? evaluationErrorMessage(casesQuery.error) : ''}
                onRetry={() => void casesQuery.refetch()}
                onOpen={setOpenCase}
              />
            </TableBody>
          </Table>
          </div>
          <CaseListPager
            total={cases.length}
            page={visible.page}
            pageCount={visible.pageCount}
            onPage={setPage}
          />
        </section>
      </div>
      <CaseDialog
        open={action === 'add'}
        title={msg('evaluations.addCase', 'Add case')}
        onClose={() => setAction(null)}
      >
        <AddCaseForm datasetId={datasetId} />
      </CaseDialog>
      <CaseDialog
        open={action === 'jsonl'}
        title={msg('evaluations.importJsonl', 'Import JSONL')}
        onClose={() => setAction(null)}
      >
        <ImportCasesForm datasetId={datasetId} />
      </CaseDialog>
      <CaseDialog
        open={action === 'session'}
        title={msg('evaluations.importFromSession', 'Import from session')}
        onClose={() => setAction(null)}
      >
        <ImportSessionCaseForm datasetId={datasetId} />
      </CaseDialog>
      <CaseDialog
        open={action === 'sessions'}
        title={msg('evaluations.importSessions', 'Import sessions')}
        onClose={() => setAction(null)}
      >
        <ImportSessionsForm datasetId={datasetId} />
      </CaseDialog>
      <CaseDetailSheet
        item={openCase}
        workspaceId={workspace.workspaceId}
        open={openCase !== null}
        onOpenChange={(open) => {
          if (!open) {
            setOpenCase(null);
          }
        }}
      />
      <DatasetVersionEditor
        open={editing}
        version={version?.version ?? 1}
        cases={casesQuery.data?.data ?? []}
        datasetId={datasetId}
        onOpenChange={setEditing}
        onAdd={() => setAction('add')}
      />
    </section>
  );
}

function CaseRows({
  cases,
  isLoading,
  errorMessage,
  onRetry,
  onOpen,
}: {
  cases: EvaluationCase[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
  onOpen: (item: EvaluationCase) => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={4} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={4}
        title={msg('evaluations.casesLoadFailed', 'Could not load cases')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (cases.length === 0) {
    return <TableEmptyRow colSpan={4}>{msg('evaluations.noCases', 'No cases yet.')}</TableEmptyRow>;
  }
  return cases.map((item) => (
    <TableRow key={item.id}>
      <TableCell className="w-40 font-medium">
        <button type="button" className="block w-full truncate text-left" onClick={() => onOpen(item)}>
          {item.name}
        </button>
      </TableCell>
      <TableCell className="text-muted-foreground">
        <span className="block truncate">{item.turns[0] ?? ''}</span>
      </TableCell>
      <TableCell className="text-muted-foreground">
        {item.turns.length > 1
          ? msg('evaluations.multiTurn', 'Multi-turn')
          : msg('evaluations.singleTurn', 'Single turn')}
      </TableCell>
      <TableCell className="text-right">
        <button
          type="button"
          className="text-sm text-muted-foreground hover:text-foreground"
          aria-label={`${msg('evaluations.viewCase', 'View')} ${item.name}`}
          onClick={() => onOpen(item)}
        >
          {msg('evaluations.viewCase', 'View')}
        </button>
      </TableCell>
    </TableRow>
  ));
}

function CaseDialog({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  if (!open) {
    return null;
  }
  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
    >
      <DialogContent className="max-h-[min(640px,80vh)] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {children}
      </DialogContent>
    </Dialog>
  );
}

function DatasetIdentity({ dataset }: { dataset: EvaluationDataset | undefined }) {
  const { msg } = useI18n();
  if (!dataset) {
    return null;
  }
  return (
    <div className="mb-4 flex items-center gap-2">
      <span className="inline-flex h-5 shrink-0 items-center rounded-md border border-[var(--success)]/30 bg-[var(--success-bg)] px-1.5 text-xs font-medium text-[var(--success)]">
        {msg('evaluations.customBadge', 'Custom')}
      </span>
      <CopyIdButton id={dataset.id} className="w-auto" />
    </div>
  );
}

function DatasetFacts({
  dataset,
  loadedCount,
}: {
  dataset: EvaluationDataset | undefined;
  loadedCount: number | undefined;
}) {
  const { msg } = useI18n();
  return (
    <section className="rounded-lg border px-5 py-5">
      <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-baseline gap-x-8 gap-y-4 text-sm">
        <dt className="text-muted-foreground">{msg('evaluations.descriptionField', 'Description')}</dt>
        <dd className="min-w-0 truncate">{dataset?.desc || '—'}</dd>
        <dt className="text-muted-foreground">{msg('evaluations.caseCount', 'Cases')}</dt>
        <dd>{factCaseCount(dataset, loadedCount)}</dd>
        <dt className="text-muted-foreground">{msg('evaluations.createdAt', 'Created')}</dt>
        <dd>{dateLabel(dataset?.created_at ?? '')}</dd>
      </dl>
    </section>
  );
}

function factCaseCount(dataset: EvaluationDataset | undefined, loadedCount: number | undefined) {
  if (dataset) {
    return dataset.version.case_count;
  }
  return loadedCount ?? 0;
}

function VersionSection({
  dataset,
  version,
  onEdit,
}: {
  dataset: EvaluationDataset | undefined;
  version: EvaluationDataset['version'] | undefined;
  onEdit: () => void;
}) {
  const { msg } = useI18n();
  return (
    <section className="rounded-lg border">
      <h2 className="px-4 pt-4 pb-3 text-sm font-medium">{msg('evaluations.versionList', 'Versions')}</h2>
      <Table className="table-fixed [&_td]:px-4 [&_th]:h-9 [&_th]:px-4">
        <TableHeader>
          <TableRow className="bg-muted hover:bg-muted">
            <TableHead className="w-16 text-xs font-normal text-muted-foreground">
              {msg('evaluations.versionColumn', 'Version')}
            </TableHead>
            <TableHead className="text-xs font-normal text-muted-foreground">{msg('evaluations.id', 'ID')}</TableHead>
            <TableHead className="w-28 text-xs font-normal text-muted-foreground">
              {msg('evaluations.status', 'Status')}
            </TableHead>
            <TableHead className="w-24 text-xs font-normal text-muted-foreground">
              {msg('evaluations.caseCount', 'Cases')}
            </TableHead>
            <TableHead className="w-32 text-xs font-normal text-muted-foreground">
              {msg('evaluations.publishedAt', 'Published')}
            </TableHead>
            <TableHead className="w-28 text-right text-xs font-normal text-muted-foreground">
              {msg('evaluations.actions', 'Action')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {version ? (
            <VersionRow dataset={dataset} version={version} onEdit={onEdit} />
          ) : (
            <TableEmptyRow colSpan={6}>{msg('common.loading', 'Loading...')}</TableEmptyRow>
          )}
        </TableBody>
      </Table>
    </section>
  );
}

function VersionRow({
  dataset,
  version,
  onEdit,
}: {
  dataset: { status?: string; created_at?: string } | undefined;
  version: { id: string; version: number; case_count: number };
  onEdit: () => void;
}) {
  const { msg } = useI18n();
  return (
    <TableRow>
      <TableCell>v{version.version}</TableCell>
      <TableCell>
        <CopyIdButton id={version.id} className="max-w-48" />
      </TableCell>
      <TableCell>
        <EvaluationStatusBadge status={dataset?.status ?? 'available'} />
      </TableCell>
      <TableCell>{version.case_count}</TableCell>
      <TableCell className="text-muted-foreground">{dateLabel(dataset?.created_at ?? '')}</TableCell>
      <TableCell className="text-right">
        <a className="text-sm text-muted-foreground hover:text-foreground" href="#evaluation-cases">
          {msg('evaluations.viewCase', 'View')}
        </a>
        <button
          type="button"
          className="ml-3 text-sm text-muted-foreground hover:text-foreground"
          aria-label={msg('evaluations.editVersionAction', 'Edit version {version}', {
            version: version.version,
          })}
          onClick={onEdit}
        >
          {msg('evaluations.edit', 'Edit')}
        </button>
      </TableCell>
    </TableRow>
  );
}

function CurrentVersion({ dataset, version }: { dataset: EvaluationDataset | undefined; version: number | undefined }) {
  const { msg } = useI18n();
  return (
    <div className="flex flex-wrap items-center gap-3 px-4 py-3">
      <p className="text-sm">
        <span className="text-muted-foreground">{msg('evaluations.currentVersion', 'Current version')}: </span>
        {versionLabel(version)}
      </p>
      {dataset ? <EvaluationStatusBadge status={dataset.status} /> : null}
    </div>
  );
}

function versionLabel(version: number | undefined) {
  if (!version) {
    return '';
  }
  return `v${version}`;
}

function AddCaseMenu({ onAction }: { onAction: (action: CaseAction) => void }) {
  const { msg } = useI18n();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button type="button" variant="outline" className="h-8">
            {msg('evaluations.addCase', 'Add case')}
            <ChevronDown className="size-4" aria-hidden />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onClick={() => onAction('add')}>{msg('evaluations.addCase', 'Add case')}</DropdownMenuItem>
        <DropdownMenuItem onClick={() => onAction('jsonl')}>
          {msg('evaluations.importJsonl', 'Import JSONL')}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onAction('session')}>
          {msg('evaluations.importFromSession', 'Import from session')}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onAction('sessions')}>
          {msg('evaluations.importSessions', 'Import sessions')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function dateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}

function matchesCaseQuery(query: string, parts: string[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.some((part) => part.toLowerCase().includes(needle));
}

function AddCaseForm({ datasetId }: { datasetId: string }) {
  const { msg } = useI18n();
  const create = useCreateEvaluationCase(datasetId);
  const [name, setName] = useState('');
  const [turns, setTurns] = useState('');
  const [desc, setDesc] = useState('');
  const [checkpoints, setCheckpoints] = useState('');
  const [checkpointError, setCheckpointError] = useState(false);

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = parseCheckpointsField(checkpoints);
    if (parsed.error) {
      setCheckpointError(true);
      return;
    }
    setCheckpointError(false);
    create.mutate(
      {
        name,
        desc,
        turns: turns
          .split('\n')
          .map((turn) => turn.trim())
          .filter((turn) => turn.length > 0),
        checkpoints: parsed.checkpoints,
      },
      {
        onSuccess: () => {
          setName('');
          setTurns('');
          setDesc('');
          setCheckpoints('');
        },
      },
    );
  }

  return (
    <form className="grid gap-3" onSubmit={submit}>
      <Label htmlFor="evaluation-case-name">{msg('evaluations.name', 'Name')}</Label>
      <Input id="evaluation-case-name" value={name} onChange={(event) => setName(event.target.value)} required />
      <Label htmlFor="evaluation-case-turns">{msg('evaluations.turns', 'Turns')}</Label>
      <Textarea id="evaluation-case-turns" value={turns} onChange={(event) => setTurns(event.target.value)} required />
      <Label htmlFor="evaluation-case-desc">{msg('evaluations.descriptionField', 'Description')}</Label>
      <Textarea id="evaluation-case-desc" value={desc} onChange={(event) => setDesc(event.target.value)} />
      <CheckpointsField
        id="evaluation-add-case-checkpoints"
        value={checkpoints}
        onChange={setCheckpoints}
        error={checkpointError}
      />
      {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
      <Button type="submit" disabled={create.isPending}>
        {msg('evaluations.addCase', 'Add case')}
      </Button>
    </form>
  );
}

function ImportSessionCaseForm({ datasetId }: { datasetId: string }) {
  const { msg } = useI18n();
  const create = useCreateEvaluationCaseFromSession(datasetId);
  const [sessionId, setSessionId] = useState('');
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [checkpoints, setCheckpoints] = useState('');
  const [includeOutputFiles, setIncludeOutputFiles] = useState(false);
  const [checkpointError, setCheckpointError] = useState(false);

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = parseCheckpointsField(checkpoints);
    if (parsed.error) {
      setCheckpointError(true);
      return;
    }
    setCheckpointError(false);
    create.mutate(
      {
        session_id: sessionId,
        name,
        desc,
        checkpoints: parsed.checkpoints,
        include_output_files: includeOutputFiles,
      },
      {
        onSuccess: () => {
          setSessionId('');
          setName('');
          setDesc('');
          setCheckpoints('');
        },
      },
    );
  }

  return (
    <form className="grid gap-3" onSubmit={submit}>
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.addSessionToDatasetHint',
          'User messages become turns. Agent replies are not saved as checkpoints.',
        )}
      </p>
      <Label htmlFor="evaluation-case-session">{msg('evaluations.sessionId', 'Session')}</Label>
      <Input
        id="evaluation-case-session"
        value={sessionId}
        onChange={(event) => setSessionId(event.target.value)}
        required
      />
      <SessionCaseFields
        idPrefix="import-session-case"
        name={name}
        desc={desc}
        checkpoints={checkpoints}
        checkpointError={checkpointError}
        onName={setName}
        onDesc={setDesc}
        onCheckpoints={setCheckpoints}
      />
      <IncludeOutputFilesField
        id="import-session-case-outputs"
        checked={includeOutputFiles}
        onChange={setIncludeOutputFiles}
      />
      {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
      <Button type="submit" disabled={create.isPending}>
        {msg('evaluations.importFromSession', 'Import from session')}
      </Button>
    </form>
  );
}

function ImportSessionsForm({ datasetId }: { datasetId: string }) {
  const { msg } = useI18n();
  const create = useCreateEvaluationCasesFromSessions(datasetId);
  const [sessionIDs, setSessionIDs] = useState('');
  const [namePrefix, setNamePrefix] = useState('');
  const [desc, setDesc] = useState('');
  const [checkpoints, setCheckpoints] = useState('');
  const [includeOutputFiles, setIncludeOutputFiles] = useState(false);
  const [checkpointError, setCheckpointError] = useState(false);
  const [result, setResult] = useState<EvaluationCaseFromSessionsResult | null>(null);

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = parseCheckpointsField(checkpoints);
    const ids = parseSessionIDs(sessionIDs);
    if (parsed.error || ids.length === 0) {
      setCheckpointError(Boolean(parsed.error));
      return;
    }
    setCheckpointError(false);
    create.mutate(
      {
        session_ids: ids,
        name_prefix: namePrefix,
        desc,
        checkpoints: parsed.checkpoints,
        include_output_files: includeOutputFiles,
      },
      { onSuccess: (created) => setResult(created) },
    );
  }

  return (
    <form className="grid gap-3" onSubmit={submit}>
      <p className="text-sm text-muted-foreground">
        {msg('evaluations.sessionIdsHint', 'One session id per line, or separated by commas. At most 50.')}
      </p>
      <Label htmlFor="evaluation-case-sessions">{msg('evaluations.sessionIds', 'Session ids')}</Label>
      <Textarea
        id="evaluation-case-sessions"
        value={sessionIDs}
        onChange={(event) => setSessionIDs(event.target.value)}
        required
      />
      <Label htmlFor="evaluation-case-prefix">{msg('evaluations.namePrefix', 'Name prefix')}</Label>
      <Input id="evaluation-case-prefix" value={namePrefix} onChange={(event) => setNamePrefix(event.target.value)} />
      <SessionCaseFields
        idPrefix="import-sessions"
        showName={false}
        desc={desc}
        checkpoints={checkpoints}
        checkpointError={checkpointError}
        onDesc={setDesc}
        onCheckpoints={setCheckpoints}
      />
      <IncludeOutputFilesField
        id="import-sessions-outputs"
        checked={includeOutputFiles}
        onChange={setIncludeOutputFiles}
      />
      {create.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(create.error)}</p> : null}
      {result ? (
        <div className="grid gap-1">
          <p className="text-sm text-muted-foreground">
            {msg('evaluations.sessionsImported', 'Added {count}.', { count: result.created.length })}
          </p>
          {result.failed.map((item, index) => (
            <p key={`${item.session_id}-${index}`} className="text-sm text-destructive">
              {item.session_id}: {item.error}
            </p>
          ))}
        </div>
      ) : null}
      <Button type="submit" disabled={create.isPending}>
        {msg('evaluations.importSessions', 'Import sessions')}
      </Button>
    </form>
  );
}

const maxImportBytes = 100 * 1024 * 1024;

function ImportCasesForm({ datasetId }: { datasetId: string }) {
  const { msg } = useI18n();
  const importCases = useImportEvaluationCases(datasetId);
  const [jsonl, setJsonl] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [tooLarge, setTooLarge] = useState(false);

  function submit(event: FormEvent) {
    event.preventDefault();
    if (file && file.size > maxImportBytes) {
      setTooLarge(true);
      return;
    }
    setTooLarge(false);
    const payload = file ? importFilePayload(file) : { body: jsonl, contentType: 'application/x-ndjson' };
    importCases.mutate(payload, {
      onSuccess: () => {
        setJsonl('');
        setFile(null);
      },
    });
  }

  return (
    <form className="grid gap-3" onSubmit={submit}>
      <Label htmlFor="evaluation-case-file">{msg('evaluations.importFile', 'JSONL or ZIP file')}</Label>
      <Input
        id="evaluation-case-file"
        type="file"
        accept=".jsonl,.zip,application/jsonl,application/zip"
        onChange={(event) => setFile(event.target.files?.[0] ?? null)}
      />
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.importFileHint',
          'A JSONL file, or a ZIP up to 100MB with one JSONL plus optional input_files/ and reference_files/. Companion file bytes are not copied into workspace Files.',
        )}
      </p>
      <Label htmlFor="evaluation-case-jsonl">
        {msg('evaluations.importHint', 'One JSON object per line, with name and turns.')}
      </Label>
      <Textarea id="evaluation-case-jsonl" value={jsonl} onChange={(event) => setJsonl(event.target.value)} />
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.importCheckpointsHint',
          'checkpoints is an optional string array: an acceptance checklist for evaluators, not instructions for the agent. On the form, write one checkpoint per line.',
        )}
      </p>
      {tooLarge ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.importFileTooLarge', 'File must be 100MB or smaller.')}
        </p>
      ) : null}
      {importCases.error ? (
        <p className="text-sm text-destructive">{evaluationErrorMessage(importCases.error)}</p>
      ) : null}
      <Button type="submit" disabled={importCases.isPending || (!file && jsonl.trim().length === 0)}>
        {msg('evaluations.import', 'Import')}
      </Button>
    </form>
  );
}

function importFilePayload(file: File) {
  const zip = file.name.toLowerCase().endsWith('.zip');
  return { body: file, contentType: zip ? 'application/zip' : 'application/jsonl' };
}

import { useState, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import { evaluationErrorMessage, type EvaluationCase, type EvaluationCaseFromSessionsResult } from './api';
import { CaseDrawer } from './CaseDrawer';
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

export function DatasetCasesPage() {
  const { msg } = useI18n();
  const datasetId = datasetIdFromPath();
  const { workspace, datasetQuery, casesQuery } = useEvaluationCaseList(datasetId);
  const cases = casesQuery.data?.data ?? [];
  const version = datasetQuery.data?.version;
  const [openCase, setOpenCase] = useState<EvaluationCase | null>(null);

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5">
        <a
          className="text-sm text-muted-foreground underline-offset-4 hover:underline"
          href={evaluationsIndexHref(workspace.workspaceId)}
        >
          {msg('evaluations.title', 'Datasets')}
        </a>
        <h1 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
          {datasetQuery.data?.name ?? msg('evaluations.cases', 'Cases')}
        </h1>
        {version ? (
          <p className="mt-1 text-sm text-muted-foreground">
            {msg('evaluations.version', 'Version {version}', { version: version.version })}
          </p>
        ) : null}
        {datasetQuery.data?.desc ? (
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">{datasetQuery.data.desc}</p>
        ) : null}
      </header>
      <div className="mb-8 grid gap-6 lg:grid-cols-2">
        <AddCaseForm datasetId={datasetId} />
        <ImportCasesForm datasetId={datasetId} />
        <ImportSessionCaseForm datasetId={datasetId} />
        <ImportSessionsForm datasetId={datasetId} />
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('evaluations.position', 'Position')}</TableHead>
            <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
            <TableHead>{msg('evaluations.turns', 'Turns')}</TableHead>
            <TableHead>{msg('evaluations.descriptionField', 'Description')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <CaseRows
            cases={cases}
            isLoading={!workspace.workspaceReady || casesQuery.isLoading}
            errorMessage={casesQuery.error ? evaluationErrorMessage(casesQuery.error) : ''}
            onRetry={() => void casesQuery.refetch()}
            onOpen={setOpenCase}
          />
        </TableBody>
      </Table>
      <CaseDrawer
        item={openCase}
        workspaceId={workspace.workspaceId}
        datasetId={datasetId}
        open={openCase !== null}
        onOpenChange={(open) => {
          if (!open) {
            setOpenCase(null);
          }
        }}
        onSaved={setOpenCase}
        onDeleted={() => setOpenCase(null)}
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
      <TableCell>{item.position}</TableCell>
      <TableCell>
        <button type="button" className="font-medium underline-offset-4 hover:underline" onClick={() => onOpen(item)}>
          {item.name}
        </button>
      </TableCell>
      <TableCell className="whitespace-pre-wrap">{item.turns.join('\n')}</TableCell>
      <TableCell>{item.desc}</TableCell>
    </TableRow>
  ));
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
      <h2 className="text-sm font-medium">{msg('evaluations.addCase', 'Add case')}</h2>
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
    <form className="grid gap-3 lg:col-span-2" onSubmit={submit}>
      <h2 className="text-sm font-medium">{msg('evaluations.importFromSession', 'Import from session')}</h2>
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
    <form className="grid gap-3 lg:col-span-2" onSubmit={submit}>
      <h2 className="text-sm font-medium">{msg('evaluations.importSessions', 'Import sessions')}</h2>
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
      <h2 className="text-sm font-medium">{msg('evaluations.importJsonl', 'Import JSONL')}</h2>
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

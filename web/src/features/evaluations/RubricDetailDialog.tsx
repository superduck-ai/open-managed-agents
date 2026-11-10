import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { CopyButton } from '../../shared/ui/copy-button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Textarea } from '../../shared/ui/textarea';
import {
  evaluationError,
  listEvaluationRubricVersions,
  saveEvaluationRubricVersion,
  type EvaluationRubric,
  type EvaluationRubricVersion,
} from './api';
import { formatEvaluationTime } from './evaluationChrome';

export function RubricDetailDialog({
  rubric,
  onOpenChange,
  orgUuid,
  workspaceId,
}: {
  rubric: EvaluationRubric | null;
  onOpenChange: (open: boolean) => void;
  orgUuid: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const [body, setBody] = useState('');
  const [editing, setEditing] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [selectedVersionID, setSelectedVersionID] = useState('');
  const [error, setError] = useState('');
  const versions = useQuery({
    queryKey: ['evaluation-rubric-versions', orgUuid, workspaceId, rubric?.id],
    queryFn: () => listEvaluationRubricVersions(orgUuid, workspaceId, rubric?.id ?? ''),
    enabled: Boolean(rubric?.id),
  });
  const versionItems = versions.data?.data ?? [];
  const selected = versionItems.find((item) => item.id === selectedVersionID) ?? versionItems[0];
  const shown = shownRubric(rubric, selected);
  useEffect(() => {
    setBody(rubric?.body ?? '');
    setEditing(false);
    setHistoryOpen(false);
    setSelectedVersionID('');
    setError('');
  }, [rubric]);
  useEffect(() => {
    if (!selected || editing) return;
    setBody(selected.body);
  }, [selected, editing]);
  const save = useMutation({
    mutationFn: () =>
      saveEvaluationRubricVersion(orgUuid, workspaceId, rubric?.id ?? '', {
        body,
        pass_score: shown.passScore,
      }),
    onSuccess: async () => {
      setEditing(false);
      setSelectedVersionID('');
      await queryClient.invalidateQueries({ queryKey: ['evaluation-rubrics', orgUuid, workspaceId] });
      await queryClient.invalidateQueries({
        queryKey: ['evaluation-rubric-versions', orgUuid, workspaceId, rubric?.id],
      });
    },
    onError: (cause) => setError(evaluationError(cause)),
  });

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!rubric || !editing) return;
    save.mutate();
  }

  function cancelEdit() {
    setBody(selected?.body ?? rubric?.body ?? '');
    setEditing(false);
    setError('');
  }

  function chooseVersion(item: EvaluationRubricVersion) {
    setSelectedVersionID(item.id);
    setBody(item.body);
    setEditing(false);
    setError('');
  }

  return (
    <Dialog open={rubric !== null} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-[min(860px,calc(100vh-4rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl"
        closeButtonDisabled={save.isPending}
      >
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={onSubmit}>
          <DialogHeader className="flex-row items-center justify-between gap-3 border-b border-border px-6 py-4 pr-14">
            <DialogTitle className="text-base leading-6">
              {msg('evaluations.rubricDetail', 'Rubric details')}
            </DialogTitle>
            <div className="flex items-center gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setHistoryOpen((open) => !open)}>
                {msg('evaluations.versionHistory', 'Version history')}
              </Button>
              <Button
                type="button"
                variant={editing ? 'secondary' : 'outline'}
                size="sm"
                onClick={() => setEditing(true)}
              >
                {msg('evaluations.edit', 'Edit')}
              </Button>
            </div>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-8 py-6">
            <h2 className="text-2xl leading-8 font-medium">{rubric?.name}</h2>
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
              <span>{msg('evaluations.rubrics', 'Rubrics')}</span>
              <span className="inline-flex items-center gap-1 font-mono text-xs">
                {rubric?.id}
                <CopyButton value={rubric?.id ?? ''} label={msg('evaluations.copyId', 'Copy ID')} />
              </span>
              <span className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground">
                v{shown.version}
              </span>
              <span className="tabular-nums">{shown.createdAt ? formatEvaluationTime(shown.createdAt) : ''}</span>
              <span>
                {msg('evaluations.passScore', 'Pass score')} {shown.passScore}
              </span>
            </div>
            {historyOpen ? (
              <RubricVersionList
                items={versionItems}
                selectedID={selected?.id ?? ''}
                latestLabel={msg('evaluations.latestVersion', 'Latest')}
                loading={versions.isLoading}
                loadingLabel={msg('evaluations.loading', 'Loading')}
                error={versions.isError ? evaluationError(versions.error) : ''}
                onSelect={chooseVersion}
              />
            ) : null}
            <h3 className="mt-8 mb-3 text-sm font-medium">{msg('evaluations.scoreRules', 'Scoring rules')}</h3>
            {editing ? (
              <Textarea
                value={body}
                onChange={(event) => setBody(event.target.value)}
                rows={16}
                className="min-h-80 font-mono text-sm"
                aria-label={msg('evaluations.rubricContent', 'Rubric')}
                required
              />
            ) : (
              <RubricRules body={body} />
            )}
            {error ? <p className="mt-3 text-sm text-destructive">{error}</p> : null}
          </div>
          {editing ? (
            <DialogFooter className="border-t border-border px-6 py-4">
              <Button type="button" variant="outline" disabled={save.isPending} onClick={cancelEdit}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={save.isPending || !body.trim()}>
                {msg('common.save', 'Save')}
              </Button>
            </DialogFooter>
          ) : null}
        </form>
      </DialogContent>
    </Dialog>
  );
}

function shownRubric(rubric: EvaluationRubric | null, selected?: EvaluationRubricVersion) {
  return {
    version: selected?.version ?? rubric?.version ?? 1,
    createdAt: selected?.created_at ?? rubric?.created_at ?? '',
    passScore: selected?.pass_score ?? rubric?.pass_score ?? 60,
  };
}

function RubricVersionList({
  items,
  selectedID,
  latestLabel,
  loading,
  loadingLabel,
  error,
  onSelect,
}: {
  items: EvaluationRubricVersion[];
  selectedID: string;
  latestLabel: string;
  loading: boolean;
  loadingLabel: string;
  error: string;
  onSelect: (item: EvaluationRubricVersion) => void;
}) {
  const latestVersion = items[0]?.version;
  return (
    <div className="mt-4 rounded-lg border border-border">
      {loading ? <p className="px-4 py-3 text-sm text-muted-foreground">{loadingLabel}</p> : null}
      {error ? <p className="px-4 py-3 text-sm text-destructive">{error}</p> : null}
      <ul>
        {items.map((item) => (
          <li key={item.id} className="border-t border-border first:border-t-0">
            <button
              type="button"
              className={`flex w-full items-center gap-3 px-4 py-3 text-left text-sm ${
                item.id === selectedID ? 'bg-muted' : 'hover:bg-muted/50'
              }`}
              aria-current={item.id === selectedID ? 'true' : undefined}
              onClick={() => onSelect(item)}
            >
              <span className="font-medium">v{item.version}</span>
              <span className="text-muted-foreground tabular-nums">{formatEvaluationTime(item.created_at)}</span>
              {item.version === latestVersion ? (
                <span className="rounded-full bg-[var(--success-bg)] px-2 py-0.5 text-xs text-[var(--green-11)]">
                  {latestLabel}
                </span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function RubricRules({ body }: { body: string }) {
  return (
    <div className="text-sm leading-6 text-foreground">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={rubricMarkdown}>
        {body}
      </ReactMarkdown>
    </div>
  );
}

function rubricHeading(children: ReactNode) {
  return <h4 className="mt-4 mb-2 text-base font-medium">{children}</h4>;
}

const rubricMarkdown: Components = {
  h1: ({ children }) => rubricHeading(children),
  h2: ({ children }) => rubricHeading(children),
  h3: ({ children }) => <h4 className="mt-4 mb-2 font-medium">{children}</h4>,
  p: ({ children }) => <p className="mb-2">{children}</p>,
  ul: ({ children }) => <ul className="mb-3 list-disc pl-5">{children}</ul>,
  ol: ({ children }) => <ol className="mb-3 list-decimal pl-5">{children}</ol>,
  li: ({ children }) => <li className="mb-1">{children}</li>,
};

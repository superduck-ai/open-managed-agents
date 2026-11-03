import { useI18n } from '../../shared/i18n';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '../../shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationDatasetPreset } from './api';
import { CopyIdButton } from './CopyIdButton';
import { EvaluationStatusBadge } from './EvaluationStatusBadge';
import { evaluationDatasetPresetHref, useEvaluationDatasetPresetList } from './useEvaluationDatasets';

export function DatasetPresets({ search = '' }: { search?: string }) {
  const { msg } = useI18n();
  const { workspace, query } = useEvaluationDatasetPresetList();
  const presets = (query.data?.data ?? []).filter((item) => matchesQuery(search, [item.id, item.name, item.desc]));

  return (
    <>
      <Table className="table-fixed">
        <TableHeader>
          <TableRow>
            <TableHead className="w-[14%] text-xs font-normal text-muted-foreground">
              {msg('evaluations.id', 'ID')}
            </TableHead>
            <TableHead className="w-[18%] text-xs font-normal text-muted-foreground">
              {msg('evaluations.name', 'Name')}
            </TableHead>
            <TableHead className="text-xs font-normal text-muted-foreground">
              {msg('evaluations.descriptionField', 'Description')}
            </TableHead>
            <TableHead className="w-20 text-xs font-normal text-muted-foreground">
              {msg('evaluations.presetVersion', 'Version')}
            </TableHead>
            <TableHead className="w-24 text-xs font-normal text-muted-foreground">
              {msg('evaluations.status', 'Status')}
            </TableHead>
            <TableHead className="w-16 text-xs font-normal text-muted-foreground">
              {msg('evaluations.caseCount', 'Cases')}
            </TableHead>
            <TableHead className="w-28 text-xs font-normal text-muted-foreground">
              {msg('evaluations.createdAt', 'Created')}
            </TableHead>
            <TableHead className="w-24 text-right text-xs font-normal text-muted-foreground">
              {msg('evaluations.actions', 'Action')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <DatasetPresetRows
            presets={presets}
            isLoading={query.isLoading}
            errorMessage={query.error ? evaluationErrorMessage(query.error) : ''}
            onRetry={() => void query.refetch()}
            workspaceId={workspace.workspaceId}
          />
        </TableBody>
      </Table>
    </>
  );
}

function DatasetPresetRows({
  presets,
  isLoading,
  errorMessage,
  onRetry,
  workspaceId,
}: {
  presets: EvaluationDatasetPreset[];
  isLoading: boolean;
  errorMessage: string;
  onRetry: () => void;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={8} label={msg('common.loading', 'Loading...')} />;
  }
  if (errorMessage) {
    return (
      <TableErrorRow
        colSpan={8}
        title={msg('evaluations.presetsLoadFailed', 'Could not load presets')}
        message={errorMessage}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (presets.length === 0) {
    return <TableEmptyRow colSpan={8}>{msg('evaluations.presetsEmpty', 'No presets.')}</TableEmptyRow>;
  }
  return presets.map((preset) => {
    const latest = preset.versions.reduce<(typeof preset.versions)[number] | undefined>(
      (current, version) => (!current || version.version > current.version ? version : current),
      undefined,
    );
    return (
      <TableRow key={preset.id}>
        <TableCell>
          <CopyIdButton id={preset.id} />
        </TableCell>
        <TableCell className="font-medium">
          <span className="block truncate">{preset.name}</span>
        </TableCell>
        <TableCell className="text-muted-foreground">
          <span className="block truncate">{preset.desc}</span>
        </TableCell>
        <TableCell>{latest ? `v${latest.version}` : ''}</TableCell>
        <TableCell>
          <EvaluationStatusBadge status={preset.status} />
        </TableCell>
        <TableCell>{latest?.case_count ?? ''}</TableCell>
        <TableCell className="text-muted-foreground">{dateLabel(preset.published_at)}</TableCell>
        <TableCell className="text-right">
          <a
            className="text-sm text-muted-foreground hover:text-foreground"
            href={evaluationDatasetPresetHref(workspaceId, preset.id)}
            aria-label={`${msg('evaluations.viewDetail', 'View')} ${preset.name}`}
          >
            {msg('evaluations.viewDetail', 'View')}
          </a>
        </TableCell>
      </TableRow>
    );
  });
}

function matchesQuery(query: string, parts: string[]) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.some((part) => part.toLowerCase().includes(needle));
}

function dateLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}

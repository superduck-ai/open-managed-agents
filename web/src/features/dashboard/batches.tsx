import { AlertCircle, Ban, Download, Receipt, RefreshCw, X } from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { cn } from '@/shared/lib/utils';
import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName
} from '@/shared/ui/data-table-interactions';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/shared/ui/table';
import { useI18n } from '../../shared/i18n';
import { ConsolePageFrame, CursorPagination, TableEmptyRow, TableErrorRow, TableLoadingRow } from './frame';
import {
  batchDetailHref,
  batchRequestProgressClass,
  batchStatusClass,
  canCancelBatch,
  cancelMessageBatch,
  clearBatchDetailHref,
  currentBatchId,
  downloadMessageBatchResults,
  errorMessage,
  formatBatchDateTime,
  formatBatchRequestProgress,
  formatBatchStatus,
  formatMessageBatchId,
  formatRelativeTime,
  listMessageBatches,
  retrieveMessageBatch,
  useDashboardWorkspaceScope,
  type ConsoleMessageBatch,
  type MessageBatchesPageCursor
} from './model';

export function BatchesPage() {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const [pageIndex, setPageIndex] = useState(0);
  const [pageCursors, setPageCursors] = useState<MessageBatchesPageCursor[]>([{}]);
  const [selectedBatchId, setSelectedBatchId] = useState(currentBatchId());
  const [batchAction, setBatchAction] = useState<string | null>(null);
  const previousWorkspaceIdRef = useRef(workspaceId);
  const workspaceMatchesSelection = previousWorkspaceIdRef.current === workspaceId;
  const cursor = workspaceMatchesSelection ? pageCursors[pageIndex] ?? {} : {};
  const workspaceSelectedBatchId = workspaceMatchesSelection ? selectedBatchId : '';
  const batchesQuery = useQuery({
    queryKey: ['messageBatches', workspaceId, cursor.afterId ?? '', cursor.beforeId ?? ''],
    queryFn: () => listMessageBatches(cursor, workspaceId),
    retry: false
  });
  const selectedBatchQuery = useQuery({
    queryKey: ['messageBatch', workspaceId, workspaceSelectedBatchId],
    queryFn: () => retrieveMessageBatch(workspaceSelectedBatchId, workspaceId),
    enabled: Boolean(workspaceSelectedBatchId),
    retry: false
  });
  const response = batchesQuery.data;
  const batches = response?.data ?? [];
  const lastId = response?.last_id ?? batches.at(-1)?.id;
  const selectedBatch = selectedBatchQuery.data ?? batches.find((batch) => batch.id === workspaceSelectedBatchId) ?? null;

  useEffect(() => {
    const handlePopState = () => {
      setSelectedBatchId(currentBatchId());
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  useEffect(() => {
    if (previousWorkspaceIdRef.current === workspaceId) {
      return;
    }
    previousWorkspaceIdRef.current = workspaceId;
    setPageIndex(0);
    setPageCursors([{}]);
    setBatchAction(null);
    if (currentBatchId()) {
      window.history.replaceState(null, '', clearBatchDetailHref());
    }
    setSelectedBatchId('');
  }, [workspaceId]);

  const goNext = () => {
    if (!lastId) {
      return;
    }
    setPageCursors((current) => {
      const next = current.slice(0, pageIndex + 1);
      next.push({ afterId: lastId });
      return next;
    });
    setPageIndex((current) => current + 1);
  };

  const goPrevious = () => {
    setPageIndex((current) => Math.max(0, current - 1));
  };

  const selectBatch = (batchId: string) => {
    window.history.pushState(null, '', batchDetailHref(batchId));
    setSelectedBatchId(batchId);
  };

  const clearSelectedBatch = () => {
    window.history.pushState(null, '', clearBatchDetailHref());
    setSelectedBatchId('');
  };

  const refreshBatches = async (batchId: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['messageBatches', workspaceId] }),
      queryClient.invalidateQueries({ queryKey: ['messageBatch', workspaceId, batchId] })
    ]);
  };

  const handleDownloadResults = async (batch: ConsoleMessageBatch) => {
    if (!batch.results_url || batchAction) {
      return;
    }
    setBatchAction(`download:${batch.id}`);
    try {
      await downloadMessageBatchResults(batch, workspaceId);
    } finally {
      setBatchAction(null);
    }
  };

  const handleCancelBatch = async (batch: ConsoleMessageBatch) => {
    if (!canCancelBatch(batch) || batchAction) {
      return;
    }
    setBatchAction(`cancel:${batch.id}`);
    try {
      await cancelMessageBatch(batch.id, workspaceId);
      await refreshBatches(batch.id);
    } finally {
      setBatchAction(null);
    }
  };

  return (
    <ConsolePageFrame title={msg('batches.title', 'Batches')} icon={Receipt}>
      <div className={workspaceSelectedBatchId ? 'grid gap-6 xl:grid-cols-[minmax(0,1fr)_320px]' : ''}>
        <BatchesTable
          batches={batches}
          workspaceName={workspaceName}
          selectedBatchId={workspaceSelectedBatchId}
          isLoading={batchesQuery.isLoading}
          isFetching={batchesQuery.isFetching}
          error={batchesQuery.error}
          canPrevious={pageIndex > 0 && !batchesQuery.isFetching}
          canNext={Boolean(response?.has_more && lastId) && !batchesQuery.isFetching}
          onRetry={() => void batchesQuery.refetch()}
          onPrevious={goPrevious}
          onNext={goNext}
          onSelectBatch={selectBatch}
        />

        {workspaceSelectedBatchId ? (
          <BatchDetailPanel
            batch={selectedBatch}
            batchId={workspaceSelectedBatchId}
            isLoading={selectedBatchQuery.isLoading}
            error={selectedBatchQuery.error}
            batchAction={batchAction}
            onClose={clearSelectedBatch}
            onRetry={() => void selectedBatchQuery.refetch()}
            onDownloadResults={(batch) => void handleDownloadResults(batch)}
            onCancelBatch={(batch) => void handleCancelBatch(batch)}
          />
        ) : null}
      </div>
    </ConsolePageFrame>
  );
}

function BatchesTable({
  batches,
  workspaceName,
  selectedBatchId,
  isLoading,
  isFetching,
  error,
  canPrevious,
  canNext,
  onRetry,
  onPrevious,
  onNext,
  onSelectBatch
}: {
  batches: ConsoleMessageBatch[];
  workspaceName: string;
  selectedBatchId: string;
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  canPrevious: boolean;
  canNext: boolean;
  onRetry: () => void;
  onPrevious: () => void;
  onNext: () => void;
  onSelectBatch: (batchId: string) => void;
}) {
  const { msg } = useI18n();

  return (
    <section aria-label={msg('batches.listAria', 'Batches list')} className="overflow-x-auto">
      <Table className={cn('min-w-[760px]', dataTableClassName)}>
        <colgroup>
          <col className="w-[28%]" />
          <col className="w-[16%]" />
          <col className="w-[16%]" />
          <col className="w-[40%]" />
        </colgroup>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            <TableHead className={dataTableHeaderCellClassName}>{msg('common.id', 'ID')}</TableHead>
            <TableHead className={dataTableHeaderCellClassName}>{msg('common.status', 'Status')}</TableHead>
            <TableHead className={dataTableHeaderCellClassName}>{msg('analytics.table.requests', 'Requests')}</TableHead>
            <TableHead className={dataTableHeaderCellClassName}>{msg('common.created', 'Created')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <TableLoadingRow colSpan={4} label={msg('batches.loading', 'Loading batches...')} />
          ) : error ? (
            <TableErrorRow
              colSpan={4}
              title={msg('batches.error', 'Batches could not be loaded.')}
              message={errorMessage(error)}
              retryLabel={msg('common.retry', 'Retry')}
              onRetry={onRetry}
            />
          ) : batches.length === 0 ? (
            <TableEmptyRow colSpan={4}>
              {msg('batches.empty', 'No batches have been created in the {workspaceName} workspace.', {
                workspaceName
              })}
            </TableEmptyRow>
          ) : (
            batches.map((batch) => {
              const selected = batch.id === selectedBatchId;
              return (
                <DataTableRow
                  key={batch.id}
                  clickable
                  selected={selected}
                  aria-controls="batch-detail-panel"
                  aria-expanded={selected}
                  onClick={() => onSelectBatch(batch.id)}
                >
                  <DataTableCell edge="start">
                    <CopyIdCell
                      value={batch.id}
                      ariaLabel={msg('batches.copyAria', 'Copy {batchId}', { batchId: batch.id })}
                      stopPropagation
                    >
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        aria-label={batch.id}
                        aria-controls="batch-detail-panel"
                        aria-pressed={selected}
                        aria-expanded={selected}
                        className="h-auto min-w-0 justify-start truncate p-0 font-mono text-xs font-medium text-foreground hover:bg-transparent hover:text-foreground focus-visible:bg-transparent"
                        onClick={(event) => {
                          event.stopPropagation();
                          onSelectBatch(batch.id);
                        }}
                      >
                        {formatMessageBatchId(batch.id)}
                      </Button>
                    </CopyIdCell>
                  </DataTableCell>
                  <DataTableCell>
                    <Badge variant="secondary" className={`rounded-md ${batchStatusClass(batch.processing_status)}`}>
                      {formatBatchStatus(batch.processing_status, msg)}
                    </Badge>
                  </DataTableCell>
                  <DataTableCell className="text-muted-foreground">
                    <span className="inline-flex items-center gap-2">
                      <span className={`size-3 rounded-full ${batchRequestProgressClass(batch)}`} aria-hidden />
                      {formatBatchRequestProgress(batch)}
                    </span>
                  </DataTableCell>
                  <DataTableCell edge="end" className="text-muted-foreground">{formatRelativeTime(batch.created_at)}</DataTableCell>
                </DataTableRow>
              );
            })
          )}
        </TableBody>
      </Table>

      <CursorPagination
        previousLabel={msg('pagination.previousPage', 'Previous page')}
        nextLabel={msg('pagination.nextPage', 'Next page')}
        updatingLabel={msg('common.updating', 'Updating...')}
        canPrevious={canPrevious}
        canNext={canNext}
        isUpdating={isFetching && !isLoading}
        onPrevious={onPrevious}
        onNext={onNext}
      />
    </section>
  );
}

function BatchDetailPanel({
  batch,
  batchId,
  isLoading,
  error,
  batchAction,
  onClose,
  onRetry,
  onDownloadResults,
  onCancelBatch
}: {
  batch: ConsoleMessageBatch | null;
  batchId: string;
  isLoading: boolean;
  error: unknown;
  batchAction: string | null;
  onClose: () => void;
  onRetry: () => void;
  onDownloadResults: (batch: ConsoleMessageBatch) => void;
  onCancelBatch: (batch: ConsoleMessageBatch) => void;
}) {
  const { msg } = useI18n();
  const panelClassName = 'border-t border-border pt-5 xl:min-h-[calc(100vh-5rem)] xl:border-l xl:border-t-0 xl:pl-6 xl:pt-0';

  if (isLoading && !batch) {
    return (
      <section id="batch-detail-panel" aria-label={msg('batches.details.aria', 'Batch details')} className={`${panelClassName} text-sm text-muted-foreground`}>
        <span className="inline-flex items-center gap-2">
          <RefreshCw className="size-3.5 animate-spin" aria-hidden />
          {msg('batches.details.loading', 'Loading batch...')}
        </span>
      </section>
    );
  }

  if (error || !batch) {
    return (
      <section id="batch-detail-panel" aria-label={msg('batches.details.aria', 'Batch details')} className={panelClassName}>
        <Alert variant="destructive" className="max-w-xl">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertTitle>{msg('batches.details.error', 'Batch could not be loaded.')}</AlertTitle>
          <AlertDescription>
            <p>{errorMessage(error)}</p>
            <div className="mt-3 flex gap-2">
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={onRetry}
              >
                <RefreshCw className="size-3.5" aria-hidden />
                {msg('common.retry', 'Retry')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={onClose}
              >
                <X className="size-3.5" aria-hidden />
                {msg('common.close', 'Close')}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      </section>
    );
  }

  return (
    <section id="batch-detail-panel" aria-label={msg('batches.details.aria', 'Batch details')} className={panelClassName}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="text-lg font-semibold leading-tight text-foreground">{msg('batches.details.title', 'Batch details')}</h2>
            <Badge variant="secondary" className={`rounded-md ${batchStatusClass(batch.processing_status)}`}>
              {formatBatchStatus(batch.processing_status, msg)}
            </Badge>
          </div>
          <CopyIdCell
            value={batch.id}
            displayValue={formatMessageBatchId(batchId)}
            ariaLabel={msg('batches.copyAria', 'Copy {batchId}', { batchId: formatMessageBatchId(batch.id) })}
            className="mt-2"
            textClassName="text-muted-foreground"
            alwaysVisible
          />
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={msg('batches.details.closeInspector', 'Close inspector')}
          className="shrink-0 text-muted-foreground"
          onClick={onClose}
        >
          <X className="size-4" aria-hidden />
        </Button>
      </div>

      <Table className="mt-8 table-fixed border-t border-border text-left text-sm">
        <colgroup>
          <col className="w-[48%]" />
          <col className="w-[52%]" />
        </colgroup>
        <TableBody>
          <TableRow className="border-border hover:bg-transparent">
            <TableCell className="px-0 py-3 text-xs font-medium text-muted-foreground/70">
              {msg('batches.details.totalRequests', 'Total requests')}
            </TableCell>
            <TableCell className="px-0 py-3 text-right text-foreground">
              <span className="inline-flex items-center gap-2">
                <span className={`size-3 rounded-full ${batchRequestProgressClass(batch)}`} aria-hidden />
                {formatBatchRequestProgress(batch)}
              </span>
            </TableCell>
          </TableRow>
          <TableRow className="border-border hover:bg-transparent">
            <TableCell className="px-0 py-3 text-xs font-medium text-muted-foreground/70">
              {msg('batches.details.createdAt', 'Created at')}
            </TableCell>
            <TableCell className="px-0 py-3 text-right text-foreground">{formatBatchDateTime(batch.created_at)}</TableCell>
          </TableRow>
          <TableRow className="border-border hover:bg-transparent">
            <TableCell className="px-0 py-3 text-xs font-medium text-muted-foreground/70">
              {msg('batches.details.endedAt', 'Ended at')}
            </TableCell>
            <TableCell className="px-0 py-3 text-right text-foreground">{formatBatchDateTime(batch.ended_at)}</TableCell>
          </TableRow>
        </TableBody>
      </Table>

      <div className="mt-10 space-y-2">
        <Button
          type="button"
          variant="outline"
          disabled={!batch.results_url || batchAction === `download:${batch.id}`}
          onClick={() => onDownloadResults(batch)}
        >
          <Download className="size-3.5" aria-hidden />
          {msg('batches.actions.downloadResults', 'Download Results')}
        </Button>
        {canCancelBatch(batch) ? (
          <Button
            type="button"
            variant="outline"
            disabled={batchAction === `cancel:${batch.id}`}
            onClick={() => onCancelBatch(batch)}
          >
            <Ban className="size-3.5" aria-hidden />
            {msg('batches.actions.cancelBatch', 'Cancel batch')}
          </Button>
        ) : null}
      </div>
    </section>
  );
}

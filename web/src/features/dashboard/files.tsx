import { Copy, Download, FileText } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Button } from '@/shared/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/shared/ui/table';
import { useI18n } from '../../shared/i18n';
import { ConsolePageFrame, CursorPagination, DataTableCard, TableEmptyRow, TableErrorRow, TableLoadingRow } from './frame';
import {
  copyText,
  downloadFile,
  errorMessage,
  formatBytes,
  formatFileId,
  formatRelativeTime,
  listFiles,
  useDashboardWorkspaceScope,
  type ConsoleFile,
  type FilesPageCursor
} from './model';

export function FilesPage() {
  const { msg } = useI18n();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const [pageIndex, setPageIndex] = useState(0);
  const [pageCursors, setPageCursors] = useState<FilesPageCursor[]>([{}]);
  const [downloadingFileId, setDownloadingFileId] = useState<string | null>(null);
  const paginationWorkspaceIdRef = useRef(workspaceId);
  const cursor = paginationWorkspaceIdRef.current === workspaceId ? pageCursors[pageIndex] ?? {} : {};
  const filesQuery = useQuery({
    queryKey: ['files', workspaceId, cursor.afterId ?? '', cursor.beforeId ?? ''],
    queryFn: () => listFiles(cursor, workspaceId),
    retry: false
  });
  const response = filesQuery.data;
  const files = response?.data ?? [];
  const lastId = response?.last_id ?? files.at(-1)?.id;

  useEffect(() => {
    if (paginationWorkspaceIdRef.current === workspaceId) {
      return;
    }
    paginationWorkspaceIdRef.current = workspaceId;
    setPageIndex(0);
    setPageCursors([{}]);
    setDownloadingFileId(null);
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

  const handleDownload = async (file: ConsoleFile) => {
    if (!file.downloadable || downloadingFileId) {
      return;
    }
    setDownloadingFileId(file.id);
    try {
      await downloadFile(file, workspaceId);
    } finally {
      setDownloadingFileId(null);
    }
  };

  return (
    <ConsolePageFrame
      title={msg('files.title', 'Files')}
      icon={FileText}
      description={msg(
        'files.description',
        "Only files from the {workspaceName} workspace are shown. To see another workspace's files, select a workspace.",
        { workspaceName }
      )}
    >
      <FilesTable
        files={files}
        workspaceName={workspaceName}
        isLoading={filesQuery.isLoading}
        isFetching={filesQuery.isFetching}
        error={filesQuery.error}
        canPrevious={pageIndex > 0 && !filesQuery.isFetching}
        canNext={Boolean(response?.has_more && lastId) && !filesQuery.isFetching}
        downloadingFileId={downloadingFileId}
        onRetry={() => void filesQuery.refetch()}
        onPrevious={goPrevious}
        onNext={goNext}
        onDownload={(file) => void handleDownload(file)}
      />
    </ConsolePageFrame>
  );
}

function FilesTable({
  files,
  workspaceName,
  isLoading,
  isFetching,
  error,
  canPrevious,
  canNext,
  downloadingFileId,
  onRetry,
  onPrevious,
  onNext,
  onDownload
}: {
  files: ConsoleFile[];
  workspaceName: string;
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  canPrevious: boolean;
  canNext: boolean;
  downloadingFileId: string | null;
  onRetry: () => void;
  onPrevious: () => void;
  onNext: () => void;
  onDownload: (file: ConsoleFile) => void;
}) {
  const { msg } = useI18n();

  return (
    <section aria-label={msg('files.listAria', 'Files list')}>
      <DataTableCard>
        <Table className="min-w-[920px] table-fixed text-left">
          <colgroup>
            <col className="w-[18%]" />
            <col className="w-[49%]" />
            <col className="w-[11%]" />
            <col className="w-[15%]" />
            <col className="w-[7%]" />
          </colgroup>
          <TableHeader className="text-xs text-muted-foreground/70">
            <TableRow className="border-b border-border hover:bg-transparent">
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.id', 'ID')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.name', 'Name')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.size', 'Size')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.created', 'Created')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70" aria-label={msg('common.actions', 'Actions')} />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoadingRow colSpan={5} label={msg('files.loading', 'Loading files...')} />
            ) : error ? (
              <TableErrorRow
                colSpan={5}
                title={msg('files.error', 'Files could not be loaded.')}
                message={errorMessage(error)}
                retryLabel={msg('common.retry', 'Retry')}
                onRetry={onRetry}
              />
            ) : files.length === 0 ? (
              <TableEmptyRow colSpan={5}>
                {msg('files.empty', 'No files have been uploaded to the {workspaceName} workspace.', {
                  workspaceName
                })}
              </TableEmptyRow>
            ) : (
              files.map((file) => (
                <TableRow key={file.id} className="group border-b border-border text-foreground">
                  <TableCell className="px-3 py-3 align-middle">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="truncate font-mono text-[13px]">{formatFileId(file.id)}</span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        aria-label={msg('files.copyAria', 'Copy {fileId}', { fileId: file.id })}
                        className="shrink-0 text-muted-foreground/70 opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
                        onClick={() => void copyText(file.id)}
                      >
                        <Copy className="size-3.5" aria-hidden />
                      </Button>
                    </div>
                  </TableCell>
                  <TableCell className="truncate px-3 py-3 align-middle">{file.filename}</TableCell>
                  <TableCell className="px-3 py-3 align-middle text-muted-foreground">{formatBytes(file.size_bytes)}</TableCell>
                  <TableCell className="px-3 py-3 align-middle text-muted-foreground">{formatRelativeTime(file.created_at)}</TableCell>
                  <TableCell className="px-3 py-3 text-right align-middle">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={msg('files.downloadAria', 'Download {filename}', { filename: file.filename })}
                      className="text-muted-foreground/70"
                      disabled={!file.downloadable || downloadingFileId === file.id}
                      onClick={() => onDownload(file)}
                    >
                      <Download className="size-3.5" aria-hidden />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </DataTableCard>

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

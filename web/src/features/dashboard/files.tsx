import { Download, FileText, Upload } from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { type ChangeEvent, useEffect, useRef, useState } from 'react';
import { cn } from '@/shared/lib/utils';
import { Button } from '@/shared/ui/button';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  RowIconButton,
  dataTableActionsColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableIdColumnClassName,
  dataTableNumericColumnClassName,
  dataTablePrimaryColumnClassName,
  dataTableTimeColumnClassName,
} from '@/shared/ui/data-table-interactions';
import { TableSkeletonRows } from '@/shared/ui/resource-table';
import { RelativeTimeText } from '@/shared/ui/resource-time-cell';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { toast } from '@/shared/ui/sonner';
import { useI18n } from '../../shared/i18n';
import { ResourceListState } from '@/shared/ui/resource-list-state';
import { consoleResourceListLimit } from '@/shared/console-list';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { resourceListPageCount } from '@/shared/ui/resource-list-pagination';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { CursorPagination, TableErrorRow } from './frame';
import {
  downloadFile,
  errorMessage,
  formatBytes,
  listFiles,
  uploadFile,
  useDashboardWorkspaceScope,
  type ConsoleFile,
  type FilesPageCursor,
} from './model';

export function FilesPage() {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { workspaceId, workspaceName: workspaceNameToken } = useDashboardWorkspaceScope();
  const workspaceName = localizedWorkspaceName(workspaceNameToken, msg);
  const [pageIndex, setPageIndex] = useState(0);
  const [pageCursors, setPageCursors] = useState<FilesPageCursor[]>([{}]);
  const [downloadingFileId, setDownloadingFileId] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const uploadInputRef = useRef<HTMLInputElement>(null);
  const paginationWorkspaceIdRef = useRef(workspaceId);
  const cursor = paginationWorkspaceIdRef.current === workspaceId ? (pageCursors[pageIndex] ?? {}) : {};
  const filesQuery = useQuery({
    queryKey: ['files', workspaceId, cursor.afterId ?? '', cursor.beforeId ?? ''],
    queryFn: () => listFiles(cursor, workspaceId),
    retry: false,
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

  const handleUpload = async (event: ChangeEvent<HTMLInputElement>) => {
    const selectedFiles = Array.from(event.currentTarget.files ?? []);
    if (!selectedFiles.length || uploading) {
      return;
    }
    setUploading(true);
    try {
      const results = await Promise.allSettled(selectedFiles.map((file) => uploadFile(file, workspaceId)));
      const succeeded = results.filter((result) => result.status === 'fulfilled').length;
      const failed = results.find((result): result is PromiseRejectedResult => result.status === 'rejected');
      if (succeeded > 0) {
        if (pageIndex === 0) {
          await filesQuery.refetch();
        } else {
          await queryClient.invalidateQueries({
            queryKey: ['files', workspaceId, '', ''],
            exact: true,
            refetchType: 'none',
          });
          setPageIndex(0);
          setPageCursors([{}]);
        }
        toast.success(
          msg('files.upload.success', '{count, plural, one {File uploaded} other {# files uploaded}}', {
            count: succeeded,
          }),
        );
      }
      if (failed) {
        toast.error(msg('files.upload.error', 'File upload failed'), {
          description: errorMessage(failed.reason),
        });
      }
    } finally {
      setUploading(false);
      if (uploadInputRef.current) {
        uploadInputRef.current.value = '';
      }
    }
  };

  return (
    <>
      <section>
        <ResourcePageHeader
          contentGap="content"
          title={msg('files.title', 'Files')}
          actions={
            <>
              <input
                ref={uploadInputRef}
                className="sr-only"
                type="file"
                multiple
                aria-label={msg('files.upload.inputAria', 'Choose files to upload')}
                onChange={(event) => void handleUpload(event)}
              />
              <Button type="button" size="lg" disabled={uploading} onClick={() => uploadInputRef.current?.click()}>
                <Upload aria-hidden />
                {uploading ? msg('files.upload.uploading', 'Uploading...') : msg('files.upload.action', 'Upload files')}
              </Button>
            </>
          }
        />
        <FilesTable
          files={files}
          workspaceName={workspaceName}
          isLoading={filesQuery.isLoading}
          isFetching={filesQuery.isFetching}
          error={filesQuery.error}
          canPrevious={pageIndex > 0 && !filesQuery.isFetching}
          canNext={Boolean(response?.has_more && lastId) && !filesQuery.isFetching}
          currentPage={pageIndex + 1}
          totalPages={resourceListPageCount(response?.total_count, consoleResourceListLimit)}
          downloadingFileId={downloadingFileId}
          onRetry={() => void filesQuery.refetch()}
          onPrevious={goPrevious}
          onNext={goNext}
          onDownload={(file) => void handleDownload(file)}
        />
      </section>
    </>
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
  currentPage,
  totalPages,
  downloadingFileId,
  onRetry,
  onPrevious,
  onNext,
  onDownload,
}: {
  files: ConsoleFile[];
  workspaceName: string;
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  canPrevious: boolean;
  canNext: boolean;
  currentPage: number;
  totalPages: number | null;
  downloadingFileId: string | null;
  onRetry: () => void;
  onPrevious: () => void;
  onNext: () => void;
  onDownload: (file: ConsoleFile) => void;
}) {
  const { msg } = useI18n();
  const showRows = !isLoading && !error && files.length > 0;

  return (
    <section aria-label={msg('files.listAria', 'Files list')} className="min-w-0">
      <Table className={cn(dataTableClassName, !showRows && '[&_td]:whitespace-normal')}>
        {showRows || isLoading ? (
          <TableHeader>
            <TableRow className={dataTableHeaderRowClassName}>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTableIdColumnClassName)}>
                {msg('common.id', 'ID')}
              </TableHead>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTablePrimaryColumnClassName)}>
                {msg('common.name', 'Name')}
              </TableHead>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTableNumericColumnClassName)}>
                {msg('common.size', 'Size')}
              </TableHead>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTableTimeColumnClassName, 'pl-6')}>
                {msg('common.created', 'Created')}
              </TableHead>
              <TableHead
                className={cn(dataTableHeaderCellClassName, dataTableActionsColumnClassName)}
                aria-label={msg('common.actions', 'Actions')}
              />
            </TableRow>
          </TableHeader>
        ) : null}
        <TableBody>
          {isLoading ? (
            <TableSkeletonRows colCount={5} label={msg('files.loading', 'Loading files...')} />
          ) : error ? (
            <TableErrorRow
              colSpan={5}
              title={msg('files.error', 'Files could not be loaded.')}
              message={errorMessage(error)}
              retryLabel={msg('common.retry', 'Retry')}
              onRetry={onRetry}
            />
          ) : files.length === 0 ? null : (
            files.map((file) => (
              <DataTableRow key={file.id}>
                <DataTableCell edge="start" className={dataTableIdColumnClassName}>
                  <CopyIdCell value={file.id} ariaLabel={msg('files.copyAria', 'Copy {fileId}', { fileId: file.id })} />
                </DataTableCell>
                <DataTableCell className={cn(dataTablePrimaryColumnClassName, 'max-w-[420px] truncate font-medium')}>
                  <span title={file.filename}>{file.filename}</span>
                </DataTableCell>
                <DataTableCell className={cn(dataTableNumericColumnClassName, 'text-muted-foreground')}>
                  {formatBytes(file.size_bytes)}
                </DataTableCell>
                <DataTableCell className={cn(dataTableTimeColumnClassName, 'pl-6')}>
                  <RelativeTimeText value={file.created_at} />
                </DataTableCell>
                <DataTableCell edge="end" className={dataTableActionsColumnClassName}>
                  <RowIconButton
                    label={msg('files.downloadAria', 'Download {filename}', { filename: file.filename })}
                    icon={<Download aria-hidden />}
                    disabled={!file.downloadable || downloadingFileId === file.id}
                    onClick={() => onDownload(file)}
                  />
                </DataTableCell>
              </DataTableRow>
            ))
          )}
        </TableBody>
      </Table>

      {!isLoading && !error && files.length === 0 ? (
        <ResourceListState
          icon={FileText}
          title={msg('files.emptyTitle', 'No files yet')}
          body={msg('files.empty', 'Upload files to the {workspaceName} workspace to see them here.', {
            workspaceName,
          })}
        />
      ) : null}

      <CursorPagination
        updatingLabel={msg('common.updating', 'Updating...')}
        canPrevious={canPrevious}
        canNext={canNext}
        currentPage={currentPage}
        totalPages={totalPages}
        isUpdating={isFetching && !isLoading}
        onPrevious={onPrevious}
        onNext={onNext}
      />
    </section>
  );
}

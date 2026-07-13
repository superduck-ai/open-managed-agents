import { Download, FileText } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { cn } from "@/shared/lib/utils";
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  RowIconButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from "@/shared/ui/data-table-interactions";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/shared/ui/table";
import { useI18n } from "../../shared/i18n";
import { ConsolePageFrame, CursorPagination, TableEmptyRow, TableErrorRow, TableLoadingRow } from "./frame";
import {
  downloadFile,
  errorMessage,
  formatBytes,
  formatFileId,
  formatRelativeTime,
  listFiles,
  useDashboardWorkspaceScope,
  type ConsoleFile,
  type FilesPageCursor,
} from "./model";

export function FilesPage() {
  const { msg } = useI18n();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const [pageIndex, setPageIndex] = useState(0);
  const [pageCursors, setPageCursors] = useState<FilesPageCursor[]>([{}]);
  const [downloadingFileId, setDownloadingFileId] = useState<string | null>(null);
  const paginationWorkspaceIdRef = useRef(workspaceId);
  const cursor = paginationWorkspaceIdRef.current === workspaceId ? (pageCursors[pageIndex] ?? {}) : {};
  const filesQuery = useQuery({
    queryKey: ["files", workspaceId, cursor.afterId ?? "", cursor.beforeId ?? ""],
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

  return (
    <ConsolePageFrame
      title={msg("files.title", "Files")}
      icon={FileText}
      description={msg(
        "files.description",
        "Only files from the {workspaceName} workspace are shown. To see another workspace's files, select a workspace.",
        { workspaceName },
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
  onDownload,
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
    <section aria-label={msg("files.listAria", "Files list")} className="overflow-x-auto">
      <Table className={cn("min-w-[920px]", dataTableClassName)}>
        <colgroup>
          <col className="w-[18%]" />
          <col className="w-[49%]" />
          <col className="w-[11%]" />
          <col className="w-[15%]" />
          <col className="w-[7%]" />
        </colgroup>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            <TableHead className={dataTableHeaderCellClassName}>{msg("common.id", "ID")}</TableHead>
            <TableHead className={dataTableHeaderCellClassName}>{msg("common.name", "Name")}</TableHead>
            <TableHead className={dataTableHeaderCellClassName}>{msg("common.size", "Size")}</TableHead>
            <TableHead className={dataTableHeaderCellClassName}>{msg("common.created", "Created")}</TableHead>
            <TableHead className={dataTableHeaderCellClassName} aria-label={msg("common.actions", "Actions")} />
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <TableLoadingRow colSpan={5} label={msg("files.loading", "Loading files...")} />
          ) : error ? (
            <TableErrorRow
              colSpan={5}
              title={msg("files.error", "Files could not be loaded.")}
              message={errorMessage(error)}
              retryLabel={msg("common.retry", "Retry")}
              onRetry={onRetry}
            />
          ) : files.length === 0 ? (
            <TableEmptyRow colSpan={5}>
              {msg("files.empty", "No files have been uploaded to the {workspaceName} workspace.", {
                workspaceName,
              })}
            </TableEmptyRow>
          ) : (
            files.map((file) => (
              <DataTableRow key={file.id}>
                <DataTableCell edge="start">
                  <CopyIdCell
                    value={file.id}
                    displayValue={formatFileId(file.id)}
                    ariaLabel={msg("files.copyAria", "Copy {fileId}", { fileId: file.id })}
                  />
                </DataTableCell>
                <DataTableCell className="truncate">{file.filename}</DataTableCell>
                <DataTableCell className="text-muted-foreground">{formatBytes(file.size_bytes)}</DataTableCell>
                <DataTableCell className="text-muted-foreground">{formatRelativeTime(file.created_at)}</DataTableCell>
                <DataTableCell edge="end" className="text-right">
                  <RowIconButton
                    label={msg("files.downloadAria", "Download {filename}", { filename: file.filename })}
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

      <CursorPagination
        previousLabel={msg("pagination.previousPage", "Previous page")}
        nextLabel={msg("pagination.nextPage", "Next page")}
        updatingLabel={msg("common.updating", "Updating...")}
        canPrevious={canPrevious}
        canNext={canNext}
        isUpdating={isFetching && !isLoading}
        onPrevious={onPrevious}
        onNext={onNext}
      />
    </section>
  );
}

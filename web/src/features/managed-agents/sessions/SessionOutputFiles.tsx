import { Download, FileText, LoaderCircle } from 'lucide-react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { MessageScrollerItem } from '../../../shared/ui/message-scroller';
import type { FileMetadataApiResponse, SessionApiResponse } from '../types';
import { errorMessage } from '../utils';
import { useSessionOutputFiles } from './sessionFiles';
import { useSessionFileDownload } from './useSessionFileDownload';

type SessionOutputFilesProps = {
  session: SessionApiResponse;
  workspaceId: string;
  refreshKey: number;
};

export function SessionOutputFiles({
  mainLane = true,
  filtered = false,
  ...props
}: SessionOutputFilesProps & { mainLane?: boolean; filtered?: boolean }) {
  if (!mainLane || filtered) return null;
  return <SessionOutputFilesContent {...props} />;
}

function SessionOutputFilesContent({ session, workspaceId, refreshKey }: SessionOutputFilesProps) {
  const { msg } = useI18n();
  const { files, error, refresh } = useSessionOutputFiles(session, workspaceId, refreshKey);
  if (!files.length && !error) return null;
  return (
    <MessageScrollerItem messageId={`session-output-files-${session.id}`} className="mt-5">
      <section
        className="@container"
        data-testid="session-output-files"
        aria-label={msg('managedAgents.sessions.outputs.title', 'Output files')}
      >
        {files.length ? (
          <>
            <h3 className="text-sm font-medium text-muted-foreground">
              {msg('managedAgents.sessions.outputs.title', 'Output files')}
            </h3>
            <div className="mt-2 grid grid-cols-1 gap-2 @min-[480px]:grid-cols-2">
              {files.map((file) => (
                <SessionOutputFileCard key={file.id} file={file} workspaceId={workspaceId} />
              ))}
            </div>
          </>
        ) : null}
        {error ? (
          <div role="alert" className="mt-2 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
            <span>{msg('managedAgents.sessions.outputs.loadError', 'Output files could not be refreshed.')}</span>
            <span className="break-words">{errorMessage(error)}</span>
            <Button type="button" variant="link" size="xs" onClick={() => void refresh()}>
              {msg('common.retry', 'Retry')}
            </Button>
          </div>
        ) : null}
      </section>
    </MessageScrollerItem>
  );
}

function SessionOutputFileCard({ file, workspaceId }: { file: FileMetadataApiResponse; workspaceId: string }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const { download, downloading } = useSessionFileDownload(workspaceId);
  const typeLabel = /\.([a-z0-9]{1,8})$/i.exec(file.filename)?.[1].toUpperCase() ?? file.mime_type;
  return (
    <Button
      type="button"
      variant="ghost"
      disabled={downloading}
      aria-busy={downloading}
      aria-label={msg('files.downloadAria', 'Download {filename}', { filename: file.filename })}
      title={file.filename}
      className="h-auto min-w-0 justify-start gap-3 rounded-xl border-0 bg-muted/55 px-3 py-2.5 text-left font-normal hover:bg-muted/80 dark:hover:bg-muted"
      onClick={() => void download(file)}
    >
      <FileText className="size-5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium text-foreground">{file.filename}</span>
        <span className="truncate text-xs text-muted-foreground">
          {typeLabel} · {formatters.bytes(file.size_bytes)}
        </span>
      </span>
      {downloading ? (
        <LoaderCircle className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
      ) : (
        <Download className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      )}
    </Button>
  );
}

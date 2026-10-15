import { ChevronDown, Download, FileText, LoaderCircle } from 'lucide-react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Card } from '../../../shared/ui/card';
import { type ReactNode } from 'react';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { SessionFileContext } from './SessionFileContext';
import { sessionOutputFilePaths, type SessionOutputFile } from './sessionOutputModel';
import { sessionStatusIsLive } from './sessionDetailModel';
import type { SessionApiResponse } from '../types';
import { errorMessage } from '../utils';
import { useSessionOutputFiles } from './sessionFiles';
import { useSessionFileDownload } from './useSessionFileDownload';

type SessionOutputFilesProps = {
  session: SessionApiResponse;
  workspaceId: string;
  refreshKey: number;
  children: ReactNode;
};

export function SessionOutputFiles({
  mainLane = true,
  filtered = false,
  ...props
}: SessionOutputFilesProps & { mainLane?: boolean; filtered?: boolean }) {
  if (!mainLane || filtered) return props.children;
  return <SessionOutputFilesContent {...props} />;
}

function SessionOutputFilesContent({ session, workspaceId, refreshKey, children }: SessionOutputFilesProps) {
  const { msg } = useI18n();
  const { files, error, refresh } = useSessionOutputFiles(session, workspaceId, refreshKey);
  return (
    <SessionFileContext.Provider
      value={{
        files: sessionOutputFilePaths(files, session.resources),
        workspaceId,
        running: sessionStatusIsLive(session.status) && !session.archived_at,
      }}
    >
      {error ? (
        <div role="alert" className="my-2 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
          <span>{msg('managedAgents.sessions.outputs.loadError', 'Output files could not be refreshed.')}</span>
          <span className="break-words">{errorMessage(error)}</span>
          <Button type="button" variant="link" size="xs" onClick={() => void refresh()}>
            {msg('common.retry', 'Retry')}
          </Button>
        </div>
      ) : null}
      {children}
    </SessionFileContext.Provider>
  );
}

export function SessionOutputFileList({ files, workspaceId }: { files: SessionOutputFile[]; workspaceId: string }) {
  const { msg } = useI18n();
  if (!files.length) return null;
  const cards = (items: SessionOutputFile[]) =>
    items.map((file) => <SessionOutputFileCard key={file.id} file={file} workspaceId={workspaceId} />);
  return (
    <Card
      role="region"
      className="@container mt-3 gap-0 rounded-xl bg-muted/60 p-3 ring-border/60"
      data-testid="session-output-files"
      aria-label={msg('managedAgents.sessions.outputs.title', 'Output files')}
    >
      <h3 className="text-sm font-medium text-foreground/80">
        {msg('managedAgents.sessions.outputs.title', 'Output files')}
      </h3>
      <Collapsible key={files.map((file) => file.id).join('|')}>
        <div className="mt-2 grid grid-cols-1 gap-2 @min-[480px]:grid-cols-2">{cards(files.slice(0, 2))}</div>
        {files.length > 2 ? (
          <>
            <CollapsibleContent>
              <div className="mt-2 grid grid-cols-1 gap-2 @min-[480px]:grid-cols-2">{cards(files.slice(2))}</div>
            </CollapsibleContent>
            <CollapsibleTrigger
              render={<Button variant="ghost" size="sm" className="group mt-2 w-full text-muted-foreground" />}
            >
              <span className="group-data-[panel-open]:hidden">
                {msg('managedAgents.sessions.outputs.showAll', 'Show all {count} files', { count: files.length })}
              </span>
              <span className="hidden group-data-[panel-open]:inline">
                {msg('managedAgents.sessions.outputs.collapse', 'Collapse files')}
              </span>
              <ChevronDown className="size-4 group-data-[panel-open]:rotate-180" />
            </CollapsibleTrigger>
          </>
        ) : null}
      </Collapsible>
    </Card>
  );
}

function SessionOutputFileCard({ file, workspaceId }: { file: SessionOutputFile; workspaceId: string }) {
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
      title={file.path ?? file.filename}
      className="h-auto min-w-0 justify-start gap-3 rounded-xl border-0 bg-background/85 px-3 py-2.5 text-left font-normal hover:bg-background dark:hover:bg-background"
      onClick={() => void download(file)}
    >
      <FileText className="size-5 shrink-0 text-muted-foreground" aria-hidden />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium text-foreground">{file.filename}</span>
        <span className="truncate text-xs text-muted-foreground">{file.path}</span>
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

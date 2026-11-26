import {
  ChevronDown,
  Download,
  Eye,
  FileCode2,
  FileText,
  Globe,
  Image,
  Info,
  LoaderCircle,
  Table2,
} from 'lucide-react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Card } from '../../../shared/ui/card';
import type { RuntimeArtifactDiscoveryMode } from '../../../shared/workspaces/runtime';
import { type ReactNode, useState } from 'react';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { SessionFileContext, useSessionFiles } from './SessionFileContext';
import { SessionFilePreview } from './SessionFilePreview';
import { sessionFilePreviewKind, type SessionFilePreviewKind } from './sessionFilePreviewModel';
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
  artifactDiscovery?: RuntimeArtifactDiscoveryMode;
  hasEvents?: boolean;
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

function SessionOutputFilesContent({
  session,
  workspaceId,
  refreshKey,
  artifactDiscovery = 'filesystem_outputs_dir',
  hasEvents = false,
  children,
}: SessionOutputFilesProps) {
  const { msg } = useI18n();
  const { files, error, refresh } = useSessionOutputFiles(session, workspaceId, refreshKey);
  const [previewId, setPreviewId] = useState<string>();
  const outputFiles = sessionOutputFilePaths(files, session.resources);
  const previewFile = outputFiles.find((file) => file.id === previewId);
  const previewKind = previewFile && sessionFilePreviewKind(previewFile);
  const running = sessionStatusIsLive(session.status) && !session.archived_at;
  const showExplicitDeliverHint =
    artifactDiscovery === 'explicit_deliver_tool' && hasEvents && !running && !error && outputFiles.length === 0;
  return (
    <SessionFileContext.Provider
      value={{
        files: outputFiles,
        workspaceId,
        running,
        openPreview: setPreviewId,
      }}
    >
      {error ? (
        <div role="alert" className="my-2 flex min-w-0 flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
          <span>{msg('managedAgents.sessions.outputs.loadError', 'Output files could not be refreshed.')}</span>
          <span className="min-w-0 [overflow-wrap:anywhere]">{errorMessage(error)}</span>
          <Button type="button" variant="link" size="xs" onClick={() => void refresh()}>
            {msg('common.retry', 'Retry')}
          </Button>
        </div>
      ) : null}
      {children}
      {showExplicitDeliverHint ? (
        <div
          data-testid="session-deliver-artifacts-hint"
          className="mt-3 flex items-start gap-2 rounded-lg border-[0.5px] border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground"
        >
          <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          <span>
            {msg(
              'managedAgents.sessions.outputs.explicitDeliverHint',
              'No delivered artifacts yet. This runtime exports downloadable output files when the agent calls the DeliverArtifacts tool.',
            )}
          </span>
        </div>
      ) : null}
      {previewFile && previewKind ? (
        <SessionFilePreview
          key={previewFile.id}
          file={previewFile}
          kind={previewKind}
          workspaceId={workspaceId}
          onClose={() => setPreviewId(undefined)}
        />
      ) : null}
    </SessionFileContext.Provider>
  );
}

export function SessionOutputFileList({ files, workspaceId }: { files: SessionOutputFile[]; workspaceId: string }) {
  const { msg } = useI18n();
  const openPreview = useSessionFiles()?.openPreview;
  if (!files.length) return null;
  const cards = (items: SessionOutputFile[]) =>
    items.map((file) => (
      <SessionOutputFileCard key={file.id} file={file} workspaceId={workspaceId} openPreview={openPreview} />
    ));
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
              <ChevronDown className="size-4 transition-transform duration-200 ease-out group-data-[panel-open]:rotate-180 motion-reduce:transition-none" />
            </CollapsibleTrigger>
          </>
        ) : null}
      </Collapsible>
    </Card>
  );
}

const PREVIEW_ICONS: Record<SessionFilePreviewKind, typeof FileText> = {
  code: FileCode2,
  csv: Table2,
  docx: FileText,
  html: Globe,
  image: Image,
  markdown: FileText,
  pdf: FileText,
  xlsx: Table2,
};

function SessionOutputFileCard({
  file,
  workspaceId,
  openPreview,
}: {
  file: SessionOutputFile;
  workspaceId: string;
  openPreview?: (fileId: string) => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const { download, downloading } = useSessionFileDownload(workspaceId);
  const previewKind = openPreview ? sessionFilePreviewKind(file) : null;
  const Icon = previewKind ? PREVIEW_ICONS[previewKind] : FileText;
  const typeLabel = /\.([a-z0-9]{1,8})$/i.exec(file.filename)?.[1].toUpperCase() ?? file.mime_type;
  const downloadLabel = msg('files.downloadAria', 'Download {filename}', { filename: file.filename });
  const downloadIcon = downloading ? (
    <LoaderCircle className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
  ) : (
    <Download className="size-4 shrink-0 text-muted-foreground" aria-hidden />
  );
  return (
    <div className="flex min-w-0 items-center rounded-xl bg-background/85 hover:bg-background">
      <Button
        type="button"
        variant="ghost"
        disabled={!previewKind && downloading}
        aria-busy={!previewKind && downloading}
        aria-label={
          previewKind ? msg('files.previewAria', 'Preview {filename}', { filename: file.filename }) : downloadLabel
        }
        title={file.path ?? file.filename}
        className="group/card h-auto min-w-0 flex-1 justify-start gap-3 rounded-xl border-0 px-3 py-2.5 text-left font-normal hover:bg-transparent dark:hover:bg-transparent"
        onClick={() => (previewKind ? openPreview?.(file.id) : void download(file))}
      >
        <Icon className="size-5 shrink-0 text-muted-foreground" aria-hidden />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="truncate text-sm font-medium text-foreground">{file.filename}</span>
          <span className="truncate text-xs text-muted-foreground">{file.path}</span>
          <span className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
            <span className="min-w-0 truncate">{typeLabel}</span>
            <span className="shrink-0">{formatters.bytes(file.size_bytes)}</span>
          </span>
        </span>
        {previewKind ? (
          <Eye
            className="size-4 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/card:opacity-100 group-focus-visible/card:opacity-100 motion-reduce:transition-none"
            aria-hidden
          />
        ) : (
          downloadIcon
        )}
      </Button>
      {previewKind ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="mr-2 shrink-0"
          disabled={downloading}
          aria-busy={downloading}
          aria-label={downloadLabel}
          onClick={() => void download(file)}
        >
          {downloadIcon}
        </Button>
      ) : null}
    </div>
  );
}

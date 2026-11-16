import { useQuery } from '@tanstack/react-query';
import { Download, LoaderCircle, Maximize2, Minimize2 } from 'lucide-react';
import { useCallback, useState } from 'react';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { CopyButton } from '../../../shared/ui/copy-button';
import { Sheet, SheetContent, SheetTitle } from '../../../shared/ui/sheet';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { SyntaxCodeBlock } from '../components/CodeBlocks';
import { cn } from '../../../shared/lib/utils';
import { errorMessage } from '../utils';
import {
  loadSessionFilePreview,
  SESSION_FILE_PREVIEW_COLUMN_LIMIT,
  SESSION_FILE_PREVIEW_ROW_LIMIT,
  type SessionFilePreviewContent,
  type SessionFilePreviewKind,
  type SessionFilePreviewSheet,
} from './sessionFilePreviewModel';
import type { SessionOutputFile } from './sessionOutputModel';
import { MarkdownTranscriptContent } from './SessionTranscriptContent';
import { useSessionFileDownload } from './useSessionFileDownload';

export function SessionFilePreview({
  file,
  kind,
  workspaceId,
  onClose,
}: {
  file: SessionOutputFile;
  kind: SessionFilePreviewKind;
  workspaceId: string;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [expanded, setExpanded] = useState(false);
  const { download, downloading } = useSessionFileDownload(workspaceId);
  const preview = useQuery({
    queryKey: ['files', workspaceId, 'preview', file.id],
    queryFn: async ({ signal }) => {
      const response = await anthropicBetaApi.files.download(file.id, workspaceId, signal);
      const blob = await response.blob();
      signal.throwIfAborted();
      return loadSessionFilePreview(kind, blob, file.filename);
    },
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
  });
  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        showOverlay={false}
        className={cn(
          'gap-0 shadow-xl',
          expanded && 'sm:max-w-[calc(100vw-var(--console-panel-left,0px)-var(--console-panel-gap,0px))]',
        )}
        data-testid="session-file-preview"
      >
        <header className="flex items-center gap-1 border-b-[0.5px] border-border py-2 pr-11 pl-4">
          <SheetTitle className="min-w-0 truncate text-sm leading-7">{file.filename}</SheetTitle>
          <span className="mr-auto shrink-0 pl-2 text-xs text-muted-foreground">
            {formatters.bytes(file.size_bytes)}
          </span>
          {preview.data && 'text' in preview.data ? (
            <CopyButton value={preview.data.text} label={msg('common.copy', 'Copy')} />
          ) : null}
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            disabled={downloading}
            aria-busy={downloading}
            aria-label={msg('files.downloadAria', 'Download {filename}', { filename: file.filename })}
            onClick={() => void download(file)}
          >
            {downloading ? <LoaderCircle className="animate-spin" aria-hidden /> : <Download aria-hidden />}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="max-sm:hidden"
            aria-pressed={expanded}
            aria-label={
              expanded
                ? msg('managedAgents.sessions.outputs.previewRestore', 'Restore size')
                : msg('managedAgents.sessions.outputs.previewExpand', 'Expand preview')
            }
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? <Minimize2 aria-hidden /> : <Maximize2 aria-hidden />}
          </Button>
        </header>
        <div className="min-h-0 flex-1 overflow-auto" aria-busy={preview.isPending}>
          {preview.data ? (
            <PreviewContent content={preview.data} title={file.filename} />
          ) : preview.error ? (
            <PreviewError detail={errorMessage(preview.error)} />
          ) : (
            <div role="status" className="flex h-full items-center justify-center">
              <LoaderCircle className="size-4 animate-spin text-muted-foreground" aria-hidden />
              <span className="sr-only">{msg('common.loading', 'Loading...')}</span>
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function PreviewError({ detail }: { detail?: string }) {
  const { msg } = useI18n();
  return (
    <p role="alert" className="p-4 text-sm text-muted-foreground [overflow-wrap:anywhere]">
      {msg('managedAgents.sessions.outputs.previewError', 'Preview could not be loaded.')} {detail}
    </p>
  );
}

function PreviewContent({ content, title }: { content: SessionFilePreviewContent; title: string }) {
  switch (content.kind) {
    case 'code':
      return (
        <div className="p-4">
          <SyntaxCodeBlock value={content.text} language={content.language} />
        </div>
      );
    case 'markdown':
      return (
        <div className="p-4">
          <MarkdownTranscriptContent value={content.text} />
        </div>
      );
    case 'table':
      return <SheetTables sheets={content.sheets} />;
    case 'frame':
    case 'image':
      return <BlobPreview content={content} title={title} />;
  }
}

function BlobPreview({
  content,
  title,
}: {
  content: Extract<SessionFilePreviewContent, { blob: Blob }>;
  title: string;
}) {
  const { blob } = content;
  const [failed, setFailed] = useState(false);
  const attachObjectUrl = useCallback(
    (element: HTMLIFrameElement | HTMLImageElement | null) => {
      if (!element) return;
      const url = URL.createObjectURL(blob);
      element.src = url;
      return () => URL.revokeObjectURL(url);
    },
    [blob],
  );
  if (failed) return <PreviewError />;
  if (content.kind === 'image') {
    return (
      <img
        ref={attachObjectUrl}
        alt={title}
        referrerPolicy="no-referrer"
        className="mx-auto max-w-full p-4"
        onError={() => setFailed(true)}
      />
    );
  }
  return (
    <iframe
      ref={attachObjectUrl}
      title={title}
      sandbox={content.sandbox}
      referrerPolicy="no-referrer"
      className="size-full border-0 bg-white"
    />
  );
}

function SheetTables({ sheets }: { sheets: SessionFilePreviewSheet[] }) {
  if (sheets.length === 1) return <SheetTable sheet={sheets[0]} />;
  return (
    <Tabs defaultValue={0} className="gap-0">
      <TabsList className="m-4 mb-0 max-w-[calc(100%-2rem)] justify-start overflow-x-auto">
        {sheets.map((sheet, index) => (
          <TabsTrigger key={index} value={index} className="flex-none">
            {sheet.name}
          </TabsTrigger>
        ))}
      </TabsList>
      {sheets.map((sheet, index) => (
        <TabsContent key={index} value={index}>
          <SheetTable sheet={sheet} />
        </TabsContent>
      ))}
    </Tabs>
  );
}

function SheetTable({ sheet }: { sheet: SessionFilePreviewSheet }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [header = [], ...rows] = sheet.rows;
  return (
    <div className="p-4">
      <Table>
        <TableHeader>
          <TableRow>
            {header.map((cell, index) => (
              <TableHead key={index}>{cell}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, rowIndex) => (
            <TableRow key={rowIndex}>
              {row.map((cell, index) => (
                <TableCell key={index}>{cell}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {sheet.totalRows > sheet.rows.length ? (
        <p role="note" className="mt-2 text-xs text-muted-foreground">
          {msg('managedAgents.sessions.outputs.previewTruncated', 'Showing the first {limit} of {total} rows.', {
            limit: formatters.number(SESSION_FILE_PREVIEW_ROW_LIMIT),
            total: formatters.number(sheet.totalRows),
          })}
        </p>
      ) : null}
      {sheet.totalColumns > SESSION_FILE_PREVIEW_COLUMN_LIMIT ? (
        <p role="note" className="mt-2 text-xs text-muted-foreground">
          {msg(
            'managedAgents.sessions.outputs.previewColumnsTruncated',
            'Showing the first {limit} of {total} columns.',
            {
              limit: formatters.number(SESSION_FILE_PREVIEW_COLUMN_LIMIT),
              total: formatters.number(sheet.totalColumns),
            },
          )}
        </p>
      ) : null}
    </div>
  );
}

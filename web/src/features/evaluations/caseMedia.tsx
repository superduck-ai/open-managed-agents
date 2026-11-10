import { XIcon } from 'lucide-react';
import { useEffect, useState } from 'react';

import { anthropicBetaApi } from '../../shared/api/anthropic';
import { consoleRequestHeaders } from '../../shared/api/client';
import { useI18n } from '../../shared/i18n';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import type { EvaluationContentBlock } from './caseInputs';
import { fileBlocks, type CaseFile } from './caseInputs';

type FileResponse = { blob: () => Promise<Blob> };
type LoadFile = (orgUuid: string, workspaceId: string, fileId: string) => Promise<FileResponse>;
type LoadName = (fileId: string, workspaceId: string) => Promise<string>;

export function CaseInputMedia({
  inputs,
  orgUuid,
  workspaceId,
  loadFile = downloadCaseFile,
  loadName = downloadCaseName,
}: {
  inputs: EvaluationContentBlock[] | undefined;
  orgUuid: string;
  workspaceId: string;
  loadFile?: LoadFile;
  loadName?: LoadName;
}) {
  const files = fileBlocks(inputs);
  if (files.length === 0) return null;
  return (
    <div className="mt-1 flex flex-wrap items-center gap-1">
      {files.map((file) => (
        <CaseFileView
          key={file.id}
          file={file}
          orgUuid={orgUuid}
          workspaceId={workspaceId}
          size="sm"
          loadFile={loadFile}
          loadName={loadName}
        />
      ))}
    </div>
  );
}

export function CaseAttachments({
  files,
  orgUuid,
  workspaceId,
  onRemove,
  loadFile = downloadCaseFile,
  loadName = downloadCaseName,
}: {
  files: CaseFile[];
  orgUuid: string;
  workspaceId: string;
  onRemove: (id: string) => void;
  loadFile?: LoadFile;
  loadName?: LoadName;
}) {
  const { msg } = useI18n();
  return (
    <>
      {files.map((file) => (
        <span key={file.id} className="relative inline-flex max-w-full shrink-0 items-center">
          <CaseFileView
            file={file}
            orgUuid={orgUuid}
            workspaceId={workspaceId}
            size="md"
            loadFile={loadFile}
            loadName={loadName}
          />
          {file.kind === 'image' ? (
            <button
              type="button"
              className="absolute top-0.5 right-0.5 z-10 rounded-full bg-background/90 p-0.5 text-foreground"
              aria-label={msg('evaluations.removeFile', 'Remove')}
              onClick={() => onRemove(file.id)}
            >
              <XIcon className="size-3" />
            </button>
          ) : (
            <button type="button" className="ml-1 text-xs text-muted-foreground" onClick={() => onRemove(file.id)}>
              {msg('evaluations.removeFile', 'Remove')}
            </button>
          )}
        </span>
      ))}
    </>
  );
}

function CaseFileView({
  file,
  orgUuid,
  workspaceId,
  size,
  loadFile,
  loadName,
}: {
  file: CaseFile;
  orgUuid: string;
  workspaceId: string;
  size: 'sm' | 'md';
  loadFile: LoadFile;
  loadName: LoadName;
}) {
  if (file.kind === 'document') return <DocumentChip file={file} workspaceId={workspaceId} loadName={loadName} />;
  return (
    <CaseImageThumb fileId={file.id} orgUuid={orgUuid} workspaceId={workspaceId} size={size} loadFile={loadFile} />
  );
}

function DocumentChip({ file, workspaceId, loadName }: { file: CaseFile; workspaceId: string; loadName: LoadName }) {
  const { msg } = useI18n();
  const name = useDocumentName(file, workspaceId, loadName);
  return (
    <span className="inline-flex max-w-40 truncate rounded-md bg-muted px-2 py-1 text-xs">
      {name || msg('evaluations.documentName', 'Document')}
    </span>
  );
}

function CaseImageThumb({
  fileId,
  orgUuid,
  workspaceId,
  size,
  loadFile,
}: {
  fileId: string;
  orgUuid: string;
  workspaceId: string;
  size: 'sm' | 'md';
  loadFile: LoadFile;
}) {
  const { msg } = useI18n();
  const preview = useFileObjectUrl(fileId, orgUuid, workspaceId, loadFile);
  const [open, setOpen] = useState(false);
  const [broken, setBroken] = useState(false);
  if (preview.failed || broken) {
    return <span className="text-xs text-muted-foreground">{msg('evaluations.imageOnly', 'Image')}</span>;
  }
  if (!preview.url) return <span className={imagePlaceholderClass(size)} />;
  return (
    <>
      <button type="button" className={imageFrameClass(size)} onClick={() => setOpen(true)}>
        <img
          src={preview.url}
          alt={msg('evaluations.previewImage', 'View image')}
          className="size-full object-cover"
          onError={() => setBroken(true)}
        />
      </button>
      {open ? (
        <Dialog open onOpenChange={setOpen}>
          <DialogContent className="sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>{msg('evaluations.previewImage', 'View image')}</DialogTitle>
            </DialogHeader>
            <img src={preview.url} alt="" className="max-h-[70vh] w-full object-contain" />
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}

function imagePlaceholderClass(size: 'sm' | 'md') {
  if (size === 'md') return 'inline-block size-16 rounded-md bg-muted';
  return 'inline-block size-10 rounded-md bg-muted';
}

function imageFrameClass(size: 'sm' | 'md') {
  if (size === 'md') return 'size-16 shrink-0 overflow-hidden rounded-md border border-border';
  return 'size-10 shrink-0 overflow-hidden rounded-md border border-border';
}

function useDocumentName(file: CaseFile, workspaceId: string, loadName: LoadName) {
  const [name, setName] = useState(file.name ?? '');
  useEffect(() => {
    if (file.name) return;
    const controller = new AbortController();
    void loadName(file.id, workspaceId)
      .then((value) => {
        if (!controller.signal.aborted && value) setName(value);
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [file.id, file.name, loadName, workspaceId]);
  return file.name || name;
}

type PreviewState = { id: string; status: 'ready'; url: string } | { id: string; status: 'failed' };

function useFileObjectUrl(fileId: string, orgUuid: string, workspaceId: string, loadFile: LoadFile) {
  const [state, setState] = useState<PreviewState>();
  useEffect(() => {
    const controller = new AbortController();
    let objectUrl = '';
    void loadFile(orgUuid, workspaceId, fileId)
      .then((response) => response.blob())
      .then((blob) => {
        if (controller.signal.aborted) return;
        objectUrl = URL.createObjectURL(blob);
        setState({ id: fileId, status: 'ready', url: objectUrl });
      })
      .catch(() => {
        if (!controller.signal.aborted) setState({ id: fileId, status: 'failed' });
      });
    return () => {
      controller.abort();
      setState(undefined);
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [fileId, loadFile, orgUuid, workspaceId]);
  if (state?.id === fileId && state.status === 'failed') return { url: '', failed: true };
  if (state?.id === fileId && state.status === 'ready') return { url: state.url, failed: false };
  return { url: '', failed: false };
}

function downloadCaseFile(orgUuid: string, workspaceId: string, fileId: string) {
  const path = `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/files/${encodeURIComponent(fileId)}/content`;
  return fetch(path, { credentials: 'include', headers: consoleRequestHeaders() }).then((response) => {
    if (!response.ok) throw new Error('image unavailable');
    return response;
  });
}

function downloadCaseName(fileId: string, workspaceId: string) {
  return anthropicBetaApi.files
    .retrieveMetadata<{ filename?: string }>(fileId, workspaceId)
    .then((meta) => meta.filename ?? '');
}

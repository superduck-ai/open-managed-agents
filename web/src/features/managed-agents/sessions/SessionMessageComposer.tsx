import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { ArrowUp, CircleAlert, FileText, FileUp, ImagePlus, Loader2, Plus, Square, X } from 'lucide-react';
import { type ChangeEvent, useEffect, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { interruptQuickstartSession, postQuickstartSessionMessage, uploadQuickstartSessionAttachment } from '../api';
import { type QuickstartSessionEvent, type SessionMessageContentBlock } from '../types';
import {
  quickstartComposerFrameClassName,
  quickstartComposerSendButtonClassName,
  quickstartComposerTextareaClassName,
} from '../components/composerStyles';
import { errorMessage } from '../utils';

type ComposerAttachment = {
  id: string;
  fileId?: string;
  filename: string;
  mimeType: string;
  previewUrl?: string;
  sizeBytes: number;
  status: 'preparing' | 'ready' | 'failed';
};

const supportedSessionImageMimeTypes = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp']);

function isSupportedSessionImage(mimeType: string) {
  return supportedSessionImageMimeTypes.has(mimeType.toLowerCase());
}

function attachmentContentBlock(attachment: ComposerAttachment): SessionMessageContentBlock | null {
  if (!attachment.fileId || attachment.status !== 'ready') {
    return null;
  }
  const source = { type: 'file' as const, file_id: attachment.fileId };
  return isSupportedSessionImage(attachment.mimeType)
    ? { type: 'image', source, filename: attachment.filename }
    : { type: 'document', source, title: attachment.filename };
}

function formatAttachmentSize(sizeBytes: number) {
  if (sizeBytes < 1024) {
    return `${sizeBytes} B`;
  }
  if (sizeBytes < 1024 * 1024) {
    return `${Math.max(1, Math.round(sizeBytes / 1024))} KB`;
  }
  return `${(sizeBytes / (1024 * 1024)).toFixed(1)} MB`;
}

function createAttachmentPreview(file: File) {
  if (!isSupportedSessionImage(file.type) || typeof URL.createObjectURL !== 'function') {
    return undefined;
  }
  return URL.createObjectURL(file);
}

export function SessionMessageComposer({
  disabled,
  live,
  onError,
  onEventsChanged,
  onMessageSent,
  sessionId,
  workspaceId,
}: {
  disabled: boolean;
  live: boolean;
  onError: (error: string | null) => void;
  onEventsChanged: () => void;
  onMessageSent: (events: QuickstartSessionEvent[]) => void;
  sessionId: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const [draft, setDraft] = useState('');
  const [attachments, setAttachments] = useState<ComposerAttachment[]>([]);
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const imageInputRef = useRef<HTMLInputElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const attachmentSequenceRef = useRef(0);
  const previewUrlsRef = useRef(new Set<string>());
  // In-flight guards are refs: state updates only apply after a re-render, so a
  // second Enter (or Enter + send click) in the same frame would pass a state
  // guard and double-post the message.
  const sendingRef = useRef(false);
  const interruptingRef = useRef(false);
  const trimmedDraft = draft.trim();
  const preparing = attachments.some((attachment) => attachment.status === 'preparing');
  const preparationFailed = attachments.some((attachment) => attachment.status === 'failed');
  const readyAttachments = attachments.flatMap((attachment) => {
    const block = attachmentContentBlock(attachment);
    return block ? [block] : [];
  });
  const canSubmit =
    !disabled && !sending && !preparing && !preparationFailed && Boolean(trimmedDraft || readyAttachments.length);

  useEffect(
    () => () => {
      if (typeof URL.revokeObjectURL !== 'function') {
        return;
      }
      previewUrlsRef.current.forEach((url) => URL.revokeObjectURL(url));
      previewUrlsRef.current.clear();
    },
    [],
  );

  const releasePreview = (previewUrl?: string) => {
    if (!previewUrl || !previewUrlsRef.current.delete(previewUrl) || typeof URL.revokeObjectURL !== 'function') {
      return;
    }
    URL.revokeObjectURL(previewUrl);
  };

  const removeAttachment = (attachmentId: string) => {
    releasePreview(attachments.find((attachment) => attachment.id === attachmentId)?.previewUrl);
    setAttachments((current) => current.filter((attachment) => attachment.id !== attachmentId));
  };

  const prepareFiles = async (files: File[]) => {
    if (!files.length || disabled || sendingRef.current) {
      return;
    }
    onError(null);
    const pendingUploads = files.map((file) => {
      attachmentSequenceRef.current += 1;
      const previewUrl = createAttachmentPreview(file);
      if (previewUrl) {
        previewUrlsRef.current.add(previewUrl);
      }
      return {
        file,
        attachment: {
          id: `session-attachment-${attachmentSequenceRef.current}`,
          filename: file.name,
          mimeType: file.type,
          previewUrl,
          sizeBytes: file.size,
          status: 'preparing' as const,
        },
      };
    });
    setAttachments((current) => [...current, ...pendingUploads.map(({ attachment }) => attachment)]);

    await Promise.all(
      pendingUploads.map(async ({ file, attachment: pending }) => {
        try {
          const { uploaded } = await uploadQuickstartSessionAttachment(file, sessionId, workspaceId);
          setAttachments((current) =>
            current.map((attachment) =>
              attachment.id === pending.id
                ? {
                    ...attachment,
                    fileId: uploaded.id,
                    filename: uploaded.filename || attachment.filename,
                    mimeType: uploaded.mime_type || attachment.mimeType,
                    sizeBytes: uploaded.size_bytes ?? attachment.sizeBytes,
                    status: 'ready',
                  }
                : attachment,
            ),
          );
        } catch (error) {
          setAttachments((current) =>
            current.map((attachment) =>
              attachment.id === pending.id ? { ...attachment, status: 'failed' } : attachment,
            ),
          );
          onError(
            msg('managedAgents.sessions.detail.attachmentPrepareFailed', 'Could not attach {filename}.', {
              filename: pending.filename,
            }) + ` ${errorMessage(error)}`,
          );
        }
      }),
    );
  };

  const handleFilesSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.currentTarget.files ?? []);
    event.currentTarget.value = '';
    void prepareFiles(files);
  };

  const submit = async () => {
    if (!canSubmit || sendingRef.current) {
      return;
    }
    const content: SessionMessageContentBlock[] = [];
    if (trimmedDraft) {
      content.push({ type: 'text', text: trimmedDraft });
    }
    content.push(...readyAttachments);

    sendingRef.current = true;
    setSending(true);
    onError(null);
    try {
      const response = await postQuickstartSessionMessage(sessionId, content, workspaceId);
      setDraft('');
      attachments.forEach((attachment) => releasePreview(attachment.previewUrl));
      setAttachments([]);
      // Always mark the session running: even with no returned events the stream
      // loop's forced tail sync backfills the message, so no history-refresh
      // fallback is needed.
      onMessageSent(response.data ?? []);
    } catch (error) {
      onError(errorMessage(error));
    } finally {
      sendingRef.current = false;
      setSending(false);
    }
  };

  const interrupt = async () => {
    if (interruptingRef.current) return;
    interruptingRef.current = true;
    setInterrupting(true);
    onError(null);
    try {
      await interruptQuickstartSession(sessionId, workspaceId);
      onEventsChanged();
    } catch (error) {
      onError(errorMessage(error));
    } finally {
      interruptingRef.current = false;
      setInterrupting(false);
    }
  };

  return (
    <div className="px-3 pb-3 pt-2" data-testid="session-message-composer">
      <input
        ref={imageInputRef}
        className="sr-only"
        type="file"
        multiple
        accept="image/jpeg,image/png,image/gif,image/webp"
        aria-label={msg('managedAgents.sessions.detail.chooseImages', 'Choose images to upload')}
        onChange={handleFilesSelected}
      />
      <input
        ref={fileInputRef}
        className="sr-only"
        type="file"
        multiple
        aria-label={msg('managedAgents.sessions.detail.chooseFiles', 'Choose files to upload')}
        onChange={handleFilesSelected}
      />
      <InputGroup
        data-disabled={disabled || sending ? 'true' : undefined}
        className={`${quickstartComposerFrameClassName} items-stretch gap-0 p-0 pl-0 shadow-none`}
      >
        {attachments.length ? (
          <InputGroupAddon align="block-start" className="overflow-x-auto px-3 pb-0 pt-3">
            <div
              className="flex min-w-0 items-center gap-2"
              aria-label={msg('managedAgents.sessions.detail.attachments', 'Message attachments')}
            >
              {attachments.map((attachment) => (
                <div
                  key={attachment.id}
                  data-attachment-status={attachment.status}
                  className="group/attachment flex max-w-56 items-center gap-2 rounded-md border border-border bg-muted/35 p-1.5 pr-1 text-left"
                >
                  <span className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-sm bg-background text-muted-foreground">
                    {attachment.previewUrl ? (
                      <img src={attachment.previewUrl} alt="" className="size-full object-cover" />
                    ) : attachment.status === 'failed' ? (
                      <CircleAlert className="size-4 text-destructive" aria-hidden />
                    ) : (
                      <FileText className="size-4" aria-hidden />
                    )}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-xs font-medium text-foreground">{attachment.filename}</span>
                    <span className="mt-0.5 flex items-center gap-1 text-[11px] font-normal text-muted-foreground">
                      {attachment.status === 'preparing' ? (
                        <>
                          <Loader2 className="size-3 animate-spin" aria-hidden />
                          {msg('managedAgents.sessions.detail.preparingAttachment', 'Uploading and mounting')}
                        </>
                      ) : attachment.status === 'failed' ? (
                        msg('managedAgents.sessions.detail.attachmentFailed', 'Attachment failed')
                      ) : (
                        formatAttachmentSize(attachment.sizeBytes)
                      )}
                    </span>
                  </span>
                  <InputGroupButton
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    className="shrink-0 text-muted-foreground hover:bg-accent hover:text-foreground"
                    aria-label={msg('managedAgents.sessions.detail.removeAttachment', 'Remove {filename}', {
                      filename: attachment.filename,
                    })}
                    disabled={sending}
                    onClick={() => removeAttachment(attachment.id)}
                  >
                    <X className="size-3.5" aria-hidden />
                  </InputGroupButton>
                </div>
              ))}
            </div>
          </InputGroupAddon>
        ) : null}
        <InputGroupTextarea
          value={draft}
          rows={2}
          aria-label={msg('managedAgents.sessions.detail.messageLabel', 'Message')}
          placeholder={
            disabled
              ? msg('managedAgents.sessions.detail.messageDisabled', 'This session can no longer receive messages.')
              : msg('managedAgents.sessions.detail.messagePlaceholder', 'Send a message to this session...')
          }
          className={`${quickstartComposerTextareaClassName} subtle-scrollbar-auto block max-h-40 min-h-[52px] overflow-y-auto px-4 pb-2 pt-3 text-[15px] leading-6`}
          disabled={disabled || sending}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (
              event.key !== 'Enter' ||
              event.shiftKey ||
              event.repeat ||
              event.nativeEvent.isComposing ||
              !canSubmit
            ) {
              return;
            }
            event.preventDefault();
            void submit();
          }}
        />
        <InputGroupAddon align="block-end" className="justify-between px-3 pb-3 pt-0">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <InputGroupButton
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
                  aria-label={msg('managedAgents.sessions.detail.addAttachment', 'Add attachment')}
                  disabled={disabled || sending}
                />
              }
            >
              <Plus className="size-4" aria-hidden />
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" align="start" sideOffset={8} className="w-44 p-1.5">
              <DropdownMenuItem className="h-9 gap-2 px-2" onClick={() => imageInputRef.current?.click()}>
                <ImagePlus className="size-4" aria-hidden />
                {msg('managedAgents.sessions.detail.uploadImage', 'Upload image')}
              </DropdownMenuItem>
              <DropdownMenuItem className="h-9 gap-2 px-2" onClick={() => fileInputRef.current?.click()}>
                <FileUp className="size-4" aria-hidden />
                {msg('managedAgents.sessions.detail.uploadFile', 'Upload file')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <div className="flex shrink-0 items-center gap-1.5">
            {live ? (
              <InputGroupButton
                type="button"
                variant="ghost"
                size="icon-sm"
                className="rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
                aria-label={
                  interrupting
                    ? msg('managedAgents.sessions.detail.stopping', 'Stopping session')
                    : msg('managedAgents.sessions.detail.stop', 'Stop session')
                }
                disabled={interrupting}
                onClick={() => void interrupt()}
              >
                <Square className="size-3.5 fill-current" aria-hidden />
              </InputGroupButton>
            ) : null}
            <InputGroupButton
              type="button"
              variant="ghost"
              size="icon-sm"
              className={quickstartComposerSendButtonClassName}
              aria-label={
                sending
                  ? msg('managedAgents.sessions.detail.sending', 'Sending message')
                  : msg('managedAgents.sessions.detail.send', 'Send message')
              }
              disabled={!canSubmit}
              onClick={() => void submit()}
            >
              <ArrowUp className="size-4" aria-hidden />
            </InputGroupButton>
          </div>
        </InputGroupAddon>
      </InputGroup>
    </div>
  );
}

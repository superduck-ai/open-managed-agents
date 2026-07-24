import {
  FileText,
  Image as ImageIcon,
  Link2,
  Loader2,
  Paperclip,
  PencilLine,
  RefreshCw,
  Search,
  Sparkles,
  Trash2,
  Upload,
} from 'lucide-react';
import { Editor, EditorContent, JSONContent, useEditor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import {
  ChangeEvent,
  Dispatch,
  DragEvent,
  SetStateAction,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import clsx from 'clsx';
import { Button } from '@/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/shared/ui/dropdown-menu';
import { Input } from '@/shared/ui/input';
import { Label } from '@/shared/ui/label';
import { WorkbenchMessage } from './api';
import {
  messageAttachments,
  messageText,
  splitVariableText,
  WorkbenchAttachmentKind,
  WorkbenchPromptGeneratorWarning,
} from './model';
import { Dialog } from './dialogs';
import { IconButton } from './components';

export const workbenchPlainTextStarterKit = StarterKit.configure({
  blockquote: false,
  bold: false,
  bulletList: false,
  code: false,
  codeBlock: false,
  dropcursor: false,
  gapcursor: false,
  heading: false,
  horizontalRule: false,
  italic: false,
  link: false,
  listItem: false,
  listKeymap: false,
  orderedList: false,
  strike: false,
  underline: false,
});

export function TiptapTextInput({
  ariaLabel,
  text,
  onChange,
  className,
  isReadOnly = false,
}: {
  ariaLabel: string;
  text: string;
  onChange: (text: string) => void;
  className: string;
  isReadOnly?: boolean;
}) {
  const onChangeRef = useRef(onChange);
  const textRef = useRef(text);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  const editor = useEditor(
    {
      extensions: [workbenchPlainTextStarterKit],
      content: plainTextToTiptapDoc(text),
      editable: !isReadOnly,
      editorProps: {
        attributes: {
          role: 'textbox',
          'aria-label': ariaLabel,
          'aria-multiline': 'true',
          spellcheck: 'false',
          class: clsx(className, isReadOnly && 'is-read-only'),
        },
      },
      onUpdate: ({ editor: updatedEditor }) => {
        const nextText = readTiptapEditorText(updatedEditor);
        if (nextText !== textRef.current) {
          textRef.current = nextText;
          onChangeRef.current(nextText);
        }
      },
    },
    [ariaLabel, className, isReadOnly],
  );

  const syncTextFromEditor = useCallback(() => {
    if (!editor) {
      return;
    }
    const nextText = readTiptapEditorText(editor);
    if (nextText !== textRef.current) {
      textRef.current = nextText;
      onChangeRef.current(nextText);
    }
  }, [editor]);

  useEffect(() => {
    if (!editor) {
      return;
    }
    editor.setEditable(!isReadOnly);
    (editor.view.dom as HTMLElement).classList.toggle('is-read-only', isReadOnly);
  }, [editor, isReadOnly]);

  useEffect(() => {
    if (!editor) {
      return;
    }
    textRef.current = text;
    if (readTiptapEditorText(editor) !== text) {
      setTiptapEditorText(editor, text, false);
    }
  }, [editor, text]);

  useEffect(() => {
    if (!editor) {
      return;
    }
    const editorElement = editor.view.dom as HTMLElement;
    installTiptapTextValueShim(editorElement, editor);
    editorElement.addEventListener('change', syncTextFromEditor);
    return () => editorElement.removeEventListener('change', syncTextFromEditor);
  }, [editor, syncTextFromEditor]);

  return <EditorContent editor={editor} className="workbench-tiptap-shell" />;
}

export function SystemPromptEditableInput({
  text,
  onChange,
  isReadOnly = false,
}: {
  text: string;
  onChange: (text: string) => void;
  isReadOnly?: boolean;
}) {
  return (
    <TiptapTextInput
      ariaLabel="System prompt"
      text={text}
      onChange={onChange}
      className="workbench-system-editor"
      isReadOnly={isReadOnly}
    />
  );
}

export function MessageEditableInput({
  label,
  index,
  text,
  onChange,
  isReadOnly = false,
}: {
  label: string;
  index: number;
  text: string;
  onChange: (index: number, text: string) => void;
  isReadOnly?: boolean;
}) {
  return (
    <TiptapTextInput
      ariaLabel={`${label} prompt ${index + 1}`}
      text={text}
      onChange={(nextText) => onChange(index, nextText)}
      className="workbench-message-textarea"
      isReadOnly={isReadOnly}
    />
  );
}

export function MessageEditor({
  message,
  index,
  messages,
  onChange,
  onRemove,
  onUpload,
  onAddUrl,
  onReplaceFile,
  onRemoveFile,
  onVariableClick,
  isReadOnly = false,
  onShowPromptGenerator,
  promptGeneratorWarning = null,
  isGeneratingPrompt = false,
  isUploading = false,
  uploadError = null,
}: {
  message: WorkbenchMessage;
  index: number;
  messages: WorkbenchMessage[];
  onChange: (index: number, text: string) => void;
  onRemove: (index: number) => void;
  onUpload: (index: number, file: File) => void | Promise<void>;
  onAddUrl: (index: number, kind: WorkbenchAttachmentKind, url: string) => void;
  onReplaceFile: (index: number, blockIndex: number, file: File) => void | Promise<void>;
  onRemoveFile: (index: number, blockIndex: number) => void;
  onVariableClick: (name: string) => void;
  isReadOnly?: boolean;
  onShowPromptGenerator?: () => void;
  promptGeneratorWarning?: WorkbenchPromptGeneratorWarning | null;
  isGeneratingPrompt?: boolean;
  isUploading?: boolean;
  uploadError?: string | null;
}) {
  const label = message.role === 'assistant' ? 'Assistant' : 'User';
  const removalLabel = messageRemovalLabel(messages, index, label);
  const imageUploadInputRef = useRef<HTMLInputElement | null>(null);
  const pdfUploadInputRef = useRef<HTMLInputElement | null>(null);
  const replacementBlockIndexRef = useRef<number | null>(null);
  const dragDepthRef = useRef(0);
  const [isDragOver, setIsDragOver] = useState(false);
  const [attachmentMenuOpen, setAttachmentMenuOpen] = useState(false);
  const [urlDialogKind, setUrlDialogKind] = useState<WorkbenchAttachmentKind | null>(null);
  const [attachmentUrl, setAttachmentUrl] = useState('');
  const attachments = messageAttachments(message);
  const text = messageText(message);
  const variableParts = useMemo(() => splitVariableText(text), [text]);
  const hasVariableTokens = variableParts.some((part) => part.type === 'variable');
  const isFirstUserMessage = message.role === 'human' && index === 0;
  const showPlaceholder = message.role === 'human' && text.length === 0 && !hasVariableTokens;

  const uploadFiles = (files: FileList | File[]) => {
    if (isReadOnly) {
      return;
    }
    Array.from(files)
      .slice(0, 100)
      .forEach((file) => {
        void onUpload(index, file);
      });
  };
  const handleUploadChange = (event: ChangeEvent<HTMLInputElement>) => {
    const files = event.currentTarget.files;
    const file = files?.[0];
    event.currentTarget.value = '';
    if (file && !isReadOnly) {
      const replacementBlockIndex = replacementBlockIndexRef.current;
      replacementBlockIndexRef.current = null;
      if (replacementBlockIndex !== null) {
        void onReplaceFile(index, replacementBlockIndex, file);
      } else {
        uploadFiles(files);
      }
    }
  };
  const openUploadPicker = (kind: WorkbenchAttachmentKind, replacementBlockIndex: number | null = null) => {
    if (isReadOnly) {
      return;
    }
    replacementBlockIndexRef.current = replacementBlockIndex;
    setAttachmentMenuOpen(false);
    if (kind === 'image') {
      imageUploadInputRef.current?.click();
    } else {
      pdfUploadInputRef.current?.click();
    }
  };
  const openUrlDialog = (kind: WorkbenchAttachmentKind) => {
    if (isReadOnly) {
      return;
    }
    setAttachmentUrl('');
    setUrlDialogKind(kind);
    setAttachmentMenuOpen(false);
  };
  const closeUrlDialog = () => {
    setUrlDialogKind(null);
    setAttachmentUrl('');
  };
  const addUrlAttachment = () => {
    const url = attachmentUrl.trim();
    if (!urlDialogKind || !url || isReadOnly) {
      return;
    }
    onAddUrl(index, urlDialogKind, url);
    closeUrlDialog();
  };
  const hasDraggedFiles = (event: DragEvent<HTMLElement>) => Array.from(event.dataTransfer.types).includes('Files');
  const handleDragEnter = (event: DragEvent<HTMLElement>) => {
    if (isReadOnly || message.role !== 'human' || !hasDraggedFiles(event)) {
      return;
    }
    event.preventDefault();
    dragDepthRef.current += 1;
    setIsDragOver(true);
  };
  const handleDragOver = (event: DragEvent<HTMLElement>) => {
    if (isReadOnly || message.role !== 'human' || !hasDraggedFiles(event)) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = 'copy';
    setIsDragOver(true);
  };
  const handleDragLeave = (event: DragEvent<HTMLElement>) => {
    if (isReadOnly || message.role !== 'human' || !hasDraggedFiles(event)) {
      return;
    }
    event.preventDefault();
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
    if (dragDepthRef.current === 0) {
      setIsDragOver(false);
    }
  };
  const handleDrop = (event: DragEvent<HTMLElement>) => {
    if (isReadOnly || message.role !== 'human' || !event.dataTransfer.files.length) {
      return;
    }
    event.preventDefault();
    dragDepthRef.current = 0;
    setIsDragOver(false);
    uploadFiles(event.dataTransfer.files);
  };
  return (
    <>
      <section
        className={clsx('workbench-message-card', message.role === 'human' && isDragOver && 'is-drag-over')}
        onDragEnter={handleDragEnter}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <div className="workbench-message-header">
          <h2 className="workbench-message-title">{label}</h2>
          <div className="flex items-center gap-1">
            {message.role === 'human' ? (
              <>
                <input
                  ref={imageUploadInputRef}
                  type="file"
                  multiple
                  accept="image/*"
                  aria-hidden="true"
                  tabIndex={-1}
                  data-testid={`workbench-upload-input-${index}-image`}
                  className="workbench-hidden-file-input"
                  onChange={handleUploadChange}
                />
                <input
                  ref={pdfUploadInputRef}
                  type="file"
                  multiple
                  accept=".pdf,application/pdf"
                  aria-hidden="true"
                  tabIndex={-1}
                  data-testid={`workbench-upload-input-${index}`}
                  className="workbench-hidden-file-input"
                  onChange={handleUploadChange}
                />
                <div className="workbench-attachment-control">
                  <DropdownMenu open={attachmentMenuOpen} onOpenChange={setAttachmentMenuOpen}>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          aria-label="Upload up to 100 files, 20MB per file."
                          title="Upload up to 100 files, 20MB per file."
                          aria-expanded={attachmentMenuOpen}
                          className={clsx('workbench-attachment-trigger', attachmentMenuOpen && 'is-open')}
                          disabled={isUploading || isReadOnly}
                        />
                      }
                    >
                      {isUploading ? (
                        <Loader2 className="size-4 animate-spin" aria-hidden />
                      ) : (
                        <Paperclip className="size-4" aria-hidden />
                      )}
                    </DropdownMenuTrigger>
                    <DropdownMenuContent
                      aria-label="Attachment type"
                      align="end"
                      side="bottom"
                      sideOffset={8}
                      className="w-32 min-w-32 rounded-xl p-1.5"
                    >
                      <AttachmentTypeSubmenu
                        kind="image"
                        label="Image"
                        onUpload={openUploadPicker}
                        onAddUrl={openUrlDialog}
                      />
                      <AttachmentTypeSubmenu
                        kind="pdf"
                        label="PDF"
                        onUpload={openUploadPicker}
                        onAddUrl={openUrlDialog}
                      />
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </>
            ) : null}
            {index > 0 ? (
              <IconButton label={removalLabel} disabled={isReadOnly} compact onClick={() => onRemove(index)}>
                <Trash2 className="size-4" aria-hidden />
              </IconButton>
            ) : null}
          </div>
        </div>
        {message.role === 'human' ? (
          <div className="workbench-message-drop-target" aria-hidden={!isDragOver}>
            <Paperclip className="size-4" aria-hidden />
            <span>Drop here to insert into user message</span>
            <small>Max 100 files at 20MB each</small>
          </div>
        ) : null}
        <div
          className={clsx(
            'workbench-message-input-wrap',
            hasVariableTokens && 'has-variable-layer',
            showPlaceholder && 'has-placeholder',
            showPlaceholder && isFirstUserMessage && 'has-generator-placeholder',
          )}
        >
          {showPlaceholder ? (
            <MessageEditorPlaceholder
              placeholder={
                isFirstUserMessage
                  ? 'or enter instructions for the selected model…'
                  : 'Enter instructions for the selected model…'
              }
              showGeneratePrompt={isFirstUserMessage}
              warning={promptGeneratorWarning}
              isReadOnly={isReadOnly}
              isGenerating={isGeneratingPrompt}
              onShowPromptGenerator={onShowPromptGenerator}
            />
          ) : null}
          {hasVariableTokens ? (
            <div className="workbench-message-variable-layer">
              {variableParts.map((part, partIndex) =>
                part.type === 'variable' ? (
                  <span key={`${part.name}-${partIndex}`} className="workbench-message-variable-token">
                    <span className="workbench-message-variable-name" aria-hidden>
                      {part.name}
                    </span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`Add value for ${part.name}`}
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => onVariableClick(part.name)}
                    >
                      <PencilLine className="size-3.5" aria-hidden />
                    </Button>
                  </span>
                ) : (
                  <span key={`text-${partIndex}`} aria-hidden>
                    {part.text}
                  </span>
                ),
              )}
            </div>
          ) : null}
          <MessageEditableInput label={label} index={index} text={text} onChange={onChange} isReadOnly={isReadOnly} />
        </div>
        {attachments.length ? (
          <div className="workbench-message-attachments" aria-label={`${label} prompt attachments`}>
            {attachments.map((attachment) => (
              <div key={attachment.id} className="workbench-message-attachment">
                <span className="workbench-message-attachment-thumb" aria-hidden>
                  {attachment.kind === 'image' ? <ImageIcon className="size-4" /> : <Paperclip className="size-4" />}
                </span>
                <span className="workbench-message-attachment-name">{attachment.label}</span>
                <span className="workbench-message-attachment-actions">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Preview ${attachment.label}`}
                    disabled
                  >
                    <Search className="size-3.5" aria-hidden />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Replace ${attachment.label}`}
                    disabled={isUploading || isReadOnly}
                    onClick={() =>
                      openUploadPicker(attachment.kind === 'image' ? 'image' : 'pdf', attachment.blockIndex)
                    }
                  >
                    <RefreshCw className="size-3.5" aria-hidden />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Remove ${attachment.label}`}
                    disabled={isReadOnly}
                    onClick={() => onRemoveFile(index, attachment.blockIndex)}
                  >
                    <Trash2 className="size-3.5" aria-hidden />
                  </Button>
                </span>
              </div>
            ))}
          </div>
        ) : null}
        {uploadError ? <div className="workbench-message-upload-error">{uploadError}</div> : null}
      </section>
      {urlDialogKind ? (
        <AttachmentUrlDialog
          kind={urlDialogKind}
          url={attachmentUrl}
          setUrl={setAttachmentUrl}
          onSubmit={addUrlAttachment}
          onClose={closeUrlDialog}
        />
      ) : null}
    </>
  );
}

export function AttachmentTypeSubmenu({
  kind,
  label,
  onUpload,
  onAddUrl,
}: {
  kind: WorkbenchAttachmentKind;
  label: string;
  onUpload: (kind: WorkbenchAttachmentKind) => void;
  onAddUrl: (kind: WorkbenchAttachmentKind) => void;
}) {
  const TypeIcon = kind === 'image' ? ImageIcon : FileText;
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger className="min-h-9 gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium">
        <TypeIcon className="size-4" aria-hidden />
        <span>{label}</span>
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent aria-label={`${kind} attachment source`} className="min-w-[188px] rounded-xl p-1.5">
        <DropdownMenuItem
          className="min-h-9 gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium"
          onClick={() => onUpload(kind)}
        >
          <Upload className="size-4" aria-hidden />
          <span>Upload from device</span>
        </DropdownMenuItem>
        <DropdownMenuItem
          className="min-h-9 gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium"
          onClick={() => onAddUrl(kind)}
        >
          <Link2 className="size-4" aria-hidden />
          <span>Add from URL</span>
        </DropdownMenuItem>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}

export function AttachmentUrlDialog({
  kind,
  url,
  setUrl,
  onSubmit,
  onClose,
}: {
  kind: WorkbenchAttachmentKind;
  url: string;
  setUrl: Dispatch<SetStateAction<string>>;
  onSubmit: () => void;
  onClose: () => void;
}) {
  const isImage = kind === 'image';
  const title = isImage ? 'Add an image via URL' : 'Add a PDF via URL';
  const placeholder = isImage ? 'https://example.com/image.jpg' : 'https://example.com/document.pdf';
  return (
    <Dialog title={title} onClose={onClose} size="attachmentUrl">
      <form
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <Label className="grid gap-2 text-foreground">
          <span>URL</span>
          <Input
            type="text"
            value={url}
            placeholder={placeholder}
            className="h-9 bg-secondary"
            autoFocus
            onChange={(event) => setUrl(event.currentTarget.value)}
          />
        </Label>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={!url.trim()}>
            Add
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

export function MessageEditorPlaceholder({
  placeholder,
  showGeneratePrompt,
  warning,
  isReadOnly,
  isGenerating,
  onShowPromptGenerator,
}: {
  placeholder: string;
  showGeneratePrompt: boolean;
  warning: WorkbenchPromptGeneratorWarning | null;
  isReadOnly: boolean;
  isGenerating: boolean;
  onShowPromptGenerator?: () => void;
}) {
  const disabled = isReadOnly || isGenerating || Boolean(warning);
  return (
    <div className={clsx('workbench-message-placeholder', showGeneratePrompt && 'has-generator')}>
      {showGeneratePrompt ? (
        <span className="workbench-generate-prompt-tooltip" title={warning?.title}>
          <Button
            type="button"
            variant="ghost"
            className="workbench-generate-prompt-button"
            disabled={disabled}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => {
              if (!disabled) {
                onShowPromptGenerator?.();
              }
            }}
          >
            {isGenerating ? (
              <Loader2 className="size-4 animate-spin" aria-hidden />
            ) : (
              <Sparkles className="size-4" aria-hidden />
            )}
            <span>Generate Prompt</span>
          </Button>
        </span>
      ) : null}
      <span className="workbench-message-placeholder-copy" aria-hidden="true">
        {placeholder}
      </span>
    </div>
  );
}

export function messageRemovalLabel(messages: WorkbenchMessage[], index: number, label: string) {
  const [startIndex, endIndex] = messageRemovalRange(messages, index);
  if (endIndex > startIndex) {
    return 'Delete both to maintain user & assistant alternation';
  }
  return `Remove ${label.toLowerCase()} message`;
}

export function messageRemovalRange(messages: WorkbenchMessage[], index: number): [number, number] {
  const message = messages[index];
  if (!message) {
    return [index, index];
  }
  if (message.role === 'assistant' && messages[index + 1]?.role === 'human') {
    return [index, index + 1];
  }
  if (message.role === 'human' && messages[index - 1]?.role === 'assistant') {
    return [index - 1, index];
  }
  return [index, index];
}

export function plainTextToTiptapDoc(text: string): JSONContent {
  const lines = normalizeEditorText(text).split('\n');
  return {
    type: 'doc',
    content: lines.map((line) => ({
      type: 'paragraph',
      ...(line ? { content: [{ type: 'text', text: line }] } : {}),
    })),
  };
}

export function normalizeEditorText(text: unknown) {
  return String(text ?? '')
    .replace(/\r\n?/g, '\n')
    .replace(/\u00a0/g, ' ');
}

export function readTiptapEditorText(editor: Editor) {
  return normalizeEditorText(editor.state.doc.textBetween(0, editor.state.doc.content.size, '\n', '\n'));
}

export function setTiptapEditorText(editor: Editor, text: string, emitUpdate: boolean) {
  editor.commands.setContent(plainTextToTiptapDoc(text), { emitUpdate });
}

export function installTiptapTextValueShim(element: HTMLElement, editor: Editor) {
  Object.defineProperty(element, 'value', {
    configurable: true,
    get: () => readTiptapEditorText(editor),
    set: (value) => {
      setTiptapEditorText(editor, normalizeEditorText(value), false);
    },
  });
  Object.defineProperty(element, 'placeholder', {
    configurable: true,
    get: () => element.querySelector('[data-placeholder]')?.getAttribute('data-placeholder') ?? '',
  });
}

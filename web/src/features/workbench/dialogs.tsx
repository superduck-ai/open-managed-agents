import {
  BookOpen,
  Box,
  Code2,
  Copy,
  FileText,
  Loader2,
  Mail,
  MoreHorizontal,
  ShieldCheck,
  ShoppingBag,
  TriangleAlert,
  X,
} from 'lucide-react';
import { type ComponentProps, Dispatch, ReactNode, SetStateAction, useEffect, useMemo, useRef, useState } from 'react';
import clsx from 'clsx';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/shared/ui/alert-dialog';
import { Button } from '@/shared/ui/button';
import { Checkbox } from '@/shared/ui/checkbox';
import { Dialog as DialogRoot, DialogClose, DialogContent, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { useDialogDismissal } from '@/shared/hooks/use-dialog-dismissal';
import { useI18n } from '@/shared/i18n';
import { Label } from '@/shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Sheet, SheetClose, SheetContent, SheetTitle } from '@/shared/ui/sheet';
import { Textarea } from '@/shared/ui/textarea';
import { hljs } from './highlight';
import { AuthAccount } from '../../shared/auth/api';
import { WorkbenchPromptDetail, WorkbenchRevision } from './api';
import {
  codeForRevision,
  CodeLanguage,
  codeLanguageOptions,
  DrawerName,
  extractVariablesFromText,
  formatShareCreatedDate,
  generatePromptExamples,
  GeneratePromptStep,
  isPromptCreator,
  promptCreatorLabel,
  promptGeneratorWarningLabel,
  promptGeneratorWarningTitle,
  WorkbenchPromptGeneratorWarning,
} from './model';

export function CodeModal({
  language,
  setLanguage,
  revision,
  onClose,
}: {
  language: CodeLanguage;
  setLanguage: Dispatch<SetStateAction<CodeLanguage>>;
  revision: WorkbenchRevision;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const code = useMemo(() => codeForRevision(language, revision), [language, revision]);
  const selectedLanguageLabel = codeLanguageOptions.find((option) => option.value === language)?.label ?? 'Python';
  const hlLanguage =
    language === 'curl'
      ? 'bash'
      : language === 'typescript' || language === 'bedrock-typescript' || language === 'vertex-typescript'
        ? 'typescript'
        : 'python';
  const highlighted = useMemo(() => hljs.highlight(code, { language: hlLanguage }).value, [code, hlLanguage]);
  const highlightedLines = useMemo(() => highlighted.split('\n'), [highlighted]);

  return (
    <Dialog title={msg('workbench.codeTitle', 'Code for Claude API')} onClose={onClose} size="code">
      <div className="workbench-code-modal">
        <div className="workbench-code-controls">
          <div className="min-w-0 flex-1">
            <Select<CodeLanguage>
              value={language}
              items={codeLanguageOptions.map((option) => ({ value: option.value, label: option.label }))}
              onValueChange={(nextLanguage) => {
                if (nextLanguage) {
                  setLanguage(nextLanguage);
                }
              }}
            >
              <SelectTrigger
                aria-label={msg('workbench.codeLanguage', 'Code language')}
                className="h-9 w-full px-3 font-normal"
              >
                <SelectValue>{selectedLanguageLabel}</SelectValue>
              </SelectTrigger>
              <SelectContent className="min-w-[240px]">
                {codeLanguageOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="lg"
            className="min-w-[106px]"
            onClick={() => window.open('https://docs.anthropic.com/en/api/messages', '_blank', 'noopener,noreferrer')}
          >
            <BookOpen className="size-4" aria-hidden />
            {msg('workbench.viewDocs', 'View Docs')}
          </Button>
        </div>
        <div className="workbench-code-frame">
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="workbench-code-copy-button absolute right-3.5 top-2.5 z-[2] size-8 border-border bg-background/70 hover:bg-background/90"
            aria-label={msg('workbench.copyCode', 'Copy code')}
            title={msg('workbench.copyCode', 'Copy code')}
            onClick={() => void navigator.clipboard?.writeText(code)}
          >
            <Copy className="size-4" aria-hidden />
          </Button>
          <pre className="workbench-code-pre subtle-scrollbar">
            <code className={`language-${hlLanguage} hljs`}>
              {highlightedLines.map((line, index) => (
                <span className="workbench-code-line" key={`${index}-${line}`}>
                  <span className="workbench-code-line-number" aria-hidden>
                    {index + 1}
                  </span>
                  <span className="workbench-code-line-content" dangerouslySetInnerHTML={{ __html: line || ' ' }} />
                </span>
              ))}
            </code>
          </pre>
        </div>
      </div>
    </Dialog>
  );
}

export function GeneratePromptDialog({
  open,
  onExited,
  step,
  task,
  setTask,
  output,
  setOutput,
  examplesExpanded,
  setExamplesExpanded,
  thinkingEnabled,
  setThinkingEnabled,
  warning,
  error,
  isGenerating,
  onGenerate,
  onStop,
  onEdit,
  onOpen,
  onSelectExample,
  onBuyCredits,
  onClose,
}: {
  open: boolean;
  onExited: () => void;
  step: GeneratePromptStep;
  task: string;
  setTask: Dispatch<SetStateAction<string>>;
  output: string;
  setOutput: Dispatch<SetStateAction<string>>;
  examplesExpanded: boolean;
  setExamplesExpanded: Dispatch<SetStateAction<boolean>>;
  thinkingEnabled: boolean;
  setThinkingEnabled: Dispatch<SetStateAction<boolean>>;
  warning: WorkbenchPromptGeneratorWarning | null;
  error: string | null;
  isGenerating: boolean;
  onGenerate: () => void;
  onStop: () => void;
  onEdit: () => void;
  onOpen: () => void;
  onSelectExample: (example: (typeof generatePromptExamples)[number]) => void;
  onBuyCredits: () => void;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const hasOutput = Boolean(output.trim());
  const canGenerate = Boolean(task.trim()) && !isGenerating && !warning;
  const visibleExamples = examplesExpanded ? generatePromptExamples : generatePromptExamples.slice(0, 3);
  const isOutputStep = step === 'output';
  const outputVariables = extractVariablesFromText(output);
  const outputTextareaRef = useRef<HTMLTextAreaElement | null>(null);
  const taskTextareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    if (!isOutputStep || !isGenerating || !outputTextareaRef.current) {
      return;
    }
    const textarea = outputTextareaRef.current;
    textarea.scrollTop = textarea.scrollHeight;
  }, [isGenerating, isOutputStep, output]);

  return (
    <DialogRoot
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onClose();
      }}
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) onExited();
      }}
    >
      <DialogSurface
        title={msg('workbench.generatorTitle', 'Generate a prompt')}
        initialFocus={isOutputStep ? outputTextareaRef : taskTextareaRef}
        size="generate"
      >
        <div className={clsx('workbench-generate-modal', isOutputStep ? 'is-output' : 'is-input')}>
          <form
            id="workbench-generate-form"
            className="workbench-generate-modal-main subtle-scrollbar"
            onSubmit={(event) => {
              event.preventDefault();
              if (isOutputStep) {
                if (hasOutput && !isGenerating) {
                  onOpen();
                }
                return;
              }
              if (canGenerate) {
                onGenerate();
              }
            }}
          >
            {warning ? (
              <div className="workbench-generate-credit-banner">
                <strong>{promptGeneratorWarningLabel(warning, msg)}</strong>
                {warning.title === promptGeneratorWarningTitle ? (
                  <Button type="button" variant="outline" onClick={onBuyCredits}>
                    {msg('workbench.buyCredits', 'Buy credits')}
                  </Button>
                ) : null}
              </div>
            ) : null}

            {isOutputStep ? (
              <div className="workbench-generate-output-view">
                <div className="workbench-generate-copy">
                  <h3>{msg('workbench.generatedPrompt', 'Your prompt')}</h3>
                </div>
                <div className={clsx('workbench-generate-error', !error && 'is-empty')} aria-hidden={!error}>
                  {error}
                </div>
                <div className={clsx('workbench-generate-output', isGenerating && 'is-streaming')}>
                  {output ? (
                    <Textarea
                      ref={outputTextareaRef}
                      aria-label={msg('workbench.generatedPrompt', 'Your prompt')}
                      rows={20}
                      value={output}
                      onChange={(event) => setOutput(event.currentTarget.value)}
                      className="workbench-generate-output-textarea subtle-scrollbar"
                    />
                  ) : (
                    <div className="workbench-generate-output-empty">
                      <Loader2 className="size-4 animate-spin" aria-hidden />
                      {msg('workbench.generatingPrompt', 'Generating prompt…')}
                    </div>
                  )}
                </div>
                <GeneratePromptVariables variables={outputVariables} />
              </div>
            ) : (
              <div className="workbench-generate-input-view">
                <div className="workbench-generate-copy">
                  <h3>{msg('workbench.generatorTitle', 'Generate a prompt')}</h3>
                </div>
                <Textarea
                  ref={taskTextareaRef}
                  id="workbench-generate-task"
                  aria-label={msg('workbench.describeTask', 'Describe your task...')}
                  placeholder={msg('workbench.describeTask', 'Describe your task...')}
                  value={task}
                  disabled={isGenerating}
                  onChange={(event) => setTask(event.currentTarget.value)}
                  className="min-h-[136px] resize-none"
                />
                <div
                  className="workbench-generate-examples"
                  aria-label={msg('workbench.generatorExamples', 'Prompt generator examples')}
                >
                  {visibleExamples.map((example) => {
                    const ExampleIcon = promptExampleIcons[example.id];
                    return (
                      <Button
                        key={example.id}
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="workbench-generate-example"
                        disabled={isGenerating}
                        onClick={() => onSelectExample(example)}
                      >
                        <ExampleIcon className="size-4" aria-hidden />
                        <span>{msg(`workbench.generatorExample.${example.id}`, example.title)}</span>
                      </Button>
                    );
                  })}
                  {!examplesExpanded ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      className="workbench-generate-example is-more"
                      aria-label={msg('workbench.morePromptExamples', 'Show more prompt examples')}
                      disabled={isGenerating}
                      onClick={() => setExamplesExpanded(true)}
                    >
                      <MoreHorizontal className="size-4" aria-hidden />
                    </Button>
                  ) : null}
                </div>
                <Label className="items-start gap-2.5 text-muted-foreground">
                  <Checkbox
                    checked={thinkingEnabled}
                    disabled={isGenerating}
                    onCheckedChange={(nextChecked) => setThinkingEnabled(nextChecked === true)}
                  />
                  <span>
                    {msg('workbench.targetThinking', 'This prompt will be used with models that have thinking enabled')}
                  </span>
                </Label>
                {error ? <div className="workbench-generate-error">{error}</div> : null}
              </div>
            )}
          </form>

          <div className="workbench-generate-footer">
            {isOutputStep ? (
              <>
                <Button type="button" variant="outline" onClick={isGenerating ? onStop : onEdit}>
                  {isGenerating ? msg('common.cancel', 'Cancel') : msg('workbench.back', 'Back')}
                </Button>
                <Button type="submit" form="workbench-generate-form" disabled={!hasOutput || isGenerating}>
                  {msg('common.continue', 'Continue')}
                </Button>
              </>
            ) : (
              <>
                <DialogClose render={<Button type="button" variant="outline" />}>
                  {msg('common.cancel', 'Cancel')}
                </DialogClose>
                <Button type="submit" form="workbench-generate-form" disabled={!canGenerate}>
                  {msg('workbench.generate', 'Generate')}
                </Button>
              </>
            )}
          </div>
        </div>
      </DialogSurface>
    </DialogRoot>
  );
}

export function GeneratePromptVariables({ variables }: { variables: string[] }) {
  const { msg } = useI18n();
  if (!variables.length) return null;
  return (
    <div className="workbench-generate-variables">
      <h4>{msg('workbench.variables', 'Variables')}</h4>
      <div
        className="workbench-generate-variable-list"
        aria-label={msg('workbench.generatedVariables', 'Generated prompt variables')}
      >
        {variables.map((name) => (
          <span key={name}>{`{{${name}}}`}</span>
        ))}
      </div>
    </div>
  );
}

export function PromptGeneratorConfirmDialog({
  kind,
  onCancel,
  onConfirm,
}: {
  kind: 'close' | 'edit' | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const [content, setContent] = useState(kind);
  if (kind && kind !== content) setContent(kind);
  const copy =
    content === 'close'
      ? {
          title: msg('workbench.closeGeneratorTitle', 'Close prompt generator?'),
          description: msg(
            'workbench.closeGeneratorDescription',
            'By closing this modal, you will lose any progress made.',
          ),
          actionLabel: msg('common.close', 'Close'),
        }
      : {
          title: msg('workbench.clearGeneratedTitle', 'Clear generated prompt?'),
          description: msg(
            'workbench.clearGeneratedDescription',
            'Editing this page will clear the existing generated/converted prompt.',
          ),
          actionLabel: msg('workbench.clearPrompt', 'Clear prompt'),
        };

  return (
    <AlertDialog
      open={kind !== null}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onCancel();
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle>{copy.title}</AlertDialogTitle>
          <AlertDialogDescription>{copy.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onConfirm}>
            {copy.actionLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function ImprovePromptDialog({
  feedback,
  setFeedback,
  thinkingEnabled,
  setThinkingEnabled,
  showImageWarning,
  showMultiTurnWarning,
  isImproving,
  onImprove,
  onClose,
}: {
  feedback: string;
  setFeedback: Dispatch<SetStateAction<string>>;
  thinkingEnabled: boolean;
  setThinkingEnabled: Dispatch<SetStateAction<boolean>>;
  showImageWarning: boolean;
  showMultiTurnWarning: boolean;
  isImproving: boolean;
  onImprove: () => void;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const feedbackInputRef = useRef<HTMLTextAreaElement>(null);
  return (
    <Dialog
      title={msg('workbench.improveTitle', 'What would you like to improve?')}
      initialFocus={feedbackInputRef}
      onClose={onClose}
      closeDisabled={isImproving}
      size="improve"
    >
      <div className="workbench-improve-content">
        <h2>{msg('workbench.improveTitle', 'What would you like to improve?')}</h2>
        <p>{msg('workbench.improveCost', 'Uses the selected model and may incur usage charges.')}</p>
        {showImageWarning || showMultiTurnWarning ? (
          <div className="workbench-improve-warnings">
            {showImageWarning ? (
              <div className="workbench-improve-warning">
                <TriangleAlert className="size-4" aria-hidden />
                <span>
                  {msg(
                    'workbench.improveImages',
                    'Images will not be processed. Please manually add them back in to improved prompt.',
                  )}
                </span>
              </div>
            ) : null}
            {showMultiTurnWarning ? (
              <div className="workbench-improve-warning">
                <TriangleAlert className="size-4" aria-hidden />
                <span>
                  {msg(
                    'workbench.improveMultiTurn',
                    'Multi-turn prompt detected. Only the first user message will be improved.',
                  )}
                </span>
              </div>
            ) : null}
          </div>
        ) : null}
        <Textarea
          ref={feedbackInputRef}
          aria-label={msg(
            'workbench.improveFeedback',
            'The more detailed the feedback, the more Claude will be able to help.',
          )}
          placeholder={msg(
            'workbench.improveFeedback',
            'The more detailed the feedback, the more Claude will be able to help.',
          )}
          value={feedback}
          disabled={isImproving}
          onChange={(event) => setFeedback(event.currentTarget.value)}
          className="min-h-[146px] resize-y"
        />
        <Label className="items-start gap-2.5 text-muted-foreground">
          <Checkbox
            disabled={isImproving}
            checked={thinkingEnabled}
            onCheckedChange={(nextChecked) => setThinkingEnabled(nextChecked === true)}
          />
          <span>
            {msg('workbench.targetThinking', 'This prompt will be used with models that have thinking enabled')}
          </span>
        </Label>
        <div className="flex justify-center gap-2 pt-2">
          <DialogClose
            disabled={isImproving}
            render={<Button type="button" variant="outline" disabled={isImproving} />}
          >
            {msg('common.cancel', 'Cancel')}
          </DialogClose>
          <Button type="button" disabled={isImproving} onClick={onImprove}>
            {isImproving ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
            {msg('workbench.improvePrompt', 'Improve prompt')}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

export function SharePromptDialog({
  prompt,
  promptTitle,
  account,
  isSharing,
  error,
  onShare,
  onClose,
}: {
  prompt: WorkbenchPromptDetail;
  promptTitle: string;
  account: AuthAccount | null;
  isSharing: boolean;
  error: string | null;
  onShare: () => void;
  onClose: () => void;
}) {
  const { locale, msg } = useI18n();
  const creator = promptCreatorLabel(prompt, msg('workbench.you', 'you'));
  const createdAt = formatShareCreatedDate(prompt.created_at, locale);
  const shared = Boolean(prompt.is_shared_with_workspace);
  const isCreator = isPromptCreator(prompt, account);
  return (
    <Dialog
      title={msg('workbench.shareTitle', 'Share Prompt')}
      onClose={onClose}
      closeDisabled={isSharing}
      size="share"
    >
      <div className={clsx('workbench-share-content', !shared && !isCreator && 'is-readonly')}>
        <section className="workbench-share-summary" aria-label={msg('workbench.promptSummary', 'Prompt summary')}>
          <div className="workbench-share-title">{promptTitle}</div>
          <div className="workbench-share-meta">
            <Box className="size-3.5 text-primary" aria-hidden />
            <span>
              {createdAt
                ? msg('workbench.createdAtBy', '{date} by {creator}', { date: createdAt, creator })
                : msg('workbench.createdBy', 'by {creator}', { creator })}
            </span>
          </div>
        </section>

        <section className="workbench-share-access">
          <h3>{msg('workbench.shareAccess', 'Access')}</h3>
          {shared ? (
            <div className="workbench-share-access-row is-shared">
              <div>
                <strong>{msg('workbench.sharedWithWorkspace', 'Shared with workspace')}</strong>
                <p>
                  {msg('workbench.workspacePromptAccess', 'Anyone in this workspace can find and use this prompt.')}
                </p>
              </div>
            </div>
          ) : isCreator ? (
            <>
              <p>
                {msg(
                  'workbench.privatePromptAccess',
                  'Only you can access this prompt until it is shared with the workspace.',
                )}
              </p>
              <Button type="button" className="w-fit" disabled={isSharing} onClick={onShare}>
                {isSharing ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                {msg('workbench.shareWithWorkspace', 'Share with workspace')}
              </Button>
            </>
          ) : (
            <p>
              {msg(
                'workbench.askCreatorToShare',
                'Please ask the creator of this prompt, {creator}, to modify its sharing settings.',
                { creator },
              )}
            </p>
          )}
          {isCreator && error ? <div className="workbench-share-error">{error}</div> : null}
        </section>
      </div>
    </Dialog>
  );
}

export function WorkbenchDrawer({
  open,
  title,
  kind,
  children,
  headerAction,
  onClose,
  onClosed,
}: {
  open: boolean;
  title: string;
  kind: DrawerName;
  children: ReactNode;
  headerAction?: ReactNode;
  onClose: (reason?: 'escape') => void;
  onClosed: () => void;
}) {
  const { msg } = useI18n();
  const drawerLabel = msg(`workbench.drawer.${kind}`, title);
  return (
    <Sheet
      open={open}
      modal={false}
      disablePointerDismissal
      onOpenChange={(nextOpen, details) => {
        if (details.reason === 'focus-out') {
          details.cancel();
          return;
        }
        if (!nextOpen) onClose(details.reason === 'escape-key' ? 'escape' : undefined);
      }}
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) onClosed();
      }}
    >
      <SheetContent
        className={clsx('workbench-drawer-panel h-auto max-w-none gap-0 sm:max-w-none', `is-${kind}`)}
        aria-label={drawerLabel}
        showOverlay={false}
        showCloseButton={false}
        initialFocus={false}
      >
        <div className="workbench-drawer-header">
          <SheetTitle>{drawerLabel}</SheetTitle>
          <div className="workbench-drawer-header-actions">
            {headerAction}
            <SheetClose
              aria-label={msg('common.close', 'Close')}
              title={msg('common.close', 'Close')}
              render={
                <Button
                  type="button"
                  variant="ghost"
                  className="size-9 rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
                />
              }
            >
              <X className="size-4" aria-hidden />
            </SheetClose>
          </div>
        </div>
        <div
          className={clsx(
            'workbench-drawer-body subtle-scrollbar',
            kind === 'model' && 'is-model',
            kind === 'variables' && 'is-variables',
            kind === 'tools' && 'is-tools',
            kind === 'examples' && 'is-examples',
            kind === 'history' && 'is-history',
          )}
        >
          {children}
        </div>
      </SheetContent>
    </Sheet>
  );
}

export function DeletePromptDialog({
  isDeleting,
  onCancel,
  onDelete,
}: {
  isDeleting: boolean;
  onCancel: () => void;
  onDelete: () => void;
}) {
  const { msg } = useI18n();
  const dialogProps = useDialogDismissal(onCancel, isDeleting);
  return (
    <AlertDialog {...dialogProps}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{msg('workbench.deletePrompt', 'Delete prompt')}</AlertDialogTitle>
          <AlertDialogDescription>
            {msg('workbench.deletePromptConfirm', 'Are you sure you want to permanently delete this prompt?')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isDeleting}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={isDeleting} onClick={onDelete}>
            {isDeleting ? msg('common.deleting', 'Deleting') : msg('workbench.delete', 'Delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

type DialogSurfaceProps = {
  title: string;
  children: ReactNode;
  closeDisabled?: boolean;
  initialFocus?: ComponentProps<typeof DialogContent>['initialFocus'];
  size?: 'normal' | 'wide' | 'code' | 'improve' | 'generate' | 'share' | 'rename' | 'attachmentUrl';
};

export function Dialog({
  onClose,
  closeDisabled = false,
  ...surfaceProps
}: DialogSurfaceProps & { onClose: () => void }) {
  const dialogProps = useDialogDismissal(onClose, closeDisabled);
  return (
    <DialogRoot {...dialogProps}>
      <DialogSurface {...surfaceProps} closeDisabled={closeDisabled} />
    </DialogRoot>
  );
}

function DialogSurface({ title, children, closeDisabled = false, initialFocus, size = 'normal' }: DialogSurfaceProps) {
  const { msg } = useI18n();
  const styledClose = ['code', 'rename', 'share', 'generate', 'attachmentUrl'].includes(size);
  const closeLabel = msg('common.close', 'Close');
  return (
    <DialogContent
      initialFocus={initialFocus}
      aria-label={title}
      showCloseButton={false}
      className={clsx(
        'max-h-[90vh] gap-0 overflow-hidden rounded-lg border-border bg-popover p-0 shadow-[var(--shadow-popover)] sm:max-w-none',
        size === 'code'
          ? 'workbench-code-dialog'
          : size === 'improve'
            ? 'workbench-improve-dialog'
            : size === 'generate'
              ? 'workbench-generate-dialog'
              : size === 'share'
                ? 'workbench-share-dialog'
                : size === 'rename'
                  ? 'workbench-rename-dialog'
                  : size === 'attachmentUrl'
                    ? 'workbench-attachment-url-dialog'
                    : size === 'wide'
                      ? 'w-full max-w-[780px]'
                      : 'w-full max-w-[420px]',
      )}
    >
      <DialogHeader
        className={clsx(
          'flex h-14 flex-row items-center justify-between border-b-[0.5px] border-border px-4',
          size === 'code' && 'workbench-code-dialog-header',
          size === 'improve' && 'workbench-improve-dialog-header',
          size === 'generate' && 'workbench-generate-dialog-header',
          size === 'share' && 'workbench-share-dialog-header',
          size === 'rename' && 'workbench-rename-dialog-header',
          size === 'attachmentUrl' && 'workbench-attachment-url-dialog-header',
        )}
      >
        <DialogTitle
          className={clsx(
            'text-sm font-semibold text-foreground',
            size === 'code' && 'workbench-code-dialog-title',
            size === 'generate' && 'sr-only',
            size === 'share' && 'workbench-share-dialog-title',
            size === 'rename' && 'workbench-rename-dialog-title',
            size === 'attachmentUrl' && 'workbench-attachment-url-dialog-title',
            size === 'improve' && 'sr-only',
          )}
        >
          {title}
        </DialogTitle>
        <DialogClose
          disabled={closeDisabled}
          render={
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={closeLabel}
              title={closeLabel}
              disabled={closeDisabled}
              className={clsx(
                styledClose
                  ? 'workbench-code-dialog-close'
                  : 'grid size-9 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground',
                size === 'rename' && 'workbench-rename-dialog-close',
                size === 'share' && 'workbench-share-dialog-close',
                size === 'generate' && 'workbench-generate-dialog-close',
                size === 'attachmentUrl' && 'workbench-attachment-url-dialog-close',
              )}
            />
          }
        >
          <X className="size-4" aria-hidden />
        </DialogClose>
      </DialogHeader>
      <div
        className={clsx(
          'subtle-scrollbar max-h-[calc(90vh-56px)] overflow-y-auto p-4',
          size === 'code' && 'workbench-code-dialog-body',
          size === 'improve' && 'workbench-improve-dialog-body',
          size === 'generate' && 'workbench-generate-dialog-body',
          size === 'share' && 'workbench-share-dialog-body',
          size === 'rename' && 'workbench-rename-dialog-body',
          size === 'attachmentUrl' && 'workbench-attachment-url-dialog-body',
        )}
      >
        {children}
      </div>
    </DialogContent>
  );
}

export const promptExampleIcons: Record<(typeof generatePromptExamples)[number]['id'], typeof FileText> = {
  summarize: FileText,
  email: Mail,
  'translate-code': Code2,
  'content-moderation': ShieldCheck,
  'recommend-product': ShoppingBag,
};

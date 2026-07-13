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
import { Dispatch, ReactNode, SetStateAction, useEffect, useMemo, useRef } from 'react';
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
import { Dialog as DialogRoot, DialogContent, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { Label } from '@/shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
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
  WorkbenchPromptGeneratorWarning,
} from './model';
import { IconButton } from './components';

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
    <Dialog title="Code for Claude API" onClose={onClose} size="code">
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
                aria-label="Code language"
                className="h-10 w-full px-3 text-[15px] font-normal text-foreground"
              >
                <SelectValue>{selectedLanguageLabel}</SelectValue>
              </SelectTrigger>
              <SelectContent sideOffset={6} align="start" alignItemWithTrigger={false} className="min-w-[240px]">
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
            className="h-8 min-w-[106px] text-foreground hover:bg-accent"
            onClick={() => window.open('https://docs.anthropic.com/en/api/messages', '_blank', 'noopener,noreferrer')}
          >
            <BookOpen className="size-4" aria-hidden />
            View Docs
          </Button>
        </div>
        <div className="workbench-code-frame group/code-frame">
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="absolute right-3.5 top-2.5 z-[2] size-8 border-border bg-background/70 opacity-0 transition-opacity hover:bg-background/90 focus-visible:opacity-100 group-hover/code-frame:opacity-100"
            aria-label="Copy code"
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
  const hasOutput = Boolean(output.trim());
  const canGenerate = Boolean(task.trim()) && !isGenerating && !warning;
  const visibleExamples = examplesExpanded ? generatePromptExamples : generatePromptExamples.slice(0, 3);
  const isOutputStep = step === 'output';
  const outputVariables = extractVariablesFromText(output);
  const outputTextareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    if (!isOutputStep || !isGenerating || !outputTextareaRef.current) {
      return;
    }
    const textarea = outputTextareaRef.current;
    textarea.scrollTop = textarea.scrollHeight;
  }, [isGenerating, isOutputStep, output]);

  return (
    <Dialog title="Generate a prompt" onClose={onClose} size="generate">
      <div className={clsx('workbench-generate-modal', isOutputStep ? 'is-output' : 'is-input')}>
        <form
          id="workbench-generate-form"
          className="workbench-generate-modal-main"
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
              <div>
                <strong>Buy credits to use the prompt generator</strong>
                <span>{warning.title}</span>
              </div>
              <Button type="button" variant="outline" onClick={onBuyCredits}>
                Buy credits
              </Button>
            </div>
          ) : null}

          {isOutputStep ? (
            <div className="workbench-generate-output-view">
              <div className="workbench-generate-copy">
                <h3>Your prompt</h3>
                <p>You'll be able to make further changes and improvements later too.</p>
              </div>
              <div className={clsx('workbench-generate-error', !error && 'is-empty')} aria-hidden={!error}>
                {error}
              </div>
              <div className={clsx('workbench-generate-output', isGenerating && 'is-streaming')}>
                {output ? (
                  <Textarea
                    ref={outputTextareaRef}
                    aria-label="Your prompt"
                    rows={20}
                    value={output}
                    onChange={(event) => setOutput(event.currentTarget.value)}
                    className="workbench-generate-output-textarea subtle-scrollbar"
                  />
                ) : (
                  <div className="workbench-generate-output-empty">
                    <Loader2 className="size-4 animate-spin" aria-hidden />
                    Generating prompt…
                  </div>
                )}
              </div>
              <GeneratePromptVariables variables={outputVariables} />
            </div>
          ) : (
            <div className="workbench-generate-input-view">
              <div className="workbench-generate-copy">
                <h3>Generate a prompt</h3>
                <p>You can generate a prompt template by sharing basic details about your task.</p>
              </div>
              <Textarea
                id="workbench-generate-task"
                aria-label="Describe your task..."
                placeholder="Describe your task..."
                value={task}
                disabled={isGenerating}
                onChange={(event) => setTask(event.currentTarget.value)}
                className="min-h-[136px] resize-none"
                autoFocus
              />
              <div className="workbench-generate-examples" aria-label="Prompt generator examples">
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
                      <span>{example.title}</span>
                    </Button>
                  );
                })}
                {!examplesExpanded ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="workbench-generate-example is-more"
                    aria-label="Show more prompt examples"
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
                <span>This prompt will be used with models that have thinking enabled</span>
              </Label>
              {error ? <div className="workbench-generate-error">{error}</div> : null}
            </div>
          )}
        </form>

        <div className="workbench-generate-footer">
          {isOutputStep ? (
            <>
              <Button type="button" variant="outline" onClick={isGenerating ? onStop : onEdit}>
                {isGenerating ? 'Cancel' : 'Back'}
              </Button>
              <Button type="submit" form="workbench-generate-form" disabled={!hasOutput || isGenerating}>
                Continue
              </Button>
            </>
          ) : (
            <>
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" form="workbench-generate-form" disabled={!canGenerate}>
                Generate
              </Button>
            </>
          )}
        </div>
      </div>
    </Dialog>
  );
}

export function GeneratePromptVariables({ variables }: { variables: string[] }) {
  return (
    <div className="workbench-generate-variables">
      <h4>Variables</h4>
      <p>
        Variables are placeholder values that make your prompt flexible and reusable. Variables in Workbench are
        enclosed in double brackets like so: <code>{'{{VARIABLE_NAME}}'}</code>. The prompt above has the following
        variables:
      </p>
      {variables.length ? (
        <div className="workbench-generate-variable-list" aria-label="Generated prompt variables">
          {variables.map((name) => (
            <span key={name}>{`{{${name}}}`}</span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function PromptGeneratorConfirmDialog({
  kind,
  onCancel,
  onConfirm,
}: {
  kind: 'close' | 'edit';
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const copy =
    kind === 'close'
      ? {
          title: 'Close prompt generator?',
          description: 'By closing this modal, you will lose any progress made.',
          actionLabel: 'Close',
        }
      : {
          title: 'Clear generated prompt?',
          description: 'Editing this page will clear the existing generated/converted prompt.',
          actionLabel: 'Clear prompt',
        };

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open) {
          onCancel();
        }
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle>{copy.title}</AlertDialogTitle>
          <AlertDialogDescription>{copy.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
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
  return (
    <Dialog title="What would you like to improve?" onClose={onClose} size="improve">
      <div className="workbench-improve-content">
        <h2>What would you like to improve?</h2>
        <p>This takes 1-2 minutes and uses Claude Sonnet 4.5 credits</p>
        {showImageWarning || showMultiTurnWarning ? (
          <div className="workbench-improve-warnings">
            {showImageWarning ? (
              <div className="workbench-improve-warning">
                <TriangleAlert className="size-4" aria-hidden />
                <span>Images will not be processed. Please manually add them back in to improved prompt.</span>
              </div>
            ) : null}
            {showMultiTurnWarning ? (
              <div className="workbench-improve-warning">
                <TriangleAlert className="size-4" aria-hidden />
                <span>Multi-turn prompt detected. Only the first user message will be improved.</span>
              </div>
            ) : null}
          </div>
        ) : null}
        <Textarea
          aria-label="The more detailed the feedback, the more Claude will be able to help."
          placeholder="The more detailed the feedback, the more Claude will be able to help."
          value={feedback}
          onChange={(event) => setFeedback(event.currentTarget.value)}
          className="min-h-[146px] resize-y"
        />
        <Label className="items-start gap-2.5 text-muted-foreground">
          <Checkbox
            checked={thinkingEnabled}
            onCheckedChange={(nextChecked) => setThinkingEnabled(nextChecked === true)}
          />
          <span>This prompt will be used with models that have thinking enabled</span>
        </Label>
        <div className="flex justify-center gap-2 pt-2">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="button" disabled={isImproving} onClick={onImprove}>
            {isImproving ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
            Improve prompt
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
  const creator = promptCreatorLabel(prompt);
  const createdAt = formatShareCreatedDate(prompt.created_at);
  const shared = Boolean(prompt.is_shared_with_workspace);
  const isCreator = isPromptCreator(prompt, account);
  return (
    <Dialog title="Share Prompt" onClose={onClose} size="share">
      <div className={clsx('workbench-share-content', !shared && !isCreator && 'is-readonly')}>
        <section className="workbench-share-summary" aria-label="Prompt summary">
          <div className="workbench-share-title">{promptTitle}</div>
          <div className="workbench-share-meta">
            <Box className="size-3.5 text-primary" aria-hidden />
            <span>{createdAt ? `${createdAt} by ${creator}` : `by ${creator}`}</span>
          </div>
        </section>

        <section className="workbench-share-access">
          <h3>Access</h3>
          {shared ? (
            <div className="workbench-share-access-row is-shared">
              <div>
                <strong>Shared with workspace</strong>
                <p>Anyone in this workspace can find and use this prompt.</p>
              </div>
            </div>
          ) : isCreator ? (
            <>
              <p>Only you can access this prompt until it is shared with the workspace.</p>
              <Button type="button" className="w-fit" disabled={isSharing} onClick={onShare}>
                {isSharing ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                Share with workspace
              </Button>
            </>
          ) : (
            <p>Please ask the creator of this prompt, {creator}, to modify its sharing settings.</p>
          )}
          {isCreator && error ? <div className="workbench-share-error">{error}</div> : null}
        </section>
      </div>
    </Dialog>
  );
}

export function WorkbenchDrawer({
  title,
  kind,
  children,
  headerAction,
  onClose,
}: {
  title: string;
  kind: DrawerName;
  children: ReactNode;
  headerAction?: ReactNode;
  onClose: () => void;
}) {
  return (
    <aside className={clsx('workbench-drawer-panel', `is-${kind}`)} aria-label={title}>
      <div className="workbench-drawer-header">
        <h2>{title}</h2>
        <div className="workbench-drawer-header-actions">
          {headerAction}
          <IconButton label="Close" onClick={onClose}>
            <X className="size-4" aria-hidden />
          </IconButton>
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
    </aside>
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
  return (
    <AlertDialog
      open
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !isDeleting) {
          onCancel();
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete prompt</AlertDialogTitle>
          <AlertDialogDescription>Are you sure you want to permanently delete this prompt?</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isDeleting}>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={isDeleting} onClick={onDelete}>
            {isDeleting ? 'Deleting' : 'Delete'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function Dialog({
  title,
  children,
  onClose,
  size = 'normal',
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  size?: 'normal' | 'wide' | 'code' | 'improve' | 'generate' | 'share' | 'rename' | 'attachmentUrl';
}) {
  return (
    <DialogRoot
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent
        aria-label={title}
        showCloseButton={false}
        className={clsx(
          'max-h-[90vh] gap-0 overflow-hidden rounded-lg border-border bg-popover p-0 shadow-xl sm:max-w-none',
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
            'flex h-14 flex-row items-center justify-between border-b border-border px-4',
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
          {size === 'code' ||
          size === 'rename' ||
          size === 'share' ||
          size === 'generate' ||
          size === 'attachmentUrl' ? (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label="Close"
              title="Close"
              className={clsx(
                'workbench-code-dialog-close',
                size === 'rename' && 'workbench-rename-dialog-close',
                size === 'share' && 'workbench-share-dialog-close',
                size === 'generate' && 'workbench-generate-dialog-close',
                size === 'attachmentUrl' && 'workbench-attachment-url-dialog-close',
              )}
              onClick={onClose}
            >
              <X className="size-4" aria-hidden />
            </Button>
          ) : (
            <IconButton label={size === 'improve' ? 'Close' : 'Close dialog'} onClick={onClose}>
              <X className="size-4" aria-hidden />
            </IconButton>
          )}
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
    </DialogRoot>
  );
}

export const promptExampleIcons: Record<(typeof generatePromptExamples)[number]['id'], typeof FileText> = {
  summarize: FileText,
  email: Mail,
  'translate-code': Code2,
  'content-moderation': ShieldCheck,
  'recommend-product': ShoppingBag,
};

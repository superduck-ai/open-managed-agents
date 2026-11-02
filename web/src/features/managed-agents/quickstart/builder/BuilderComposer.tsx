import { useBuilderExamples } from './useBuilderExamples';
import './builder-composer.css';
import { usePromptOptimization } from './usePromptOptimization';
import { useState, type KeyboardEvent } from 'react';
import { ArrowUp, Square, Sparkles, LoaderCircle, Undo2, type LucideIcon } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { Textarea } from '../../../../shared/ui/textarea';
import type { Builder } from './useBuilder';

export function BuilderComposer({
  builder,
  entry = false,
  onSend,
}: {
  builder: Builder;
  entry?: boolean;
  onSend?: (text: string) => void;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  const [draft, setDraft] = useState('');
  const optimization = usePromptOptimization(builder.optimizePrompt, draft, setDraft);
  const blocked = builder.busy || Boolean(state.operation) || builder.draftEditing;
  const { example, onFocus, onBlur } = useBuilderExamples(entry && draft === '', zh);
  const suggestion = entry && draft === '' ? example : '';
  const send = () => {
    const text = draft.trim() || suggestion;
    if (!text || blocked || optimization.busy) return;
    optimization.cancel();
    if (onSend) {
      onSend(text);
      return;
    }
    setDraft('');
    void builder.reply(text);
  };
  return (
    <form
      className={`mx-auto flex w-full gap-2 border-[0.5px] border-input bg-card p-3 focus-within:border-foreground ${entry ? 'builder-entry-composer h-full flex-col rounded-2xl' : 'max-w-[760px] items-end rounded-2xl'}`}
      onSubmit={(event) => {
        event.preventDefault();
        send();
      }}
    >
      <Textarea
        value={draft}
        onChange={(event) => optimization.edit(event.target.value)}
        placeholder={composerPlaceholder(entry, example, zh)}
        onFocus={onFocus}
        onBlur={onBlur}
        aria-label={zh ? '构建消息' : 'Builder message'}
        className="max-h-48 min-h-16 resize-none border-0 bg-transparent px-1 pt-1 pb-3 text-sm leading-6 shadow-none focus-visible:ring-0 dark:bg-transparent"
        disabled={Boolean(state.operation) || builder.draftEditing}
        onKeyDown={(event) => handleComposerKey(event, suggestion, optimization.edit, send)}
      />
      <div className={entry ? 'flex w-full shrink-0 items-center justify-between text-muted-foreground' : 'contents'}>
        {entry && <OptimizationActions optimization={optimization} disabled={blocked || !draft.trim()} zh={zh} />}
        {builder.busy ? (
          <Button
            type="button"
            size="icon"
            aria-label={zh ? '停止' : 'Stop'}
            variant="outline"
            onClick={builder.cancel}
          >
            <Square />
          </Button>
        ) : (
          <Button
            type="submit"
            size="icon"
            aria-label={zh ? '发送' : 'Send'}
            disabled={optimization.busy || !(draft.trim() || suggestion) || blocked}
          >
            <ArrowUp />
          </Button>
        )}
      </div>
      {entry && <OptimizationFeedback optimization={optimization} zh={zh} />}
    </form>
  );
}

type OptimizationProps = { optimization: ReturnType<typeof usePromptOptimization>; zh: boolean };
function OptimizationActions({ optimization, disabled, zh }: OptimizationProps & { disabled: boolean }) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      {optimization.busy ? (
        <Button type="button" variant="ghost" size="sm" onClick={optimization.cancel}>
          <LoaderCircle className="animate-spin" />
          {zh ? '取消优化' : 'Cancel optimization'}
        </Button>
      ) : (
        <OptimizationAction
          label={zh ? '优化描述' : 'Improve description'}
          Icon={Sparkles}
          onClick={() => void optimization.start()}
          disabled={disabled}
        />
      )}
      {optimization.original !== null && !optimization.busy && (
        <OptimizationAction label={zh ? '撤销优化' : 'Undo improvement'} Icon={Undo2} onClick={optimization.undo} />
      )}
    </div>
  );
}
function OptimizationFeedback({ optimization, zh }: OptimizationProps) {
  if (!optimization.error) return null;
  return (
    <p role="alert" className="max-h-12 shrink-0 overflow-y-auto text-sm text-destructive break-words">
      {zh ? '优化失败：' : 'Optimization failed: '}
      {optimization.error}
    </p>
  );
}

function OptimizationAction({
  label,
  Icon,
  onClick,
  disabled,
}: {
  label: string;
  Icon: LucideIcon;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <span className="builder-optimize-action">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="gap-0 px-2"
        aria-label={label}
        onClick={onClick}
        disabled={disabled}
      >
        <Icon />
        <span aria-hidden="true" className="builder-optimize-label">
          <span>
            <span className="pl-2">{label}</span>
          </span>
        </span>
      </Button>
    </span>
  );
}

function handleComposerKey(
  event: KeyboardEvent<HTMLTextAreaElement>,
  suggestion: string,
  accept: (text: string) => void,
  send: () => void,
) {
  if (event.nativeEvent.isComposing || event.shiftKey || event.ctrlKey || event.altKey || event.metaKey) return;
  if (event.key === 'Tab' && suggestion) {
    event.preventDefault();
    accept(suggestion);
  }
  if (event.key === 'Enter') {
    event.preventDefault();
    send();
  }
}

function composerPlaceholder(entry: boolean, example: string, zh: boolean) {
  if (entry) return example;
  return zh ? '回复或继续调整…' : 'Reply or keep refining…';
}

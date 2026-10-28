import { useState } from 'react';
import { ArrowUp, Square, Sparkles } from 'lucide-react';
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
  const send = () => {
    const text = draft.trim();
    if (!text || state.gate?.name === 'ask_user_questions') return;
    if (onSend) {
      onSend(text);
      return;
    }
    setDraft('');
    void builder.reply(text);
  };
  return (
    <form
      className={`mx-auto flex w-full gap-2 border-[0.5px] border-input bg-card p-3 focus-within:border-foreground ${entry ? 'max-w-[728px] flex-col rounded-2xl' : 'max-w-[680px] items-end rounded-2xl'}`}
      onSubmit={(event) => {
        event.preventDefault();
        send();
      }}
    >
      <Textarea
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        placeholder={
          entry
            ? zh
              ? '一个客服助手，根据我们的文档回答问题，遇到无法解决的问题时交给人工…'
              : 'A customer support agent that answers from our docs and hands unresolved questions to a person…'
            : zh
              ? '描述你的需求…'
              : 'Describe what you need…'
        }
        aria-label={zh ? '构建消息' : 'Builder message'}
        className="max-h-48 min-h-16 resize-none border-0 bg-transparent px-1 pt-1 pb-3 text-sm leading-6 shadow-none focus-visible:ring-0 dark:bg-transparent"
        disabled={Boolean(state.operation)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            if (!builder.busy) send();
          }
        }}
      />
      <div className={entry ? 'flex w-full items-center justify-between text-muted-foreground' : 'contents'}>
        {entry && <Sparkles aria-hidden="true" className="ml-1 size-4" />}
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
            disabled={!draft.trim() || Boolean(state.operation) || state.gate?.name === 'ask_user_questions'}
          >
            <ArrowUp />
          </Button>
        )}
      </div>
    </form>
  );
}

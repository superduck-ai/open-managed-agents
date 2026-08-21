import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { HighlightedCode, SyntaxCodeBlock } from '../../../shared/ui/syntax-code-block';
import {
  highlightBashYAMLCommand,
  highlightCodeHTML,
  highlightRegisteredLanguage,
  normalizeHighlightLanguage,
} from '../../../shared/ui/syntax-highlighting';
import clsx from 'clsx';
import { Check, Copy } from 'lucide-react';
import { useState } from 'react';
import { templateBody, templateTitle } from '../labels';
import { type AgentTemplate, type CodeFormat, type HighlightLanguage } from '../types';
import { copyText } from '../utils';

export {
  HighlightedCode,
  SyntaxCodeBlock,
  highlightBashYAMLCommand as highlightBashYamlCommand,
  highlightCodeHTML as highlightCodeHtml,
  highlightRegisteredLanguage,
  normalizeHighlightLanguage,
};

export function codeFormatLanguage(format: CodeFormat): HighlightLanguage {
  return format === 'YAML' ? 'yaml' : 'json';
}

export function FormatSelect({
  value,
  onChange,
  compact = false,
  align = 'right',
  buttonClassName,
  menuClassName,
}: {
  value: CodeFormat;
  onChange: (value: CodeFormat) => void;
  compact?: boolean;
  align?: 'left' | 'right';
  buttonClassName?: string;
  menuClassName?: string;
}) {
  const { msg } = useI18n();
  const items: Array<{ value: CodeFormat; label: CodeFormat }> = [
    { value: 'YAML', label: 'YAML' },
    { value: 'JSON', label: 'JSON' },
  ];

  return (
    <Select<CodeFormat>
      value={value}
      items={items}
      onValueChange={(nextValue) => {
        if (nextValue !== null) {
          onChange(nextValue);
        }
      }}
    >
      <SelectTrigger
        aria-label={msg('managedAgents.codeBlocks.codeFormat', 'Code format')}
        size="sm"
        className={clsx(
          'h-7 w-auto min-w-[4.5rem] border-transparent bg-transparent px-2 text-sm text-foreground shadow-none hover:bg-accent',
          compact ? 'rounded-md px-2' : 'px-2.5',
          buttonClassName,
        )}
      >
        <SelectValue>{value}</SelectValue>
      </SelectTrigger>
      <SelectContent
        align={align === 'left' ? 'start' : 'end'}
        alignItemWithTrigger={false}
        sideOffset={6}
        className={clsx('w-28 min-w-[7rem]', menuClassName)}
      >
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value} label={item.label}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function CopyButton({ value, label }: { value: string; label: string }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await copyText(value);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 900);
  };

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={copied ? msg('common.copied', 'Copied') : label}
      className="text-foreground hover:bg-accent hover:text-foreground"
      onClick={handleCopy}
    >
      {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
    </Button>
  );
}

export function NumberedCodeBlock({ code, format }: { code: string; format: CodeFormat }) {
  return (
    <pre className="min-w-full whitespace-pre-wrap break-words font-mono text-[13px] leading-[19px] text-foreground">
      <HighlightedCode code={code} language={codeFormatLanguage(format)} />
    </pre>
  );
}

export function MiniCodeBlock({ code, maxLines }: { code: string; maxLines: number }) {
  const maxHeight = Math.max(96, maxLines * 18 + 24);

  return (
    <pre
      className="subtle-scrollbar-auto overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-words px-3 py-3 font-mono text-[12px] leading-[18px] text-foreground"
      style={{ maxHeight }}
    >
      <HighlightedCode code={code} language="bash-yaml" />
    </pre>
  );
}

export function ScrollableCodeBlock({ code, language }: { code: string; language: HighlightLanguage }) {
  return (
    <pre className="subtle-scrollbar-auto max-h-80 overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-words px-3 py-3 font-mono text-[12px] leading-[18px] text-foreground">
      <HighlightedCode code={code} language={language} />
    </pre>
  );
}

const maxVisibleTemplateTags = 4;

export function TemplateCard({ template, onClick }: { template: AgentTemplate; onClick: () => void }) {
  const { msg } = useI18n();
  const title = templateTitle(template, msg);
  const body = templateBody(template, msg);
  const label = [title, body, ...(template.tags?.map((tag) => tag.label) ?? [])].join(' ');
  const tags = template.tags ?? [];
  const visibleTags = tags.slice(0, maxVisibleTemplateTags);
  const hiddenTagCount = tags.length - visibleTags.length;
  const hiddenTagTitle = `${hiddenTagCount} more ${hiddenTagCount === 1 ? 'tag' : 'tags'}`;
  return (
    <Button
      type="button"
      variant="ghost"
      aria-label={label}
      className="h-[136px] min-h-[136px] w-full self-start flex-col items-start justify-start gap-0 overflow-hidden whitespace-normal rounded-lg border border-border bg-card p-3 text-left shadow-xs transition-[border-color,background-color,box-shadow] hover:border-ring/40 hover:bg-accent/40 hover:shadow-sm"
      onClick={onClick}
    >
      <div className="line-clamp-2 w-full min-w-0 text-[15px] font-medium leading-5 text-foreground">{title}</div>
      <p className="mt-1 w-full min-w-0 line-clamp-2 text-[13px] leading-[18px] text-muted-foreground">{body}</p>
      {tags.length ? (
        <div className="mt-auto flex max-w-full flex-nowrap gap-1.5 overflow-hidden pt-3">
          {visibleTags.map((tag) => {
            const Icon = tag.icon;
            return (
              <span
                key={tag.label}
                className={clsx('grid size-5 place-items-center rounded-full border border-border', tag.tone)}
                title={tag.label}
              >
                <Icon className="size-3" aria-hidden />
              </span>
            );
          })}
          {hiddenTagCount > 0 ? (
            <span
              className="grid h-5 min-w-5 place-items-center rounded-full border border-border bg-secondary px-1.5 text-[10px] font-medium leading-none text-secondary-foreground"
              title={hiddenTagTitle}
            >
              +{hiddenTagCount}
            </span>
          ) : null}
        </div>
      ) : null}
    </Button>
  );
}

import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import clsx from 'clsx';
import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import python from 'highlight.js/lib/languages/python';
import typescript from 'highlight.js/lib/languages/typescript';
import yamlLanguage from 'highlight.js/lib/languages/yaml';
import { Check, Copy } from 'lucide-react';
import { useState } from 'react';
import { templateBody, templateTitle } from '../labels';
import { looksLikeJson } from '../sessions/SessionDetailPage';
import { type AgentTemplate, type CodeFormat, type HighlightLanguage } from '../types';
import { copyText } from '../utils';

hljs.registerLanguage('bash', bash);

hljs.registerLanguage('shell', bash);

hljs.registerLanguage('javascript', javascript);

hljs.registerLanguage('json', json);

hljs.registerLanguage('python', python);

hljs.registerLanguage('typescript', typescript);

hljs.registerLanguage('yaml', yamlLanguage);

export function SyntaxCodeBlock({
  value,
  language,
  maxHeightClassName
}: {
  value: string;
  language?: string;
  maxHeightClassName?: string;
}) {
  const highlightLanguage = normalizeHighlightLanguage(language, value);
  return (
    <pre
      data-testid="session-trace-code-block"
      className={clsx(
        'rounded-lg border border-border bg-muted p-3 font-mono text-[13px] leading-[19px] text-foreground whitespace-pre-wrap break-words overflow-x-hidden',
        maxHeightClassName ? 'subtle-scrollbar overflow-y-auto' : 'overflow-visible',
        maxHeightClassName
      )}
    >
      <HighlightedCode code={value} language={highlightLanguage} />
    </pre>
  );
}

export function codeFormatLanguage(format: CodeFormat): HighlightLanguage {
  return format === 'YAML' ? 'yaml' : 'json';
}

export function normalizeHighlightLanguage(language: string | undefined, value: string): HighlightLanguage {
  const normalized = language?.toLowerCase();
  if (normalized === 'yaml' || normalized === 'yml') {
    return 'yaml';
  }
  if (normalized === 'json') {
    return 'json';
  }
  if (normalized === 'bash' || normalized === 'shell' || normalized === 'sh' || normalized === 'zsh' || normalized === 'cli' || normalized === 'curl') {
    return 'bash';
  }
  if (normalized === 'py' || normalized === 'python') {
    return 'python';
  }
  if (normalized === 'ts' || normalized === 'tsx' || normalized === 'typescript') {
    return 'typescript';
  }
  if (normalized === 'js' || normalized === 'jsx' || normalized === 'javascript') {
    return 'javascript';
  }
  return looksLikeJson(value) ? 'json' : 'plaintext';
}

export function HighlightedCode({ code, language, className }: { code: string; language: HighlightLanguage; className?: string }) {
  const codeLanguage = language === 'bash-yaml' ? 'bash' : language;
  return (
    <code
      className={clsx('whitespace-pre-wrap break-words', className, codeLanguage !== 'plaintext' && `language-${codeLanguage}`)}
      dangerouslySetInnerHTML={{ __html: highlightCodeHtml(code, language) }}
    />
  );
}

export function highlightCodeHtml(code: string, language: HighlightLanguage): string {
  if (language === 'plaintext') {
    return escapeHtml(code);
  }
  if (language === 'bash-yaml') {
    return highlightBashYamlCommand(code);
  }
  return highlightRegisteredLanguage(code, language);
}

export function highlightBashYamlCommand(code: string): string {
  const heredocStart = code.indexOf('<<YAML\n');
  if (heredocStart < 0) {
    return highlightRegisteredLanguage(code, 'bash');
  }

  const bodyStart = heredocStart + '<<YAML\n'.length;
  const beforeYaml = code.slice(0, bodyStart);
  const rest = code.slice(bodyStart);
  const closingMatch = rest.match(/([\s\S]*?)(\nYAML)$/);
  const yamlBody = closingMatch ? closingMatch[1] ?? '' : rest;
  const closingYaml = closingMatch?.[2] ?? '';

  return [
    highlightRegisteredLanguage(beforeYaml, 'bash'),
    highlightRegisteredLanguage(yamlBody, 'yaml'),
    closingYaml ? highlightRegisteredLanguage(closingYaml, 'bash') : ''
  ].join('');
}

export function highlightRegisteredLanguage(code: string, language: Exclude<HighlightLanguage, 'bash-yaml' | 'plaintext'>): string {
  if (!hljs.getLanguage(language)) {
    return escapeHtml(code);
  }
  try {
    return hljs.highlight(code, { language, ignoreIllegals: true }).value;
  } catch {
    return escapeHtml(code);
  }
}

export function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export function FormatSelect({
  value,
  onChange,
  compact = false,
  align = 'right',
  buttonClassName,
  menuClassName
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
    { value: 'JSON', label: 'JSON' }
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
          buttonClassName
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
      className="subtle-scrollbar overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-words px-3 py-3 font-mono text-[12px] leading-[18px] text-foreground"
      style={{ maxHeight }}
    >
      <HighlightedCode code={code} language="bash-yaml" />
    </pre>
  );
}

export function ScrollableCodeBlock({ code, language }: { code: string; language: HighlightLanguage }) {
  return (
    <pre className="subtle-scrollbar max-h-80 overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-words px-3 py-3 font-mono text-[12px] leading-[18px] text-foreground">
      <HighlightedCode code={code} language={language} />
    </pre>
  );
}

const maxVisibleTemplateTags = 4;

export function TemplateCard({
  template,
  onClick
}: {
  template: AgentTemplate;
  onClick: () => void;
}) {
  const { msg } = useI18n();
  const title = templateTitle(template, msg);
  const body = templateBody(template, msg);
  const label = [title, body, ...(template.tags?.map((tag) => tag.label) ?? [])].join(' ');
  const tags = template.tags ?? [];
  const visibleTags = tags.slice(0, maxVisibleTemplateTags);
  const hiddenTagCount = tags.length - visibleTags.length;
  return (
    <Button
      type="button"
      variant="ghost"
      aria-label={label}
      className="h-full min-h-0 w-full flex-col items-start justify-start gap-0 overflow-hidden whitespace-normal rounded-lg border border-border bg-card p-3 text-left shadow-sm transition-colors hover:border-border hover:bg-card"
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
              title={`${hiddenTagCount} more tags`}
            >
              +{hiddenTagCount}
            </span>
          ) : null}
        </div>
      ) : null}
    </Button>
  );
}

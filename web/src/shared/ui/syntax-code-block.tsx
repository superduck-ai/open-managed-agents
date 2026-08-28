import { cn } from '../lib/utils';
import { highlightCodeHTML, normalizeHighlightLanguage, type HighlightLanguage } from './syntax-highlighting';

export function SyntaxCodeBlock({
  value,
  language,
  maxHeightClassName,
  className,
  wrap = true,
  testId = 'session-trace-code-block',
}: {
  value: string;
  language?: string;
  maxHeightClassName?: string;
  className?: string;
  wrap?: boolean;
  testId?: string;
}) {
  const highlightLanguage = normalizeHighlightLanguage(language, value);
  return (
    <pre
      data-testid={testId}
      className={cn(
        'rounded-lg border border-border bg-muted p-3 font-mono text-[13px] leading-[19px] text-foreground',
        wrap ? 'whitespace-pre-wrap break-words overflow-x-hidden' : 'subtle-scrollbar overflow-x-auto whitespace-pre',
        maxHeightClassName ? 'subtle-scrollbar overflow-y-auto' : 'overflow-visible',
        maxHeightClassName,
        className,
      )}
    >
      <HighlightedCode code={value} language={highlightLanguage} wrap={wrap} />
    </pre>
  );
}

export function HighlightedCode({
  code,
  language,
  className,
  wrap = true,
}: {
  code: string;
  language: HighlightLanguage;
  className?: string;
  wrap?: boolean;
}) {
  const codeLanguage = language === 'bash-yaml' ? 'bash' : language;
  return (
    <code
      className={cn(
        wrap ? 'whitespace-pre-wrap break-words' : 'whitespace-pre',
        className,
        codeLanguage !== 'plaintext' && `language-${codeLanguage}`,
      )}
      dangerouslySetInnerHTML={{ __html: highlightCodeHTML(code, language) }}
    />
  );
}

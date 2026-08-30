import { createElement, isValidElement, type ComponentPropsWithoutRef, type ReactNode } from 'react';
import ReactMarkdown, { type Components, type UrlTransform } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { SyntaxCodeBlock } from '../components/CodeBlocks';
import { parseTranscriptCode } from './sessionTraceModel';

const TRANSCRIPT_TEXT_LIMIT = 50_000;
const transcriptHeadingClassName = 'text-[15px] font-semibold leading-5 text-foreground';

function transcriptHeading(tag: 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6') {
  return ({ node: _node, ...props }: ComponentPropsWithoutRef<typeof tag> & { node?: unknown }) =>
    createElement(tag, { ...props, className: transcriptHeadingClassName });
}

const transcriptMarkdownComponents: Components = {
  a: ({ node: _node, href, ...props }) => (
    <a
      {...props}
      href={href || undefined}
      target="_blank"
      rel="noreferrer"
      className="font-medium text-session-link underline decoration-session-link/35 underline-offset-2 hover:decoration-current"
    />
  ),
  blockquote: ({ node: _node, ...props }) => (
    <blockquote {...props} className="border-l-2 border-border pl-3 text-muted-foreground" />
  ),
  code: ({ node: _node, ...props }) => (
    <code
      {...props}
      className="rounded border border-border bg-secondary px-1 py-0.5 font-mono text-[0.92em] text-foreground"
    />
  ),
  del: ({ node: _node, ...props }) => <del {...props} className="text-muted-foreground" />,
  h1: transcriptHeading('h1'),
  h2: transcriptHeading('h2'),
  h3: transcriptHeading('h3'),
  h4: transcriptHeading('h4'),
  h5: transcriptHeading('h5'),
  h6: transcriptHeading('h6'),
  hr: ({ node: _node, ...props }) => <hr {...props} className="border-border/70" />,
  img: ({ node: _node, src, ...props }) => (
    <img
      {...props}
      src={src || undefined}
      loading="lazy"
      decoding="async"
      className="max-h-[32rem] max-w-full rounded-lg border border-border object-contain"
    />
  ),
  input: ({ node: _node, ...props }) => <input {...props} className="mt-0.5 size-4 accent-primary" />,
  li: ({ node: _node, ...props }) => <li {...props} className="pl-1 marker:text-muted-foreground" />,
  ol: ({ node: _node, ...props }) => <ol {...props} className="list-decimal pl-5" />,
  p: ({ node: _node, ...props }) => <p {...props} className="break-words text-sm leading-5 text-foreground" />,
  pre: ({ node: _node, children, ...props }) => {
    if (!isValidElement<{ className?: string; children?: ReactNode }>(children)) {
      return (
        <pre {...props} className="whitespace-pre-wrap break-words">
          {children}
        </pre>
      );
    }
    const language = /language-([\w-]+)/.exec(children.props.className ?? '')?.[1];
    return <SyntaxCodeBlock value={String(children.props.children ?? '').replace(/\n$/, '')} language={language} />;
  },
  table: ({ node: _node, ...props }) => (
    <div className="scrollbar-none overflow-x-auto rounded-md border border-border">
      <table {...props} className="min-w-full border-collapse text-left text-sm" />
    </div>
  ),
  td: ({ node: _node, ...props }) => <td {...props} className="border-t border-border px-3 py-2 align-top" />,
  th: ({ node: _node, ...props }) => (
    <th {...props} className="border-b border-border bg-secondary px-3 py-2 font-semibold text-foreground" />
  ),
  ul: ({ node: _node, ...props }) => <ul {...props} className="list-disc pl-5" />,
};

const transcriptMarkdownUrlTransform: UrlTransform = (value, key) => {
  if (/^https?:/i.test(value) || (value.startsWith('/') && !value.startsWith('//'))) {
    return value;
  }
  if (key === 'href' && (/^mailto:/i.test(value) || value.startsWith('#'))) {
    return value;
  }
  return '';
};

export function TranscriptContent({ value }: { value: string }) {
  const code = value.length <= TRANSCRIPT_TEXT_LIMIT ? parseTranscriptCode(value) : null;
  if (code) {
    return <SyntaxCodeBlock value={code.value} language={code.language} />;
  }

  return <MarkdownTranscriptContent value={value} />;
}

export function MarkdownTranscriptContent({ value }: { value: string }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const truncated = value.length > TRANSCRIPT_TEXT_LIMIT;
  const displayedValue = truncated ? value.slice(0, TRANSCRIPT_TEXT_LIMIT) : value;
  return (
    <div
      data-testid="session-trace-markdown"
      className="flex flex-col gap-1.5 whitespace-normal text-sm leading-5 text-foreground"
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        urlTransform={transcriptMarkdownUrlTransform}
        components={transcriptMarkdownComponents}
      >
        {displayedValue}
      </ReactMarkdown>
      {truncated ? (
        <p role="note" className="mt-1 text-xs text-muted-foreground">
          {msg(
            'managedAgents.sessions.trace.truncatedContent',
            'Truncated — showing the first {limit} of {total} characters.',
            {
              limit: formatters.number(TRANSCRIPT_TEXT_LIMIT),
              total: formatters.number(value.length),
            },
          )}
        </p>
      ) : null}
    </div>
  );
}

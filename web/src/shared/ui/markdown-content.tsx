import { isValidElement, type ComponentPropsWithoutRef, type ReactNode } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import clsx from 'clsx';
import { MermaidDiagram } from './mermaid-diagram';
import { remarkMermaidFenceState } from './remark-mermaid-fence-state';
import { SyntaxCodeBlock } from './syntax-code-block';

function safeMarkdownUrl(url: string) {
  const allowedScheme = /^(https?:|mailto:|#)/i.test(url);
  const internalPath = url.startsWith('/') && !url.startsWith('//');
  return allowedScheme || internalPath ? url : '';
}

function MarkdownLink({ href = '', children, title }: ComponentPropsWithoutRef<'a'>) {
  if (!href) return <>{children}</>;

  const external = /^(https?:|mailto:)/i.test(href);
  return (
    <a
      href={href}
      title={title}
      className="text-primary underline underline-offset-2 hover:text-primary/80"
      {...(external ? { target: '_blank', rel: 'noreferrer noopener' } : {})}
    >
      {children}
    </a>
  );
}

const markdownComponents: Components = {
  h1: ({ children }) => <h1 className="text-lg font-semibold leading-7">{children}</h1>,
  h2: ({ children }) => <h2 className="text-base font-semibold leading-6">{children}</h2>,
  h3: ({ children }) => <h3 className="font-semibold leading-6">{children}</h3>,
  h4: ({ children }) => <h4 className="font-semibold leading-6">{children}</h4>,
  p: ({ children }) => <p className="whitespace-pre-wrap break-words">{children}</p>,
  strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
  ul: ({ children }) => <ul className="list-disc space-y-1 pl-5">{children}</ul>,
  ol: ({ children }) => <ol className="list-decimal space-y-1 pl-5">{children}</ol>,
  li: ({ children }) => <li className="pl-1">{children}</li>,
  blockquote: ({ children }) => (
    <blockquote className="border-l-2 border-border pl-3 text-muted-foreground">{children}</blockquote>
  ),
  a: MarkdownLink,
  pre: ({ children }) => {
    if (
      isValidElement<{
        children?: ReactNode;
        className?: string;
        'data-mermaid-closed'?: boolean;
      }>(children)
    ) {
      const child = children;
      const source = String(child.props.children ?? '').replace(/\n$/, '');
      const language = child.props.className?.match(/(?:^|\s)language-([^\s]+)/)?.[1];
      if (language?.toLowerCase() === 'mermaid' && child.props['data-mermaid-closed']) {
        return <MermaidDiagram source={source} />;
      }
      return (
        <SyntaxCodeBlock
          value={source}
          language={language}
          wrap
          testId="markdown-code-block"
          className="rounded-md bg-secondary"
        />
      );
    }
    return <pre>{children}</pre>;
  },
  code: ({ className, children }) => (
    <code className={clsx('rounded bg-secondary px-1 py-0.5 font-mono text-[0.92em]', className)}>{children}</code>
  ),
  table: ({ children }) => (
    <div className="subtle-scrollbar overflow-x-auto rounded-md border border-border">
      <table className="min-w-full border-collapse text-left text-sm">{children}</table>
    </div>
  ),
  th: ({ children }) => <th className="border-b border-border bg-secondary px-3 py-2 font-semibold">{children}</th>,
  td: ({ children }) => <td className="border-t border-border px-3 py-2 align-top">{children}</td>,
  hr: () => <hr className="border-border" />,
  img: () => null,
};
const markdownRemarkPlugins = [remarkGfm, remarkMermaidFenceState];

export function MarkdownContent({ value, className }: { value: string; className?: string }) {
  return (
    <div className={clsx('space-y-3 whitespace-normal break-words', className)}>
      <ReactMarkdown
        remarkPlugins={markdownRemarkPlugins}
        components={markdownComponents}
        skipHtml
        urlTransform={safeMarkdownUrl}
      >
        {value}
      </ReactMarkdown>
    </div>
  );
}

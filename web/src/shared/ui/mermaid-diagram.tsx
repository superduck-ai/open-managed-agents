import { useEffect, useId, useState } from 'react';
import { useI18n } from '../i18n';
import { useTheme, type ResolvedTheme } from '../theme/context';
import { SyntaxCodeBlock } from './syntax-code-block';

const MAX_MERMAID_SOURCE_LENGTH = 20_000;
const MERMAID_RENDER_DELAY_MS = 150;

type RenderedDiagram = { svg: string };
type MermaidRenderState =
  { key: string; status: 'loading' } | { key: string; status: 'ready'; svg: string } | { key: string; status: 'error' };

let renderQueue: Promise<void> = Promise.resolve();
let renderSequence = 0;

export function MermaidDiagram({ source }: { source: string }) {
  const { msg } = useI18n();
  const { resolvedTheme } = useTheme();
  const reactId = useId();
  const renderId = `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, '')}`;
  const renderKey = `${resolvedTheme}\u0000${source}`;
  const sourceTooLong = source.length > MAX_MERMAID_SOURCE_LENGTH;
  const [renderState, setRenderState] = useState<MermaidRenderState>({ key: renderKey, status: 'loading' });
  const state: MermaidRenderState = renderState.key === renderKey ? renderState : { key: renderKey, status: 'loading' };

  useEffect(() => {
    if (sourceTooLong) {
      return;
    }

    let current = true;
    const timer = window.setTimeout(() => {
      void renderMermaid(renderId, source, resolvedTheme).then(
        ({ svg }) => {
          if (current) {
            setRenderState({ key: renderKey, status: 'ready', svg });
          }
        },
        () => {
          if (current) {
            setRenderState({ key: renderKey, status: 'error' });
          }
        },
      );
    }, MERMAID_RENDER_DELAY_MS);

    return () => {
      current = false;
      window.clearTimeout(timer);
    };
  }, [renderId, renderKey, resolvedTheme, source, sourceTooLong]);

  if (!sourceTooLong && state.status === 'ready') {
    return (
      <div
        data-mermaid-diagram
        role="img"
        aria-label={msg('common.markdown.mermaidDiagram', 'Mermaid diagram')}
        className="subtle-scrollbar overflow-x-auto rounded-md border border-border bg-background p-3 [&_svg]:mx-auto [&_svg]:h-auto [&_svg]:min-w-64"
        dangerouslySetInnerHTML={{ __html: state.svg }}
      />
    );
  }

  return (
    <div data-mermaid-state={sourceTooLong ? 'error' : state.status} className="space-y-2">
      {sourceTooLong || state.status === 'error' ? (
        <p role="status" className="text-xs text-muted-foreground">
          {sourceTooLong
            ? msg('common.markdown.mermaidTooLong', 'Mermaid diagram is too large; showing source.')
            : msg('common.markdown.mermaidInvalid', 'Mermaid diagram could not be rendered; showing source.')}
        </p>
      ) : null}
      <SyntaxCodeBlock
        value={source}
        language="plaintext"
        wrap
        testId="mermaid-source-fallback"
        className="rounded-md bg-secondary"
      />
    </div>
  );
}

function renderMermaid(id: string, source: string, theme: ResolvedTheme): Promise<RenderedDiagram> {
  const uniqueId = `${id}-${++renderSequence}`;
  const render = renderQueue.then(async () => {
    const { default: mermaid } = await import('mermaid');
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      suppressErrorRendering: true,
      maxTextSize: MAX_MERMAID_SOURCE_LENGTH,
      maxEdges: 200,
      theme: theme === 'dark' ? 'dark' : 'default',
    });

    const valid = await mermaid.parse(source, { suppressErrors: true });
    if (!valid) {
      throw new Error('Invalid Mermaid diagram');
    }
    const { svg } = await mermaid.render(uniqueId, source);
    return { svg };
  });

  renderQueue = render.then(
    () => undefined,
    () => undefined,
  );
  return render;
}

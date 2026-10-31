import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ArrowLeft, ArrowUpRight, Maximize2, Minimize2, X } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { ButtonLink } from '../../../../shared/ui/button';
import './builder-panels.css';
import { EnvironmentSummary } from '../../environments/preview';
import { useEnvironment } from '../../environments/data';
import { ManagedErrorAlert } from '../../components/common';
import { Button } from '../../../../shared/ui/button';
import { errorMessage } from '../../utils';
import { BuilderAgentPanel } from './BuilderAgentPanel';
import type { Builder } from './useBuilder';

import { BuilderPanelContext, type BuilderPanel } from './panelContext';
import { BuilderInspector } from './BuilderInspector';

export function BuilderPanels({
  builder,
  workspaceId,
  children,
}: {
  builder: Builder;
  workspaceId: string;
  children: ReactNode;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const [panel, setPanel] = useState<BuilderPanel | null>(null);
  const [open, setOpen] = useState(false);
  const workspace = useRef<HTMLDivElement>(null);
  const panelElement = useRef<HTMLElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const widthRef = useRef(0);
  const [workspaceWidth, setWorkspaceWidth] = useState(0);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    const element = workspace.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      widthRef.current = entry.contentRect.width;
      setWorkspaceWidth(entry.contentRect.width);
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const split = workspaceWidth >= 1004 && !expanded;
  useEffect(() => {
    if (open && !split) panelElement.current?.focus({ preventScroll: true });
  }, [open, split]);
  const proposal = builder.state.gate?.name === 'build_agent_config' ? builder.state.gate.id : null;
  useEffect(() => {
    if (proposal) {
      setPanel({ kind: 'agent' });
      setOpen(widthRef.current >= 1004);
      setExpanded(false);
    }
  }, [proposal]);
  const activeSession = builder.state.testClosed ? undefined : builder.state.sessionID;
  const integration = builder.state.step === 'integrate';
  useEffect(() => {
    if (activeSession || integration) {
      setOpen(false);
      setExpanded(false);
    }
  }, [activeSession, integration]);
  const close = () => {
    setOpen(false);
    requestAnimationFrame(() => returnFocus.current?.focus());
  };
  const href = panelHref(panel, builder.state.agent?.id, workspaceId);
  return (
    <BuilderPanelContext.Provider
      value={(value) => {
        if (builder.draftEditing && value.kind !== panel?.kind) return;
        returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        setExpanded(false);
        setPanel(value);
        setOpen(true);
      }}
    >
      <div
        ref={workspace}
        data-config-open={open}
        data-config-split={split}
        className="builder-workspace grid min-h-0 min-w-0 flex-1 gap-6"
        style={{
          gridTemplateColumns:
            open && split
              ? `minmax(560px, 1fr) ${Math.min(560, Math.max(420, workspaceWidth * 0.36))}px`
              : 'minmax(0, 1fr)',
        }}
      >
        <div hidden={open && !split} className="builder-conversation flex min-h-0 min-w-0 flex-col">
          {children}
        </div>
        <aside
          ref={panelElement}
          tabIndex={-1}
          hidden={!open}
          aria-label={panelTitle(panel, zh)}
          className="builder-config-panel mx-auto flex w-full min-h-0 min-w-0 max-w-[960px] flex-col gap-3 rounded-xl border-[0.5px] border-border bg-card p-4"
          onKeyDown={(event) => {
            if (event.key === 'Escape' && !event.defaultPrevented) {
              event.stopPropagation();
              close();
            }
          }}
        >
          <PanelHeader
            title={panelTitle(panel, zh)}
            href={href}
            canExpand={workspaceWidth >= 1004}
            expanded={expanded}
            showBack={!split}
            onExpand={() => setExpanded(!expanded)}
            onClose={close}
          />
          <PanelBody panel={panel} builder={builder} workspaceId={workspaceId} />
          {panel?.kind === 'agent' && proposal && <PanelConfirmation builder={builder} onBack={close} />}
        </aside>
      </div>
    </BuilderPanelContext.Provider>
  );
}

function BuilderEnvironmentPanel({ id, name, workspaceId }: { id: string; name: string; workspaceId: string }) {
  const { locale } = useI18n();
  const query = useEnvironment(workspaceId, id);
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <h3 className="break-words text-base font-medium">{query.data?.name ?? name}</h3>
      <p className="mt-2 mb-5 break-all font-mono text-xs text-muted-foreground">{id}</p>
      {query.isPending ? (
        <p role="status" className="mt-4">
          {locale === 'zh-CN' ? '正在加载环境…' : 'Loading environment…'}
        </p>
      ) : query.error ? (
        <div className="mt-4 space-y-3">
          <ManagedErrorAlert>{errorMessage(query.error)}</ManagedErrorAlert>
          <Button variant="outline" onClick={() => void query.refetch()}>
            {locale === 'zh-CN' ? '重试' : 'Retry'}
          </Button>
        </div>
      ) : query.data ? (
        <EnvironmentSummary entity={query.data} />
      ) : null}
    </div>
  );
}

function panelHref(panel: BuilderPanel | null, agentId: string | undefined, workspaceId: string) {
  const base = `/workspaces/${encodeURIComponent(workspaceId)}`;
  if (panel?.kind === 'environment') return `${base}/environments/${encodeURIComponent(panel.id)}`;
  return panel?.kind === 'agent' && agentId ? `${base}/agents/${encodeURIComponent(agentId)}` : null;
}

function panelTitle(panel: BuilderPanel | null, zh: boolean) {
  if (panel?.kind === 'overview') return zh ? '构建配置' : 'Build configuration';
  if (panel?.kind === 'environment') return zh ? '环境' : 'Environment';
  return zh ? '智能体配置' : 'Agent config';
}

function PanelHeader({
  title,
  href,
  canExpand,
  expanded,
  showBack,
  onExpand,
  onClose,
}: {
  title: string;
  href: string | null;
  canExpand: boolean;
  expanded: boolean;
  showBack: boolean;
  onExpand: () => void;
  onClose: () => void;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  return (
    <header className="shrink-0 space-y-2">
      <div className="flex min-w-0 items-center gap-2">
        {showBack && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={zh ? '返回对话' : 'Back to conversation'}
            onClick={onClose}
          >
            <ArrowLeft />
          </Button>
        )}
        <h2 className="min-w-0 flex-1 text-base font-medium">{title}</h2>
        {canExpand && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={
              expanded ? (zh ? '并排查看' : 'View side by side') : zh ? '展开配置面板' : 'Expand configuration panel'
            }
            onClick={onExpand}
          >
            {expanded ? <Minimize2 /> : <Maximize2 />}
          </Button>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={zh ? '关闭配置面板' : 'Close configuration panel'}
          title={zh ? '关闭配置面板' : 'Close configuration panel'}
          onClick={onClose}
        >
          <X />
        </Button>
      </div>
      {href && (
        <ButtonLink
          variant="ghost"
          size="sm"
          className="w-fit px-0"
          href={href}
          target="_blank"
          rel="noopener noreferrer"
        >
          {zh ? '在管理页打开' : 'Open management page'}
          <ArrowUpRight />
        </ButtonLink>
      )}
    </header>
  );
}

function PanelConfirmation({ builder, onBack }: { builder: Builder; onBack: () => void }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const version = builder.state.agent?.version;
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2">
      <Button
        disabled={builder.busy || builder.draftEditing || Boolean(builder.state.operation)}
        onClick={() => void builder.confirm()}
      >
        {version ? `${zh ? '保存为' : 'Save as'} v${version + 1}` : zh ? '创建智能体' : 'Create agent'}
      </Button>
      <Button variant="ghost" onClick={onBack}>
        {zh ? '返回对话' : 'Back to conversation'}
      </Button>
    </div>
  );
}

function PanelBody({
  panel,
  builder,
  workspaceId,
}: {
  panel: BuilderPanel | null;
  builder: Builder;
  workspaceId: string;
}) {
  return (
    <>
      {panel?.kind === 'overview' && <BuilderInspector builder={builder} workspaceId={workspaceId} />}
      {panel?.kind === 'agent' && (
        <BuilderAgentPanel
          key={`${builder.state.gate?.id ?? 'saved'}:${builder.state.agent?.version ?? 0}`}
          builder={builder}
        />
      )}
      {panel?.kind === 'environment' && (
        <BuilderEnvironmentPanel id={panel.id} name={panel.name} workspaceId={workspaceId} />
      )}
    </>
  );
}

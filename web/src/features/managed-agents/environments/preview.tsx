import { ArrowUpRight, ChevronDown, ChevronUp, Cloud, Monitor, X } from 'lucide-react';
import { useRef, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { CopyButton, HighlightedCode } from '../components/CodeBlocks';
import { PackageIcon } from './package-icon';
import { Sheet, SheetContent, SheetTitle, SheetDescription, SheetClose } from '../../../shared/ui/sheet';
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
  type ResizablePanelHandle,
} from '../../../shared/ui/resizable';
import { ToggleGroup, ToggleGroupItem } from '../../../shared/ui/toggle-group';
import { CopyIdCell } from '../../../shared/ui/data-table-interactions';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { useEnvironment, useEnvironments } from './data';
import { EnvironmentActions, type EnvironmentActionRequest } from './actions';
import { EnvironmentHosting, EnvironmentScope } from './common';
import { handleInternalLinkClick, managedEntityDetailHref, objectRecord } from '../utils';
import { environmentErrorMessage, localizedRelativeTime } from '../resources/environment-model';
import { environmentFormValues, packageManagers } from './model';
import type { EnvironmentApiResponse } from '../types';

export function EnvironmentPreview({
  id,
  ids,
  workspaceId,
  onSelect,
  onClose,
  onAction,
}: {
  id: string;
  ids: string[];
  workspaceId: string;
  onSelect: (id: string) => void;
  onClose: () => void;
  onAction: (request: EnvironmentActionRequest) => void;
}) {
  const { msg } = useI18n();
  const query = useEnvironment(workspaceId, id);
  const [view, setView] = useState('rendered');
  const panel = useRef<ResizablePanelHandle>(null);
  const list = useEnvironments(workspaceId, null);
  const navigationIds = ids.length ? ids : (list.data?.data ?? []).map((item) => item.id);
  const popup = useRef<HTMLDivElement>(null);
  const index = navigationIds.indexOf(id);
  const entity = query.data;
  const href = managedEntityDetailHref(workspaceId, 'environments', id);
  const Icon = objectRecord(entity?.config).type === 'self_hosted' ? Monitor : Cloud;
  return (
    <Sheet
      open
      modal={false}
      onOpenChange={(open, details) => {
        if (!open && details.reason !== 'outside-press') onClose();
      }}
    >
      <SheetContent
        ref={popup}
        initialFocus={popup}
        showOverlay={false}
        showCloseButton={false}
        className="environment-preview pointer-events-none inset-y-2 right-2 h-[calc(100dvh-16px)] w-[calc(100vw-16px)] gap-0 border-0 bg-transparent shadow-none ring-0 sm:max-w-[calc(100vw-32px)]"
      >
        <ResizablePanelGroup orientation="horizontal" className="pointer-events-none">
          <ResizablePanel minSize={0} />
          <ResizableHandle
            aria-label={msg('environmentPage.resizePanel', 'Resize panel')}
            className="pointer-events-auto w-1 after:bg-transparent hover:after:bg-border"
            onDoubleClick={() => panel.current?.resize('560px')}
          />
          <ResizablePanel
            panelRef={panel}
            defaultSize="560px"
            minSize="320px"
            maxSize="100%"
            className="pointer-events-auto flex flex-col rounded-xl bg-background shadow-lg ring-1 ring-border"
          >
            <div className="flex items-center gap-2 px-4 pt-3 text-xs">
              <span>{msg('managedAgents.environments.environment', 'Environment')}</span>
              <CopyIdCell
                value={id}
                ariaLabel={msg('common.copyValue', 'Copy {value}', { value: id })}
                textClassName="text-muted-foreground font-normal"
              />
              <div className="ml-auto flex">
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={index <= 0}
                  aria-label={msg('environmentPage.previous', 'Previous environment')}
                  onClick={() => onSelect(navigationIds[index - 1])}
                >
                  <ChevronUp className="size-4" />
                </Button>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={index < 0 || index >= navigationIds.length - 1}
                  aria-label={msg('environmentPage.next', 'Next environment')}
                  onClick={() => onSelect(navigationIds[index + 1])}
                >
                  <ChevronDown className="size-4" />
                </Button>
                <SheetClose
                  render={<Button size="icon-xs" variant="ghost" aria-label={msg('common.close', 'Close')} />}
                >
                  <X className="size-4" />
                </SheetClose>
              </div>
            </div>
            <div className="flex items-center gap-2 px-4 py-4">
              <Icon className="size-5 shrink-0" strokeWidth={1.5} />
              <SheetTitle className="min-w-0 flex-1 truncate text-lg font-semibold">{entity?.name || id}</SheetTitle>
              <ButtonLink
                variant="outline"
                size="sm"
                href={href}
                onClick={(event) => handleInternalLinkClick(event, href)}
              >
                {msg('common.open', 'Open')}
                <ArrowUpRight className="size-3.5" />
              </ButtonLink>
              {entity ? <EnvironmentActions entity={entity} onAction={onAction} /> : null}
            </div>
            <SheetDescription className="sr-only">
              {msg('environmentPage.preview', 'Environment preview')}
            </SheetDescription>
            <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-6">
              {query.isPending ? (
                <Skeleton className="h-48 w-full" />
              ) : query.error ? (
                <Alert variant="destructive">
                  <AlertDescription>{environmentErrorMessage(query.error, 'load', msg)}</AlertDescription>
                </Alert>
              ) : entity ? (
                view === 'api' ? (
                  <EnvironmentApi entity={entity} />
                ) : (
                  <EnvironmentSummary entity={entity} />
                )
              ) : null}
            </div>
            <div className="flex justify-end px-4 py-3">
              <ToggleGroup
                aria-label={msg('environmentPage.view', 'View')}
                value={[view]}
                onValueChange={(next) => next.length && setView(next[0])}
                className="bg-transparent"
                size="sm"
              >
                <ToggleGroupItem value="rendered" className="aria-pressed:border-border aria-pressed:shadow-none">
                  {msg('environmentPage.rendered', 'Rendered')}
                </ToggleGroupItem>
                <ToggleGroupItem value="api" className="aria-pressed:border-border aria-pressed:shadow-none">
                  API
                </ToggleGroupItem>
              </ToggleGroup>
            </div>
          </ResizablePanel>
        </ResizablePanelGroup>
      </SheetContent>
    </Sheet>
  );
}

function EnvironmentApi({ entity }: { entity: EnvironmentApiResponse }) {
  const { msg } = useI18n();
  const json = JSON.stringify(entity, null, 2);

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
        <span className="rounded bg-green-500/10 px-1 py-0.5 text-green-700 dark:text-green-400">GET</span>
        <code>/v1/environments/{'{id}'}</code>
        <a
          href="https://platform.claude.com/docs/en/api/beta/environments/retrieve"
          target="_blank"
          rel="noreferrer"
          className="ml-auto inline-flex items-center gap-1 text-muted-foreground"
        >
          {msg('common.docs', 'Docs')}
          <ArrowUpRight className="size-3" />
        </a>
      </div>
      <div className="overflow-hidden rounded-xl border border-border bg-muted/40">
        <div className="flex items-center justify-between border-b border-border px-3 py-2 text-xs text-muted-foreground">
          <span className="font-mono">JSON</span>
          <CopyButton value={json} label={msg('environmentPage.copyJson', 'Copy JSON')} />
        </div>
        <pre
          className="m-0 whitespace-pre-wrap break-words p-4 font-mono text-[13px] leading-relaxed"
          tabIndex={0}
          aria-label={msg('environmentPage.apiResponse', 'API response')}
        >
          <HighlightedCode code={json} language="json" />
        </pre>
      </div>
    </div>
  );
}

function EnvironmentSummary({ entity }: { entity: EnvironmentApiResponse }) {
  const { msg } = useI18n();
  const format = useFormatters();
  const values = environmentFormValues(entity);
  const archived = Boolean(entity.archived_at || entity.state === 'archived');
  const items = [
    [
      msg('common.created', 'Created'),
      format.date(entity.created_at, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }),
    ],
    [msg('common.updated', 'Updated'), localizedRelativeTime(entity.updated_at, format.relativeTime)],
    [
      msg('environmentPage.state', 'State'),
      <span className="inline-flex items-center gap-1.5">
        <span className={`size-1.5 rounded-full ${archived ? 'bg-muted-foreground' : 'bg-green-600'}`} />
        {archived ? msg('common.archived', 'Archived') : msg('common.active', 'Active')}
      </span>,
    ],
    [msg('common.type', 'Type'), <EnvironmentHosting entity={entity} />],
    [msg('environmentPage.scope', 'Scope'), <EnvironmentScope scope={entity.scope} />],
  ];
  const none = <p className="text-muted-foreground">{msg('common.none', 'None')}</p>;
  return (
    <div className="space-y-8 text-sm">
      <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-4 gap-y-3">
        {items.map(([label, value], index) => (
          <div key={index} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <section>
        <h3 className="mb-3 font-medium">{msg('common.description', 'Description')}</h3>
        {entity.description ? <p className="whitespace-pre-wrap">{entity.description}</p> : none}
      </section>
      {values.hosting === 'cloud' ? (
        <>
          <section>
            <h3 className="mb-3 font-medium">{msg('managedAgents.environments.networking.title', 'Networking')}</h3>
            <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-3">
              <dt className="text-muted-foreground">{msg('common.type', 'Type')}</dt>
              <dd>
                {values.networkType === 'unrestricted'
                  ? msg('managedAgents.environments.networking.unrestricted', 'Unrestricted')
                  : msg('managedAgents.environments.networking.limited', 'Limited')}
              </dd>
            </dl>
            {values.networkType === 'limited' ? (
              <div className="mt-3 space-y-2 text-muted-foreground">
                <p>
                  {msg('environmentPage.allowPackages', 'Allow package managers')}:{' '}
                  {values.allowPackageManagers ? msg('common.yes', 'Yes') : msg('common.no', 'No')}
                </p>
                <p>
                  {msg('environmentPage.allowMcp', 'Allow MCP servers')}:{' '}
                  {values.allowMcpServers ? msg('common.yes', 'Yes') : msg('common.no', 'No')}
                </p>
                <p className="whitespace-pre-wrap">{values.allowedHostsText}</p>
              </div>
            ) : null}
          </section>
          <section>
            <h3 className="mb-3 font-medium">{msg('managedAgents.environments.packages.title', 'Packages')}</h3>
            {values.packages.length ? (
              <div className="space-y-3">
                {packageManagers.map(({ id, name }) => {
                  const packages = values.packages.filter((row) => row.manager === id);
                  return packages.length ? (
                    <div key={id}>
                      <p className="mb-2 flex items-center gap-2">
                        <PackageIcon manager={id} />
                        <span>{name}</span>
                        <span className="text-xs text-muted-foreground">{id}</span>
                      </p>
                      <div className="flex flex-wrap gap-1.5">
                        {packages.map(({ value }) => (
                          <code
                            key={value}
                            className="max-w-full break-all rounded-md bg-muted px-2 py-1 text-xs leading-5"
                          >
                            {value}
                          </code>
                        ))}
                      </div>
                    </div>
                  ) : null;
                })}
              </div>
            ) : (
              none
            )}
          </section>
        </>
      ) : null}
      <section>
        <h3 className="mb-3 font-medium">{msg('managedAgents.environments.metadata.title', 'Metadata')}</h3>
        {values.metadataRows.length ? (
          <dl className="grid grid-cols-2 gap-2">
            {values.metadataRows.map(({ key, value }) => (
              <div key={key} className="contents">
                <dt className="break-all text-muted-foreground">{key}</dt>
                <dd className="break-all">{value}</dd>
              </div>
            ))}
          </dl>
        ) : (
          none
        )}
      </section>
    </div>
  );
}

import './styles.css';
import { Cloud, Monitor } from 'lucide-react';
import { useState } from 'react';
import { useLocation } from '@tanstack/react-router';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { useWorkspace } from '../../../shared/workspaces/context';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Button } from '../../../shared/ui/button';
import { CopyIdCell } from '../../../shared/ui/data-table-interactions';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import { managedEntityListHref, navigateToInternalHref, objectRecord } from '../utils';
import { environmentErrorMessage } from '../resources/environment-model';
import { EnvironmentWorkPanel } from '../resources/environment-details';
import { EnvironmentActionDialog, EnvironmentActions, type EnvironmentActionRequest } from './actions';
import { EnvironmentHosting, EnvironmentScope } from './common';
import { useEnvironment, useEnvironmentRefresh } from './data';
import { EnvironmentForm } from './form';
import { EnvironmentList } from './list';
import { EnvironmentPreview } from './preview';

export function EnvironmentsPage() {
  const { activeWorkspaceId } = useWorkspace();
  const location = useLocation();
  const segment = location.pathname.split('/environments/')[1];
  const id = segment ? decodeURIComponent(segment.split('/')[0]) : undefined;
  const selectedId = new URLSearchParams(location.searchStr).get('environment') || undefined;
  return (
    <EnvironmentPageContent key={activeWorkspaceId} workspaceId={activeWorkspaceId} id={id} selectedId={selectedId} />
  );
}

function EnvironmentPageContent({
  workspaceId,
  id,
  selectedId,
}: {
  workspaceId: string;
  id?: string;
  selectedId?: string;
}) {
  const { msg } = useI18n();
  const [action, setAction] = useState<EnvironmentActionRequest | null>(null);
  const [previewIds, setPreviewIds] = useState<string[]>([]);
  const listHref = managedEntityListHref(workspaceId, 'environments');
  const refresh = useEnvironmentRefresh(workspaceId);
  const preview = (next?: string) =>
    navigateToInternalHref(next ? `${listHref}?environment=${encodeURIComponent(next)}` : listHref);
  return (
    <div data-environment-page className="environment-page min-w-0 text-foreground [&_svg]:shrink-0">
      {id ? (
        <EnvironmentDetail workspaceId={workspaceId} id={id} listHref={listHref} onAction={setAction} />
      ) : (
        <EnvironmentList
          workspaceId={workspaceId}
          selectedId={selectedId}
          listHref={listHref}
          onAction={setAction}
          onPreview={(next, ids) => {
            setPreviewIds(ids);
            preview(next);
          }}
        />
      )}
      {!id && selectedId ? (
        <EnvironmentPreview
          id={selectedId}
          ids={previewIds}
          workspaceId={workspaceId}
          onSelect={preview}
          onClose={() => preview()}
          onAction={setAction}
        />
      ) : null}
      {action ? (
        <EnvironmentActionDialog
          request={action}
          workspaceId={workspaceId}
          onClose={() => setAction(null)}
          onChanged={(ids) => {
            refresh();
            if (action.action === 'delete' && (ids.includes(id || '') || ids.includes(selectedId || '')))
              navigateToInternalHref(listHref);
          }}
        />
      ) : null}
      <span className="sr-only">{msg('managedAgents.environments.title', 'Environments')}</span>
    </div>
  );
}

function EnvironmentDetail({
  workspaceId,
  id,
  listHref,
  onAction,
}: {
  workspaceId: string;
  id: string;
  listHref: string;
  onAction: (request: EnvironmentActionRequest) => void;
}) {
  const { msg } = useI18n();
  const format = useFormatters();
  const creating = id === 'new';
  const query = useEnvironment(workspaceId, creating ? undefined : id);
  const refresh = useEnvironmentRefresh(workspaceId);
  const entity = query.data;
  const label = creating ? msg('environmentPage.newEnvironment', 'New environment') : entity?.name || id;
  const Icon = objectRecord(entity?.config).type === 'self_hosted' ? Monitor : Cloud;
  return (
    <div className="flex min-h-[calc(100dvh-112px)] flex-col md:min-h-[calc(100dvh-48px)]">
      <ManagedDetailBreadcrumb
        listHref={listHref}
        listLabel={msg('managedAgents.environments.title', 'Environments')}
        currentLabel={label}
        className="-mt-1 mb-6 shrink-0"
      />
      <div className={creating ? 'mx-auto flex w-full max-w-3xl flex-1 flex-col' : 'flex flex-1 flex-col lg:px-6'}>
        {creating ? (
          <header className="mb-6 shrink-0">
            <h1 className="text-[22px] font-medium tracking-tight">
              {msg('managedAgents.environments.create', 'Create environment')}
            </h1>
            <p className="mt-1.5 text-sm text-muted-foreground">
              {msg('environmentPage.createDescription', 'A reusable container template for sessions.')}
            </p>
          </header>
        ) : entity ? (
          <header className="mb-4 shrink-0">
            <div className="flex flex-wrap items-center gap-2">
              <Icon className="size-5" strokeWidth={1.5} />
              <h1 className="min-w-0 break-words text-[22px] font-medium tracking-tight">{entity.name}</h1>
              <span className="rounded bg-muted px-1.5 py-0.5 text-xs">
                <EnvironmentHosting entity={entity} />
              </span>
              <EnvironmentScope scope={entity.scope} />
              <div className="ml-auto">
                <EnvironmentActions entity={entity} onAction={onAction} />
              </div>
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
              <CopyIdCell
                value={entity.id}
                ariaLabel={msg('common.copyValue', 'Copy {value}', { value: entity.id })}
                textClassName="font-normal text-muted-foreground"
              />
              <span>
                {msg('environmentPage.lastUpdated', 'Last updated {date}', {
                  date: format.date(entity.updated_at, { month: 'short', day: 'numeric' }),
                })}
              </span>
            </div>
          </header>
        ) : null}
        {creating || entity ? (
          <>
            <EnvironmentForm
              key={entity?.id ?? 'new'}
              entity={entity}
              workspaceId={workspaceId}
              onCancel={() => navigateToInternalHref(listHref)}
              onSaved={(result) => {
                refresh(result);
                if (creating) navigateToInternalHref(`${listHref}/${encodeURIComponent(result.id)}`);
              }}
            />
            {!creating && entity ? (
              <EnvironmentWorkPanel environment={entity} workspaceId={workspaceId} refreshKey={query.dataUpdatedAt} />
            ) : null}
          </>
        ) : query.error ? (
          <Alert variant="destructive">
            <AlertDescription>
              {environmentErrorMessage(query.error, 'load', msg)}{' '}
              <Button variant="ghost" size="sm" onClick={() => void query.refetch()}>
                {msg('common.retry', 'Retry')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <Skeleton className="h-96 max-w-3xl" />
        )}
      </div>
    </div>
  );
}

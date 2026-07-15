import { Archive } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '../../../shared/ui/alert';
import { listEnvironmentWork } from '../api';
import { DetailCard, DetailKV, DetailTableCard } from '../components/common';
import { type EnvironmentApiResponse, type EnvironmentWorkApiResponse } from '../types';
import { compactEntityId, objectRecord } from '../utils';
import { environmentPackageRows } from './model';
import { environmentErrorMessage, environmentWorkStatusLabel, localizedRelativeTime } from './environment-model';

export function EnvironmentArchivedNotice() {
  const { msg } = useI18n();
  return (
    <Alert className="mb-6 max-w-[820px]">
      <Archive className="mt-0.5 size-4 shrink-0" aria-hidden />
      <AlertTitle>{msg('managedAgents.environments.archived.title', 'Archived environment')}</AlertTitle>
      <AlertDescription>
        {msg(
          'managedAgents.environments.archived.description',
          'This environment is read-only. Its configuration and work queue remain available for reference.',
        )}
      </AlertDescription>
    </Alert>
  );
}

export function EnvironmentReadOnlySections({ entity }: { entity: EnvironmentApiResponse }) {
  const { msg } = useI18n();
  const config = objectRecord(entity.config);
  const networking = objectRecord(config.networking);
  const packages = environmentPackageRows(config.packages);
  const metadata = objectRecord((entity as EnvironmentApiResponse & { metadata?: unknown }).metadata);
  const networkType = networking.type === 'limited' ? 'limited' : 'unrestricted';
  return (
    <div className="mt-7 space-y-7">
      <div>
        <h2 className="text-[20px] font-semibold text-foreground">
          {msg('managedAgents.environments.networking.title', 'Networking')}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {msg(
            'managedAgents.environments.networking.description',
            'Configure network access policies for this environment.',
          )}
        </p>
        <DetailKV
          label={msg('managedAgents.environments.networking.type', 'Type')}
          value={
            networkType === 'limited'
              ? msg('managedAgents.environments.networking.limited', 'Limited')
              : msg('managedAgents.environments.networking.unrestricted', 'Unrestricted')
          }
        />
      </div>
      <div className="border-t border-border pt-7">
        <h2 className="text-[20px] font-semibold text-foreground">
          {msg('managedAgents.environments.packages.title', 'Packages')}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {msg(
            'managedAgents.environments.packages.description',
            'Specify packages and versions available in this environment. Separate multiple values with spaces.',
          )}
        </p>
        {packages.length ? (
          <div className="mt-3 text-sm text-foreground">
            {packages.map((row) => `${row.manager}: ${row.value}`).join('  ')}
          </div>
        ) : (
          <div className="mt-3 text-sm text-muted-foreground/70">
            {msg('managedAgents.environments.packages.empty', 'No packages configured')}
          </div>
        )}
      </div>
      <div className="border-t border-border pt-7">
        <h2 className="text-[20px] font-semibold text-foreground">
          {msg('managedAgents.environments.metadata.title', 'Metadata')}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {msg(
            'managedAgents.environments.metadata.description',
            'Add up to 16 unique key-value pairs. Keys can use uppercase or lowercase letters.',
          )}
        </p>
        {Object.keys(metadata).length ? (
          <div className="mt-3 grid gap-2 text-sm text-foreground">
            {Object.entries(metadata).map(([key, value]) => (
              <div key={key} className="font-mono">
                {key}: {String(value)}
              </div>
            ))}
          </div>
        ) : (
          <div className="mt-3 text-sm text-muted-foreground/70">
            {msg('managedAgents.environments.metadata.empty', 'No metadata')}
          </div>
        )}
      </div>
    </div>
  );
}

export function EnvironmentWorkPanel({
  environment,
  workspaceId,
  refreshKey,
}: {
  environment: EnvironmentApiResponse;
  workspaceId: string;
  refreshKey: number;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [state, setState] = useState<{ loading: boolean; error: string | null; data: EnvironmentWorkApiResponse[] }>({
    loading: true,
    error: null,
    data: [],
  });
  useEffect(() => {
    let active = true;
    setState({ loading: true, error: null, data: [] });
    void listEnvironmentWork(environment.id, workspaceId)
      .then((page) => active && setState({ loading: false, error: null, data: page.data ?? [] }))
      .catch(
        (error: unknown) =>
          active && setState({ loading: false, error: environmentErrorMessage(error, 'work', msg), data: [] }),
      );
    return () => {
      active = false;
    };
  }, [environment.id, msg, refreshKey, workspaceId]);
  const relativeTime = (value: string) => localizedRelativeTime(value, formatters.relativeTime);
  const title = msg('managedAgents.environments.work.title', 'Work queue');
  const description = msg(
    'managedAgents.environments.work.description',
    'Inspect pending or claimed work for this environment.',
  );
  if (state.loading) {
    return (
      <DetailCard title={title} description={description}>
        <div className="rounded-lg border border-border bg-card px-4 py-12 text-center text-sm text-muted-foreground">
          {msg('managedAgents.environments.work.loading', 'Loading work queue...')}
        </div>
      </DetailCard>
    );
  }
  return (
    <DetailTableCard
      title={title}
      description={description}
      loading={false}
      error={state.error}
      emptyTitle={msg('managedAgents.environments.work.empty', 'No work queued')}
      columns={[
        msg('common.id', 'ID'),
        msg('common.status', 'Status'),
        msg('common.created', 'Created'),
        msg('managedAgents.common.updatedAt', 'Updated at'),
      ]}
      rows={state.data.map((work) => [
        compactEntityId(work.id),
        environmentWorkStatusLabel((work as EnvironmentWorkApiResponse & { state?: string }).state || work.status, msg),
        relativeTime(work.created_at),
        work.updated_at ? relativeTime(work.updated_at) : '—',
      ])}
    />
  );
}

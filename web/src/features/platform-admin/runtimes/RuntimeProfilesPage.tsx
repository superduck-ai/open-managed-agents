import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Activity, Cpu, MoreVertical, Pencil, Plus, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import {
  DataTableCell,
  DataTableRow,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '../../../shared/ui/data-table-interactions';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { ResourceListState } from '../../../shared/ui/resource-list-state';
import { ResourcePageHeader } from '../../../shared/ui/resource-page-header';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import {
  deleteRuntimeProfile,
  listRuntimeProfiles,
  testRuntimeProfile,
  type RuntimeProfileRecord,
  type RuntimeProfileTestResult,
} from '../../../shared/api/platform-admin';
import type { ApiError } from '../../../shared/api/client';
import { formatRuntimeProviderKind } from '../../../shared/workspaces/runtime';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';
import { RuntimeProfileDialog } from './RuntimeProfileDialog';

const runtimeProfilesQueryKey = ['platform-runtime-profiles'] as const;

export function RuntimeProfilesPage() {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const queryClient = useQueryClient();
  const errorMessage = usePlatformAdminError();
  const [editing, setEditing] = useState<RuntimeProfileRecord | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<RuntimeProfileRecord | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, RuntimeProfileTestResult>>({});

  const query = useQuery({
    queryKey: runtimeProfilesQueryKey,
    queryFn: () => listRuntimeProfiles(client),
  });

  const removeMutation = useMutation({
    mutationFn: (profile: RuntimeProfileRecord) => deleteRuntimeProfile(client, profile.id, profile.version),
    onSuccess: async () => {
      setDeleting(null);
      setDeleteError(null);
      await queryClient.invalidateQueries({ queryKey: runtimeProfilesQueryKey });
    },
    onError: async (mutationError: unknown) => {
      setDeleteError(errorMessage(mutationError));
      if ((mutationError as ApiError).code === 'runtime_profile_version_conflict') {
        await queryClient.invalidateQueries({ queryKey: runtimeProfilesQueryKey });
      }
    },
  });

  const handleTestConnection = async (profile: RuntimeProfileRecord) => {
    setTestingId(profile.id);
    try {
      const result = await testRuntimeProfile(client, profile.id);
      setTestResults((current) => ({ ...current, [profile.id]: result }));
      await queryClient.invalidateQueries({ queryKey: runtimeProfilesQueryKey });
    } catch (testError) {
      setTestResults((current) => ({
        ...current,
        [profile.id]: {
          status: 'unhealthy',
          latency_ms: 0,
          message: errorMessage(testError),
          checked_at: new Date().toISOString(),
        },
      }));
    } finally {
      setTestingId((current) => (current === profile.id ? null : current));
    }
  };

  const reload = async () => {
    setEditing(undefined);
    await queryClient.invalidateQueries({ queryKey: runtimeProfilesQueryKey });
  };

  const profiles = query.data ?? [];

  return (
    <section>
      <ResourcePageHeader
        title={msg('platformAdmin.runtimeProfiles.title', 'Runtime profiles')}
        description={msg(
          'platformAdmin.runtimeProfiles.description',
          'Manage shared agent runtime profiles and test upstream connectivity.',
        )}
        actions={
          <Button size="lg" onClick={() => setEditing(null)}>
            <Plus aria-hidden className="size-4" />
            {msg('platformAdmin.runtimeProfiles.create', 'Add runtime profile')}
          </Button>
        }
      />

      {query.isPending ? (
        <p role="status">{msg('common.loading', 'Loading...')}</p>
      ) : query.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(query.error)}
        </p>
      ) : profiles.length === 0 ? (
        <ResourceListState
          icon={Cpu}
          title={msg('platformAdmin.runtimeProfiles.empty', 'No runtime profiles yet')}
          body={msg(
            'platformAdmin.runtimeProfiles.emptyBody',
            'Add a shared external runtime profile to authorize organizations to create external workspaces.',
          )}
        />
      ) : (
        <RuntimeProfilesTable
          profiles={profiles}
          testingId={testingId}
          testResults={testResults}
          onTest={(profile) => void handleTestConnection(profile)}
          onEdit={setEditing}
          onDelete={(profile) => {
            setDeleteError(null);
            setDeleting(profile);
          }}
        />
      )}

      {editing !== undefined ? (
        <RuntimeProfileDialog
          profile={editing ?? undefined}
          onClose={() => setEditing(undefined)}
          onSaved={reload}
          onConflict={() => queryClient.invalidateQueries({ queryKey: runtimeProfilesQueryKey })}
        />
      ) : null}

      {deleting ? (
        <Dialog open onOpenChange={(open) => !open && !removeMutation.isPending && setDeleting(null)}>
          <DialogContent className="sm:max-w-md" closeButtonDisabled={removeMutation.isPending}>
            <DialogHeader>
              <DialogTitle>{msg('platformAdmin.runtimeProfiles.deleteTitle', 'Delete runtime profile')}</DialogTitle>
              <DialogDescription>
                {msg(
                  'platformAdmin.runtimeProfiles.deleteBody',
                  'Delete runtime profile "{name}"? Profiles currently authorized or bound to workspaces cannot be deleted.',
                  { name: deleting.name },
                )}
              </DialogDescription>
            </DialogHeader>
            {deleteError ? (
              <p role="alert" className="text-sm text-destructive">
                {deleteError}
              </p>
            ) : null}
            <DialogFooter>
              <DialogClose render={<Button type="button" variant="outline" disabled={removeMutation.isPending} />}>
                {msg('common.cancel', 'Cancel')}
              </DialogClose>
              <Button
                type="button"
                variant="destructive"
                disabled={removeMutation.isPending}
                onClick={() => removeMutation.mutate(deleting)}
              >
                {removeMutation.isPending
                  ? msg('common.saving', 'Saving...')
                  : msg('platformAdmin.runtimeProfiles.delete', 'Delete')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
    </section>
  );
}

function RuntimeProfilesTable({
  profiles,
  testingId,
  testResults,
  onTest,
  onEdit,
  onDelete,
}: {
  profiles: RuntimeProfileRecord[];
  testingId: string | null;
  testResults: Record<string, RuntimeProfileTestResult>;
  onTest: (profile: RuntimeProfileRecord) => void;
  onEdit: (profile: RuntimeProfileRecord) => void;
  onDelete: (profile: RuntimeProfileRecord) => void;
}) {
  const { msg } = useI18n();

  return (
    <Table className={`${dataTableClassName} min-w-[920px]`}>
      <TableHeader>
        <TableRow className={dataTableHeaderRowClassName}>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.runtimeProfiles.name', 'Name')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.runtimeProfiles.provider', 'Provider')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.runtimeProfiles.endpointUrl', 'Endpoint URL')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.runtimeProfiles.credentials', 'Credentials')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>{msg('platformAdmin.status', 'Status')}</TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.runtimeProfiles.health', 'Health')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName} />
        </TableRow>
      </TableHeader>
      <TableBody>
        {profiles.map((profile) => {
          const liveResult = testResults[profile.id];
          const isTesting = testingId === profile.id;
          return (
            <DataTableRow key={profile.id}>
              <DataTableCell edge="start" className="font-medium">
                <div className="flex flex-wrap items-center gap-2">
                  <span>{profile.name}</span>
                  {profile.builtin ? (
                    <Badge variant="outline">{msg('platformAdmin.runtimeProfiles.builtin', 'Built-in')}</Badge>
                  ) : null}
                </div>
                <div className="font-mono text-xs text-muted-foreground">{profile.id}</div>
              </DataTableCell>
              <DataTableCell>{formatRuntimeProviderKind(profile.provider_kind)}</DataTableCell>
              <DataTableCell className="max-w-[260px] truncate font-mono text-xs">
                {profile.endpoint_url || '—'}
              </DataTableCell>
              <DataTableCell className="text-xs">
                {profile.builtin ? (
                  <span className="text-muted-foreground">—</span>
                ) : profile.has_credentials ? (
                  <span className="font-mono text-foreground">{profile.credential_hint || 'configured'}</span>
                ) : (
                  <span className="text-muted-foreground">
                    {msg('platformAdmin.runtimeProfiles.notConfigured', 'Not configured')}
                  </span>
                )}
              </DataTableCell>
              <DataTableCell>
                <RuntimeProfileStatusBadge status={profile.status} />
              </DataTableCell>
              <DataTableCell>
                <RuntimeProfileHealthCell profile={profile} liveResult={liveResult} />
              </DataTableCell>
              <DataTableCell edge="end">
                <div className="flex items-center justify-end gap-1">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={isTesting}
                    onClick={() => onTest(profile)}
                  >
                    <Activity className="size-3.5" aria-hidden />
                    {isTesting
                      ? msg('platformAdmin.runtimeProfiles.testing', 'Testing...')
                      : msg('platformAdmin.runtimeProfiles.testConnection', 'Test connection')}
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={msg('skills.moreFor', 'More actions for {name}', { name: profile.name })}
                        />
                      }
                    >
                      <MoreVertical className="size-4" strokeWidth={1.5} aria-hidden />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-44">
                      <DropdownMenuItem onClick={() => onEdit(profile)}>
                        <Pencil size={15} aria-hidden />
                        {msg('common.edit', 'Edit')}
                      </DropdownMenuItem>
                      {!profile.builtin ? (
                        <>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onClick={() => onDelete(profile)}>
                            <Trash2 size={15} aria-hidden />
                            {msg('platformAdmin.runtimeProfiles.delete', 'Delete')}
                          </DropdownMenuItem>
                        </>
                      ) : null}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </DataTableCell>
            </DataTableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function RuntimeProfileStatusBadge({ status }: { status: RuntimeProfileRecord['status'] }) {
  const { msg } = useI18n();
  if (status === 'active') {
    return <Badge variant="secondary">{msg('platformAdmin.active', 'Active')}</Badge>;
  }
  if (status === 'archived') {
    return <Badge variant="outline">{msg('platformAdmin.archived', 'Archived')}</Badge>;
  }
  return <Badge variant="outline">{msg('platformAdmin.runtimeProfiles.disabled', 'Disabled')}</Badge>;
}

function RuntimeProfileHealthCell({
  profile,
  liveResult,
}: {
  profile: RuntimeProfileRecord;
  liveResult?: RuntimeProfileTestResult;
}) {
  const { msg } = useI18n();
  const status = liveResult?.status || profile.last_health_status || 'unknown';
  const message = liveResult?.message || profile.last_health_error;
  const latency = liveResult ? liveResult.latency_ms : null;

  return (
    <div className="space-y-1 text-xs">
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant={status === 'healthy' ? 'secondary' : status === 'unhealthy' ? 'destructive' : 'outline'}>
          {status === 'healthy'
            ? msg('platformAdmin.runtimeProfiles.healthy', 'Healthy')
            : status === 'unhealthy'
              ? msg('platformAdmin.runtimeProfiles.unhealthy', 'Unhealthy')
              : msg('platformAdmin.runtimeProfiles.unknownHealth', 'Unknown')}
        </Badge>
        {latency !== null && latency >= 0 ? (
          <span className="font-mono text-muted-foreground">{latency} ms</span>
        ) : null}
      </div>
      {message && status !== 'healthy' ? (
        <p className="max-w-[220px] truncate text-destructive" title={message}>
          {message}
        </p>
      ) : null}
    </div>
  );
}

import { useWorkspace } from '../../../shared/workspaces/context';
import clsx from 'clsx';
import {
  AlertCircle,
  Archive,
  ChevronDown,
  ChevronRight,
  Code2,
  Copy,
  Database,
  Download,
  Eye,
  FileText,
  Folder,
  FolderOpen,
  Loader2,
  MoreVertical,
  Pencil,
  Plus,
  X,
} from 'lucide-react';
import { type FormEvent, useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { Card, CardContent } from '../../../shared/ui/card';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { toast } from '../../../shared/ui/sonner';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Textarea } from '../../../shared/ui/textarea';
import { relativeTime } from '../agents/AgentsResourcePage';
import {
  archiveManagedEntity,
  archiveVaultCredential,
  createMemory,
  createVaultCredential,
  deleteManagedEntity,
  deleteMemory,
  deleteVaultCredential,
  listDeploymentRuns,
  listEnvironmentWork,
  listMemories,
  listSessionEvents,
  listSessionResources,
  listSessionThreads,
  listVaultCredentials,
  retrieveManagedEntity,
  retrieveMemory,
  updateEnvironmentDetail,
  updateManagedEntity,
  updateMemory,
  updateVaultCredential,
} from '../api';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import {
  ConfirmEntityDialog,
  DetailCard,
  DetailKV,
  DetailTableCard,
  ManagedTextArea,
  ManagedTextField,
  NestedRows,
  StatusPill,
} from '../components/common';
import { entityKindLabel } from '../labels';
import {
  type CredentialFormValues,
  type DeploymentApiResponse,
  type DeploymentRunApiResponse,
  type EnvironmentApiResponse,
  type EnvironmentWorkApiResponse,
  type ManagedEntityApiResponse,
  type ManagedEntityFormValues,
  type ManagedEntitySection,
  type MemoryApiResponse,
  type MemoryBranchState,
  type MemoryFormValues,
  type MemoryStoreApiResponse,
  type MemoryTreeNode,
  type MemoryViewMode,
  type ResourceConfig,
  type SessionApiResponse,
  type SessionResourceApiResponse,
  type SessionThreadApiResponse,
  type VaultApiResponse,
  type VaultCredentialApiResponse,
} from '../types';
import {
  compactEntityId,
  copyText,
  downloadTextFile,
  errorMessage,
  formatBytes,
  formatKilobytes,
  managedEntityDetailHref,
  managedEntityListHref,
  objectRecord,
  titleCase,
} from '../utils';
import { CredentialDialog, MemoryDialog } from './dialogs';
import {
  buildMemoryTreeNodes,
  credentialAuthLabel,
  deploymentRunStatus,
  detailRowsForEntity,
  entityDescription,
  entityDisplayName,
  entityStatusLabel,
  environmentEditValues,
  environmentPackageRows,
  initialFormValues,
  initialSelectedMemoryId,
  loadedMemoryRowsFromBranches,
  memoryBranchFromPage,
  memoryFileName,
  memoryFolderPathsFromBranches,
  memoryFolderPathsFromRows,
  memoryPreviewContent,
  normalizeMemoryFolderPath,
  removeMemoryFromBranches,
  sortMemoryRows,
  triggerLabel,
  updateMemoryQueryParam,
  upsertMemoryInBranch,
  upsertMemoryInBranches,
} from './model';

export function ManagedEntityDetailPage({
  config,
  entityId,
}: {
  config: ResourceConfig & { section: ManagedEntitySection };
  entityId: string;
}) {
  const { activeWorkspaceId } = useWorkspace();
  const [entity, setEntity] = useState<ManagedEntityApiResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [confirmAction, setConfirmAction] = useState<'archive' | 'delete' | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }
      setLoading(true);
      setLoadError(null);
      try {
        const value = await retrieveManagedEntity(config.section, entityId, activeWorkspaceId);
        if (active) {
          setEntity(value);
          setLoading(false);
        }
      } catch (error) {
        if (active) {
          setEntity(null);
          setLoadError(errorMessage(error));
          setLoading(false);
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [activeWorkspaceId, config.section, entityId, refreshKey]);

  const listHref = managedEntityListHref(activeWorkspaceId, config.section);
  const label = entity ? entityDisplayName(config.section, entity) : entityId;

  const handleArchive = async () => {
    if (!entity) {
      return;
    }
    setBusyAction('archive');
    setMutationError(null);
    try {
      const updated = await archiveManagedEntity(config.section, entity.id, activeWorkspaceId);
      setEntity(updated);
      setConfirmAction(null);
      toast.success(`${entityKindLabel(config.section)} archived`);
    } catch (error) {
      setMutationError(errorMessage(error));
      setConfirmAction(null);
    } finally {
      setBusyAction(null);
    }
  };

  const handleDelete = async () => {
    if (!entity) {
      return;
    }
    setBusyAction('delete');
    setMutationError(null);
    try {
      await deleteManagedEntity(config.section, entity.id, activeWorkspaceId);
      setConfirmAction(null);
      setBusyAction(null);
      window.location.assign(listHref);
    } catch (error) {
      setMutationError(errorMessage(error));
      setConfirmAction(null);
      setBusyAction(null);
    }
  };

  const handleSave = async (values: ManagedEntityFormValues) => {
    if (!entity) {
      return;
    }
    setMutationError(null);
    const updated = await updateManagedEntity(config.section, entity.id, values, activeWorkspaceId);
    setEntity(updated);
    setEditing(false);
    toast.success(`${entityKindLabel(config.section)} updated`);
    setRefreshKey((value) => value + 1);
  };

  if (loading) {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <ManagedDetailBreadcrumb listHref={listHref} listLabel={config.title} />
        <div className="mt-14 text-sm text-muted-foreground">Loading {entityKindLabel(config.section)}...</div>
      </section>
    );
  }

  if (!entity || loadError) {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <ManagedDetailBreadcrumb listHref={listHref} listLabel={config.title} />
        <Alert variant="destructive" className="mt-6 max-w-xl">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertDescription>{loadError || `${titleCase(entityKindLabel(config.section))} not found`}</AlertDescription>
        </Alert>
      </section>
    );
  }

  if (config.section === 'memory-stores') {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <MemoryStorePanel
          store={entity as MemoryStoreApiResponse}
          workspaceId={activeWorkspaceId}
          refreshKey={refreshKey}
          onRefresh={() => setRefreshKey((value) => value + 1)}
          variant="page"
          listHref={listHref}
        />
      </section>
    );
  }

  const archived = Boolean(entity.archived_at);

  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      {confirmAction ? (
        <ConfirmEntityDialog
          action={confirmAction}
          section={config.section}
          entity={entity}
          busy={busyAction === confirmAction}
          onCancel={() => {
            if (!busyAction) {
              setConfirmAction(null);
            }
          }}
          onConfirm={() => {
            if (confirmAction === 'archive') {
              void handleArchive();
              return;
            }
            void handleDelete();
          }}
        />
      ) : null}

      <ManagedDetailBreadcrumb listHref={listHref} listLabel={config.title} currentLabel={label} className="mb-5" />

      <header className="mb-7 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="truncate text-[28px] font-semibold leading-tight text-foreground">{label}</h1>
          <div className="mt-3 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            {config.section === 'environments' ? (
              <span className="text-foreground">Cloud</span>
            ) : (
              <StatusPill>{entityStatusLabel(entity)}</StatusPill>
            )}
            <Button
              type="button"
              variant="outline"
              size="xs"
              className="max-w-[260px] font-mono text-[13px] text-foreground"
              onClick={() => void copyText(entity.id)}
            >
              <Copy className="size-3.5" aria-hidden />
              <span className="truncate">{compactEntityId(entity.id)}</span>
            </Button>
            <span>
              {config.section === 'environments' ? 'Last updated' : 'Created'}{' '}
              {relativeTime(config.section === 'environments' ? entity.updated_at : entity.created_at)}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" size="lg" disabled={archived} onClick={() => setEditing(true)}>
            Edit
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="icon-lg"
                  aria-label="More actions"
                  disabled={Boolean(busyAction)}
                  className="text-foreground disabled:cursor-wait disabled:text-muted-foreground/70"
                />
              }
            >
              <MoreVertical className="size-4" aria-hidden />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-[154px]">
              <DropdownMenuItem
                disabled={archived || busyAction === 'archive'}
                onClick={() => setConfirmAction('archive')}
              >
                <Archive className="size-4" aria-hidden />
                Archive
              </DropdownMenuItem>
              {config.section !== 'deployments' ? (
                <DropdownMenuItem
                  variant="destructive"
                  disabled={busyAction === 'delete'}
                  onClick={() => setConfirmAction('delete')}
                >
                  <X className="size-4" aria-hidden />
                  Delete
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      {mutationError ? (
        <Alert variant="destructive" className="mb-4 max-w-xl">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertDescription>{mutationError}</AlertDescription>
        </Alert>
      ) : null}

      {editing ? (
        <ManagedEntityInlineEditor
          section={config.section}
          entity={entity}
          workspaceId={activeWorkspaceId}
          onCancel={() => setEditing(false)}
          onSaved={(updated) => {
            setEntity(updated);
            setEditing(false);
            toast.success(`${entityKindLabel(config.section)} updated`);
            setRefreshKey((value) => value + 1);
          }}
          onSubmit={handleSave}
        />
      ) : (
        <>
          <ManagedEntityOverview section={config.section} entity={entity} />
          <ManagedEntityNestedPanel
            section={config.section}
            entity={entity}
            workspaceId={activeWorkspaceId}
            refreshKey={refreshKey}
            onRefresh={() => setRefreshKey((value) => value + 1)}
          />
        </>
      )}
    </section>
  );
}

export function ManagedEntityOverview({
  section,
  entity,
}: {
  section: ManagedEntitySection;
  entity: ManagedEntityApiResponse;
}) {
  const rows = detailRowsForEntity(section, entity);
  return (
    <DetailCard title="Overview" description={entityDescription(entity) || undefined}>
      <Card size="sm" className="gap-0 py-0">
        <CardContent className="px-0">
          <dl className="grid gap-px bg-border md:grid-cols-2">
            {rows.map((row) => (
              <div key={row.label} className="bg-card px-4 py-3">
                <dt className="text-xs font-medium uppercase text-muted-foreground/70">{row.label}</dt>
                <dd className="mt-1 min-h-5 truncate text-sm text-foreground">{row.value}</dd>
              </div>
            ))}
          </dl>
        </CardContent>
      </Card>
      {section === 'environments' ? <EnvironmentReadOnlySections entity={entity as EnvironmentApiResponse} /> : null}
    </DetailCard>
  );
}

export function EnvironmentReadOnlySections({ entity }: { entity: EnvironmentApiResponse }) {
  const config = objectRecord(entity.config);
  const networking = objectRecord(config.networking);
  const packages = environmentPackageRows(config.packages);
  const metadata = objectRecord((entity as EnvironmentApiResponse & { metadata?: unknown }).metadata);
  return (
    <div className="mt-7 space-y-7">
      <div>
        <h2 className="text-[20px] font-semibold text-foreground">Networking</h2>
        <p className="mt-1 text-sm text-muted-foreground">Configure network access policies for this environment.</p>
        <DetailKV label="Type" value={titleCase(String(networking.type || 'unrestricted'))} />
      </div>
      <div className="border-t border-border pt-7">
        <h2 className="text-[20px] font-semibold text-foreground">Packages</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Specify packages and their versions available in this environment. Separate multiple values with spaces.
        </p>
        {packages.length ? (
          <div className="mt-3 text-sm text-foreground">
            {packages.map((row) => `${row.manager}: ${row.value}`).join('  ')}
          </div>
        ) : (
          <div className="mt-3 text-sm text-muted-foreground/70">No packages configured</div>
        )}
      </div>
      <div className="border-t border-border pt-7">
        <h2 className="text-[20px] font-semibold text-foreground">Metadata</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Add custom key-value pairs to tag and organize this environment. Keys must be lowercase.
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
          <div className="mt-3 text-sm text-muted-foreground/70">No metadata</div>
        )}
      </div>
    </div>
  );
}

export function ManagedEntityInlineEditor({
  section,
  entity,
  workspaceId,
  onCancel,
  onSaved,
  onSubmit,
}: {
  section: ManagedEntitySection;
  entity: ManagedEntityApiResponse;
  workspaceId: string;
  onCancel: () => void;
  onSaved: (entity: ManagedEntityApiResponse) => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
}) {
  if (section === 'environments') {
    return (
      <EnvironmentInlineEditor
        entity={entity as EnvironmentApiResponse}
        workspaceId={workspaceId}
        onCancel={onCancel}
        onSaved={onSaved}
      />
    );
  }

  return <GenericManagedEntityInlineEditor section={section} entity={entity} onCancel={onCancel} onSubmit={onSubmit} />;
}

export function GenericManagedEntityInlineEditor({
  section,
  entity,
  onCancel,
  onSubmit,
}: {
  section: ManagedEntitySection;
  entity: ManagedEntityApiResponse;
  onCancel: () => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
}) {
  const [values, setValues] = useState<ManagedEntityFormValues>(() => initialFormValues(section, entity));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasDescription = section === 'deployments' || section === 'memory-stores';

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!values.name.trim()) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(values);
    } catch (submitError) {
      setError(errorMessage(submitError));
      setSubmitting(false);
    }
  };

  return (
    <DetailCard title={`Edit ${entityKindLabel(section)}`}>
      <form className="max-w-[760px] space-y-4" onSubmit={submit}>
        <ManagedTextField
          label={section === 'sessions' ? 'Title' : 'Name'}
          value={values.name}
          onChange={(name) => setValues((current) => ({ ...current, name }))}
          autoFocus
        />
        {hasDescription ? (
          <ManagedTextArea
            label="Description"
            value={values.description}
            onChange={(description) => setValues((current) => ({ ...current, description }))}
          />
        ) : null}
        {section === 'sessions' || section === 'deployments' ? (
          <div className="grid gap-4 md:grid-cols-2">
            <ManagedTextField
              label="Agent"
              value={values.agentId}
              onChange={(agentId) => setValues((current) => ({ ...current, agentId }))}
            />
            <ManagedTextField
              label="Environment"
              value={values.environmentId}
              onChange={(environmentId) => setValues((current) => ({ ...current, environmentId }))}
            />
          </div>
        ) : null}
        {error ? <p className="text-sm text-destructive">{error}</p> : null}
        <div className="flex gap-2">
          <Button type="submit" size="lg" disabled={submitting || !values.name.trim()}>
            {submitting ? 'Saving...' : 'Save changes'}
          </Button>
          <Button type="button" variant="secondary" size="lg" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </form>
    </DetailCard>
  );
}

export function EnvironmentInlineEditor({
  entity,
  workspaceId,
  onCancel,
  onSaved,
}: {
  entity: EnvironmentApiResponse;
  workspaceId: string;
  onCancel: () => void;
  onSaved: (entity: ManagedEntityApiResponse) => void;
}) {
  const [values, setValues] = useState(() => environmentEditValues(entity));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!values.name.trim()) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const updated = await updateEnvironmentDetail(entity.id, values, workspaceId);
      onSaved(updated);
    } catch (submitError) {
      setError(errorMessage(submitError));
      setSubmitting(false);
    }
  };

  const addPackage = () =>
    setValues((current) => ({ ...current, packages: [...current.packages, { manager: 'pip', value: '' }] }));
  const addMetadata = () =>
    setValues((current) => ({ ...current, metadataRows: [...current.metadataRows, { key: '', value: '' }] }));

  return (
    <form className="max-w-[820px] space-y-7" onSubmit={submit}>
      <ManagedTextField
        label="Environment name"
        value={values.name}
        onChange={(name) => setValues((current) => ({ ...current, name }))}
        autoFocus
      />
      <ManagedTextArea
        label="Description"
        value={values.description}
        onChange={(description) => setValues((current) => ({ ...current, description }))}
      />
      <DetailCard title="Networking" description="Configure network access policies for this environment.">
        <Card size="sm" className="py-0">
          <CardContent className="p-3">
            <Field className="gap-2">
              <FieldLabel>Type</FieldLabel>
              <Select<string>
                value={values.networkType}
                items={[
                  { value: 'unrestricted', label: 'Unrestricted' },
                  { value: 'limited', label: 'Limited' },
                ]}
                onValueChange={(networkType) => {
                  if (networkType === null) {
                    return;
                  }
                  setValues((current) => ({
                    ...current,
                    networkType: networkType === 'limited' ? 'limited' : 'unrestricted',
                  }));
                }}
              >
                <SelectTrigger aria-label="Type" className="h-10 w-full px-3 text-sm text-foreground">
                  <SelectValue>{values.networkType === 'limited' ? 'Limited' : 'Unrestricted'}</SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectItem value="unrestricted" label="Unrestricted">
                    Unrestricted
                  </SelectItem>
                  <SelectItem value="limited" label="Limited">
                    Limited
                  </SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </CardContent>
        </Card>
      </DetailCard>
      <DetailCard
        title="Packages"
        description="Specify packages and their versions available in this environment. Separate multiple values with spaces."
      >
        <Card size="sm" className="py-0">
          <CardContent className="space-y-3 p-3">
            <div className="space-y-2">
              {values.packages.map((row, index) => (
                <div key={`${row.manager}-${index}`} className="grid gap-2 md:grid-cols-[160px_1fr_40px]">
                  <Select<string>
                    value={row.manager}
                    items={['apt', 'cargo', 'gem', 'go', 'npm', 'pip'].map((manager) => ({
                      value: manager,
                      label: manager,
                    }))}
                    onValueChange={(manager) => {
                      if (manager === null) {
                        return;
                      }
                      setValues((current) => ({
                        ...current,
                        packages: current.packages.map((item, itemIndex) =>
                          itemIndex === index ? { ...item, manager } : item,
                        ),
                      }));
                    }}
                  >
                    <SelectTrigger aria-label="Manager" className="h-10 w-full px-3 text-sm text-foreground">
                      <SelectValue>{row.manager}</SelectValue>
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      {['apt', 'cargo', 'gem', 'go', 'npm', 'pip'].map((manager) => (
                        <SelectItem key={manager} value={manager} label={manager}>
                          {manager}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input
                    aria-label="package package==1.0.0"
                    value={row.value}
                    placeholder="package==1.0.0"
                    className="h-10"
                    onChange={(event) =>
                      setValues((current) => ({
                        ...current,
                        packages: current.packages.map((item, itemIndex) =>
                          itemIndex === index ? { ...item, value: event.target.value } : item,
                        ),
                      }))
                    }
                  />
                  <Button
                    type="button"
                    aria-label="Remove package"
                    disabled={values.packages.length <= 1}
                    variant="secondary"
                    size="icon-lg"
                    className="size-10"
                    onClick={() =>
                      setValues((current) => ({
                        ...current,
                        packages: current.packages.filter((_, itemIndex) => itemIndex !== index),
                      }))
                    }
                  >
                    <X className="size-4" aria-hidden />
                  </Button>
                </div>
              ))}
            </div>
            <Button type="button" variant="secondary" size="lg" onClick={addPackage}>
              <Plus className="size-4" aria-hidden />
              Add package
            </Button>
          </CardContent>
        </Card>
      </DetailCard>
      <DetailCard
        title="Metadata"
        description="Add custom key-value pairs to tag and organize this environment. Keys must be lowercase."
      >
        <Card size="sm" className="py-0">
          <CardContent className="space-y-3 p-3">
            <div className="space-y-2">
              {values.metadataRows.map((row, index) => (
                <div key={`${row.key}-${index}`} className="grid gap-2 md:grid-cols-[1fr_1fr_40px]">
                  <Input
                    aria-label={`Metadata key ${index + 1}`}
                    value={row.key}
                    placeholder="key"
                    className="h-10"
                    onChange={(event) =>
                      setValues((current) => ({
                        ...current,
                        metadataRows: current.metadataRows.map((item, itemIndex) =>
                          itemIndex === index ? { ...item, key: event.target.value } : item,
                        ),
                      }))
                    }
                  />
                  <Input
                    aria-label={`Metadata value ${index + 1}`}
                    value={row.value}
                    placeholder="value"
                    className="h-10"
                    onChange={(event) =>
                      setValues((current) => ({
                        ...current,
                        metadataRows: current.metadataRows.map((item, itemIndex) =>
                          itemIndex === index ? { ...item, value: event.target.value } : item,
                        ),
                      }))
                    }
                  />
                  <Button
                    type="button"
                    aria-label={`Remove metadata row ${index + 1}`}
                    disabled={values.metadataRows.length <= 1}
                    variant="secondary"
                    size="icon-lg"
                    className="size-10"
                    onClick={() =>
                      setValues((current) => ({
                        ...current,
                        metadataRows: current.metadataRows.filter((_, itemIndex) => itemIndex !== index),
                      }))
                    }
                  >
                    <X className="size-4" aria-hidden />
                  </Button>
                </div>
              ))}
            </div>
            <Button type="button" variant="secondary" size="lg" onClick={addMetadata}>
              <Plus className="size-4" aria-hidden />
              Add metadata entry
            </Button>
          </CardContent>
        </Card>
      </DetailCard>
      {error ? <p className="text-sm text-destructive">{error}</p> : null}
      <div className="flex gap-2">
        <Button type="submit" size="lg" disabled={submitting || !values.name.trim()}>
          {submitting ? 'Saving...' : 'Save changes'}
        </Button>
        <Button type="button" variant="secondary" size="lg" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

export function ManagedEntityNestedPanel({
  section,
  entity,
  workspaceId,
  refreshKey,
  onRefresh,
}: {
  section: ManagedEntitySection;
  entity: ManagedEntityApiResponse;
  workspaceId: string;
  refreshKey: number;
  onRefresh: () => void;
}) {
  if (section === 'credential-vaults') {
    return (
      <VaultCredentialsPanel
        vault={entity as VaultApiResponse}
        workspaceId={workspaceId}
        refreshKey={refreshKey}
        onRefresh={onRefresh}
      />
    );
  }
  if (section === 'memory-stores') {
    return (
      <MemoryStorePanel
        store={entity as MemoryStoreApiResponse}
        workspaceId={workspaceId}
        refreshKey={refreshKey}
        onRefresh={onRefresh}
      />
    );
  }
  if (section === 'deployments') {
    return (
      <DeploymentRunsPanel
        deployment={entity as DeploymentApiResponse}
        workspaceId={workspaceId}
        refreshKey={refreshKey}
      />
    );
  }
  if (section === 'environments') {
    return (
      <EnvironmentWorkPanel
        environment={entity as EnvironmentApiResponse}
        workspaceId={workspaceId}
        refreshKey={refreshKey}
      />
    );
  }
  return (
    <SessionNestedPanel session={entity as SessionApiResponse} workspaceId={workspaceId} refreshKey={refreshKey} />
  );
}

export function DeploymentRunsPanel({
  deployment,
  workspaceId,
  refreshKey,
}: {
  deployment: DeploymentApiResponse;
  workspaceId: string;
  refreshKey: number;
}) {
  const [state, setState] = useState<{ loading: boolean; error: string | null; data: DeploymentRunApiResponse[] }>({
    loading: true,
    error: null,
    data: [],
  });
  useEffect(() => {
    let active = true;
    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }
      setState((current) => ({ ...current, loading: true, error: null }));
      try {
        const page = await listDeploymentRuns(deployment.id, workspaceId);
        if (active) {
          setState({ loading: false, error: null, data: [...(page.data ?? [])].reverse() });
        }
      } catch (error) {
        if (active) {
          setState({ loading: false, error: errorMessage(error), data: [] });
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [deployment.id, refreshKey, workspaceId]);

  return (
    <DetailTableCard
      title="Deployment runs"
      description="Review manual and scheduled runs for this deployment."
      loading={state.loading}
      error={state.error}
      emptyTitle="No deployment runs yet"
      columns={['ID', 'Status', 'Trigger', 'Session', 'Created']}
      rows={state.data.map((run) => [
        compactEntityId(run.id),
        deploymentRunStatus(run),
        run.trigger_type || triggerLabel(run.trigger),
        run.session_id ? (
          <a
            className="font-sans text-[13px] text-foreground underline-offset-2 hover:underline"
            href={`${managedEntityDetailHref(workspaceId, 'sessions', run.session_id)}?from_deployment=${encodeURIComponent(deployment.id)}&from_run=${encodeURIComponent(run.id)}`}
            target="_blank"
            rel="noreferrer"
          >
            {compactEntityId(run.session_id)}
          </a>
        ) : (
          '—'
        ),
        relativeTime(run.created_at),
      ])}
    />
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
  const [state, setState] = useState<{ loading: boolean; error: string | null; data: EnvironmentWorkApiResponse[] }>({
    loading: true,
    error: null,
    data: [],
  });
  useEffect(() => {
    let active = true;
    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }
      setState((current) => ({ ...current, loading: true, error: null }));
      try {
        const page = await listEnvironmentWork(environment.id, workspaceId);
        if (active) {
          setState({ loading: false, error: null, data: page.data ?? [] });
        }
      } catch (error) {
        if (active) {
          setState({ loading: false, error: errorMessage(error), data: [] });
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [environment.id, refreshKey, workspaceId]);

  return (
    <DetailTableCard
      title="Work queue"
      description="Inspect pending or claimed work for this environment."
      loading={state.loading}
      error={state.error}
      emptyTitle="No work queued"
      columns={['ID', 'Status', 'Created', 'Updated']}
      rows={state.data.map((work) => [
        compactEntityId(work.id),
        titleCase(work.status || 'queued'),
        relativeTime(work.created_at),
        work.updated_at ? relativeTime(work.updated_at) : '—',
      ])}
    />
  );
}

export function SessionNestedPanel({
  session,
  workspaceId,
  refreshKey,
}: {
  session: SessionApiResponse;
  workspaceId: string;
  refreshKey: number;
}) {
  const [resources, setResources] = useState<{
    loading: boolean;
    error: string | null;
    data: SessionResourceApiResponse[];
  }>({ loading: true, error: null, data: [] });
  const [threads, setThreads] = useState<{ loading: boolean; error: string | null; data: SessionThreadApiResponse[] }>({
    loading: true,
    error: null,
    data: [],
  });
  const [events, setEvents] = useState<{ loading: boolean; error: string | null; data: Record<string, unknown>[] }>({
    loading: true,
    error: null,
    data: [],
  });

  useEffect(() => {
    let active = true;
    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }
      setResources((current) => ({ ...current, loading: true, error: null }));
      setThreads((current) => ({ ...current, loading: true, error: null }));
      setEvents((current) => ({ ...current, loading: true, error: null }));
      void listSessionResources(session.id, workspaceId)
        .then((page) => active && setResources({ loading: false, error: null, data: page.data ?? [] }))
        .catch((error) => active && setResources({ loading: false, error: errorMessage(error), data: [] }));
      void listSessionThreads(session.id, workspaceId)
        .then((page) => active && setThreads({ loading: false, error: null, data: page.data ?? [] }))
        .catch((error) => active && setThreads({ loading: false, error: errorMessage(error), data: [] }));
      void listSessionEvents(session.id, workspaceId)
        .then((page) => active && setEvents({ loading: false, error: null, data: page.data ?? [] }))
        .catch((error) => active && setEvents({ loading: false, error: errorMessage(error), data: [] }));
    })();
    return () => {
      active = false;
    };
  }, [refreshKey, session.id, workspaceId]);

  return (
    <div className="space-y-6">
      <DetailTableCard
        title="Resources"
        description="Mounted files, repositories, and memory stores for this session."
        loading={resources.loading}
        error={resources.error}
        emptyTitle="No resources mounted"
        columns={['ID', 'Type', 'Created']}
        rows={resources.data.map((resource) => [
          String(resource.id || '—'),
          String(resource.type || resource.resource_type || 'resource'),
          typeof resource.created_at === 'string' ? relativeTime(resource.created_at) : '—',
        ])}
      />
      <DetailTableCard
        title="Threads"
        description="Conversation threads created inside this session."
        loading={threads.loading}
        error={threads.error}
        emptyTitle="No threads yet"
        columns={['ID', 'Status', 'Created']}
        rows={threads.data.map((thread) => [
          compactEntityId(thread.id),
          thread.archived_at ? 'Archived' : 'Active',
          relativeTime(thread.created_at),
        ])}
      />
      <DetailTableCard
        title="Events"
        description="Recent session events."
        loading={events.loading}
        error={events.error}
        emptyTitle="No events yet"
        columns={['Type', 'Created', 'Payload']}
        rows={events.data.map((event) => [
          String(event.type || 'event'),
          typeof event.created_at === 'string' ? relativeTime(event.created_at) : '—',
          JSON.stringify(event).slice(0, 90),
        ])}
      />
    </div>
  );
}

export function VaultCredentialsPanel({
  vault,
  workspaceId,
  refreshKey,
  onRefresh,
}: {
  vault: VaultApiResponse;
  workspaceId: string;
  refreshKey: number;
  onRefresh: () => void;
}) {
  const { msg } = useI18n();
  const [state, setState] = useState<{ loading: boolean; error: string | null; data: VaultCredentialApiResponse[] }>({
    loading: true,
    error: null,
    data: [],
  });
  const [dialog, setDialog] = useState<{ mode: 'create' | 'edit'; credential?: VaultCredentialApiResponse } | null>(
    null,
  );
  const [confirmAction, setConfirmAction] = useState<{
    action: 'archive' | 'delete';
    credential: VaultCredentialApiResponse;
  } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setState((current) => ({ ...current, loading: true, error: null }));
    void listVaultCredentials(vault.id, workspaceId)
      .then((page) => active && setState({ loading: false, error: null, data: page.data ?? [] }))
      .catch((error) => active && setState({ loading: false, error: errorMessage(error), data: [] }));
    return () => {
      active = false;
    };
  }, [refreshKey, vault.id, workspaceId]);

  const submit = async (values: CredentialFormValues, credential?: VaultCredentialApiResponse) => {
    const updated = credential
      ? await updateVaultCredential(vault.id, credential.id, values, workspaceId)
      : await createVaultCredential(vault.id, values, workspaceId);
    setState((current) => ({ ...current, data: [updated, ...current.data.filter((item) => item.id !== updated.id)] }));
    setDialog(null);
    onRefresh();
  };

  const remove = async (credential: VaultCredentialApiResponse, action: 'archive' | 'delete') => {
    setBusyId(credential.id);
    try {
      if (action === 'archive') {
        await archiveVaultCredential(vault.id, credential.id, workspaceId);
      } else {
        await deleteVaultCredential(vault.id, credential.id, workspaceId);
      }
      setState((current) => ({ ...current, data: current.data.filter((item) => item.id !== credential.id) }));
      setConfirmAction(null);
      onRefresh();
    } catch (error) {
      setState((current) => ({ ...current, error: errorMessage(error) }));
      setConfirmAction(null);
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      {confirmAction ? (
        <ConfirmEntityDialog
          action={confirmAction.action}
          section="credential-vaults"
          entity={confirmAction.credential}
          labelOverride={msg('managedAgents.credentialVaults.credentialKind', 'credential')}
          busy={busyId === confirmAction.credential.id}
          onCancel={() => {
            if (!busyId) {
              setConfirmAction(null);
            }
          }}
          onConfirm={() => {
            void remove(confirmAction.credential, confirmAction.action);
          }}
        />
      ) : null}
      <DetailCard
        title="Credentials"
        description="Credentials available to agents that attach this vault."
        action={
          <Button type="button" size="lg" onClick={() => setDialog({ mode: 'create' })}>
            <Plus className="size-4" aria-hidden />
            Add credential
          </Button>
        }
      >
        <NestedRows
          loading={state.loading}
          error={state.error}
          emptyTitle="No credentials yet"
          columns={['ID', 'Name', 'Auth', 'Created', 'Actions']}
          rows={state.data.map((credential) => [
            compactEntityId(credential.id),
            credential.display_name,
            credentialAuthLabel(credential.auth),
            relativeTime(credential.created_at),
            <div key={credential.id} className="flex justify-end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      type="button"
                      variant="outline"
                      size="icon-sm"
                      aria-label="More actions"
                      className="text-foreground"
                      disabled={busyId === credential.id}
                    />
                  }
                >
                  <MoreVertical className="size-4" aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-[164px]">
                  <DropdownMenuItem className="h-9" onClick={() => setDialog({ mode: 'edit', credential })}>
                    <Pencil className="size-4" aria-hidden />
                    Edit
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="h-9"
                    disabled={busyId === credential.id}
                    onClick={() => setConfirmAction({ action: 'archive', credential })}
                  >
                    <Archive className="size-4" aria-hidden />
                    Archive
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="h-9"
                    variant="destructive"
                    disabled={busyId === credential.id}
                    onClick={() => setConfirmAction({ action: 'delete', credential })}
                  >
                    <X className="size-4" aria-hidden />
                    Delete
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>,
          ])}
        />
      </DetailCard>
      {dialog ? (
        <CredentialDialog
          credential={dialog.credential}
          onClose={() => setDialog(null)}
          onSubmit={(values) => submit(values, dialog.credential)}
        />
      ) : null}
    </>
  );
}

export function MemoryStorePanel({
  store,
  workspaceId,
  refreshKey,
  onRefresh,
  variant = 'nested',
  listHref,
}: {
  store: MemoryStoreApiResponse;
  workspaceId: string;
  refreshKey: number;
  onRefresh: () => void;
  variant?: 'nested' | 'page';
  listHref?: string;
}) {
  const { msg } = useI18n();
  const [memories, setMemories] = useState<MemoryBranchState>({ loading: true, error: null, data: [], prefixes: [] });
  const [folderBranches, setFolderBranches] = useState<Record<string, MemoryBranchState>>({});
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(() => new Set());
  const [treeBusy, setTreeBusy] = useState(false);
  const [selectedMemoryId, setSelectedMemoryId] = useState(() => initialSelectedMemoryId());
  const [viewMode, setViewMode] = useState<MemoryViewMode>('preview');
  const [dialog, setDialog] = useState<{ mode: 'create' | 'edit'; memory?: MemoryApiResponse } | null>(null);
  const [confirmAction, setConfirmAction] = useState<{ action: 'delete'; memory: MemoryApiResponse } | null>(null);
  const [fullMemory, setFullMemory] = useState<{
    loading: boolean;
    error: string | null;
    data: MemoryApiResponse | null;
  }>({ loading: false, error: null, data: null });
  const [editingContent, setEditingContent] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setMemories((current) => ({ ...current, loading: true, error: null }));
    setFolderBranches({});
    setExpandedFolders(new Set());
    void listMemories(store.id, workspaceId)
      .then((page) => active && setMemories(memoryBranchFromPage(page)))
      .catch((error) => active && setMemories({ loading: false, error: errorMessage(error), data: [], prefixes: [] }));
    return () => {
      active = false;
    };
  }, [refreshKey, store.id, workspaceId]);

  const memoryRows = useMemo(() => sortMemoryRows(memories.data), [memories.data]);
  const rootSelectableMemories = useMemo(() => memoryRows.filter((memory) => memory.type === 'memory'), [memoryRows]);
  const loadedMemoryRows = useMemo(
    () => loadedMemoryRowsFromBranches(memoryRows, folderBranches),
    [folderBranches, memoryRows],
  );
  const folderPaths = useMemo(
    () => memoryFolderPathsFromBranches(memoryRows, folderBranches),
    [folderBranches, memoryRows],
  );
  const allFoldersExpanded = folderPaths.length > 0 && folderPaths.every((path) => expandedFolders.has(path));
  const treeNodes = useMemo(
    () => buildMemoryTreeNodes(memoryRows, expandedFolders, folderBranches),
    [expandedFolders, folderBranches, memoryRows],
  );
  const selectedMemoryFromTree = selectedMemoryId
    ? (loadedMemoryRows.find((memory) => memory.id === selectedMemoryId) ?? null)
    : (rootSelectableMemories[0] ?? null);
  const selectedMemorySummary =
    selectedMemoryFromTree ?? (selectedMemoryId && fullMemory.data?.id === selectedMemoryId ? fullMemory.data : null);
  const selectedMemory =
    selectedMemorySummary && fullMemory.data?.id === selectedMemorySummary.id
      ? { ...selectedMemorySummary, ...fullMemory.data }
      : selectedMemorySummary;

  useEffect(() => {
    if (memories.loading || selectedMemoryId || !rootSelectableMemories.length) {
      return;
    }
    setSelectedMemoryId(rootSelectableMemories[0].id);
  }, [memories.loading, rootSelectableMemories, selectedMemoryId]);

  useEffect(() => {
    setEditingContent(null);
  }, [selectedMemorySummary?.id]);

  useEffect(() => {
    const memoryId = selectedMemoryId ?? selectedMemoryFromTree?.id ?? null;
    if (!memoryId) {
      setFullMemory({ loading: false, error: null, data: null });
      return;
    }
    if (selectedMemoryFromTree?.id === memoryId && typeof selectedMemoryFromTree.content === 'string') {
      setFullMemory({ loading: false, error: null, data: selectedMemoryFromTree });
      return;
    }
    let active = true;
    setFullMemory({ loading: true, error: null, data: null });
    void retrieveMemory(store.id, memoryId, workspaceId)
      .then((memory) => active && setFullMemory({ loading: false, error: null, data: memory }))
      .catch((error) => active && setFullMemory({ loading: false, error: errorMessage(error), data: null }));
    return () => {
      active = false;
    };
  }, [selectedMemoryFromTree?.content, selectedMemoryFromTree?.id, selectedMemoryId, store.id, workspaceId]);

  const loadFolderBranch = async (folderPath: string) => {
    const normalizedPath = normalizeMemoryFolderPath(folderPath);
    setFolderBranches((current) => ({
      ...current,
      [normalizedPath]: { ...(current[normalizedPath] ?? { data: [], prefixes: [] }), loading: true, error: null },
    }));
    try {
      const page = await listMemories(store.id, workspaceId, normalizedPath);
      const branch = memoryBranchFromPage(page);
      setFolderBranches((current) => ({ ...current, [normalizedPath]: branch }));
      return branch;
    } catch (error) {
      const branch = { loading: false, error: errorMessage(error), data: [], prefixes: [] };
      setFolderBranches((current) => ({ ...current, [normalizedPath]: branch }));
      return branch;
    }
  };

  const toggleFolder = (folderPath: string) => {
    const normalizedPath = normalizeMemoryFolderPath(folderPath);
    if (expandedFolders.has(normalizedPath)) {
      setExpandedFolders((current) => {
        const next = new Set(current);
        next.delete(normalizedPath);
        return next;
      });
      return;
    }
    setExpandedFolders((current) => new Set(current).add(normalizedPath));
    if (!folderBranches[normalizedPath] || folderBranches[normalizedPath].error) {
      void loadFolderBranch(normalizedPath);
    }
  };

  const toggleAllFolders = async () => {
    if (!folderPaths.length || treeBusy) {
      return;
    }
    if (allFoldersExpanded) {
      setExpandedFolders(new Set());
      return;
    }
    setTreeBusy(true);
    const visited = new Set<string>();
    const nextExpanded = new Set(expandedFolders);
    const branchCache = new Map(Object.entries(folderBranches));
    const queue = memoryFolderPathsFromRows(memoryRows);
    try {
      while (queue.length && visited.size < 100) {
        const folderPath = queue.shift();
        if (!folderPath || visited.has(folderPath)) {
          continue;
        }
        visited.add(folderPath);
        nextExpanded.add(folderPath);
        setExpandedFolders(new Set(nextExpanded));
        let branch = branchCache.get(folderPath);
        if (!branch || branch.error) {
          branch = await loadFolderBranch(folderPath);
          branchCache.set(folderPath, branch);
        }
        for (const childPath of memoryFolderPathsFromRows(branch.data)) {
          if (!visited.has(childPath)) {
            queue.push(childPath);
          }
        }
      }
      setExpandedFolders(nextExpanded);
    } finally {
      setTreeBusy(false);
    }
  };

  const submit = async (values: MemoryFormValues, memory?: MemoryApiResponse) => {
    const isUpdate = Boolean(memory);
    const updated = memory
      ? await updateMemory(store.id, memory.id, values, workspaceId, memory.content_sha256)
      : await createMemory(store.id, values, workspaceId);
    setMemories((current) => upsertMemoryInBranch(current, updated, '/'));
    setFolderBranches((current) => upsertMemoryInBranches(current, updated));
    setFullMemory({ loading: false, error: null, data: updated });
    setSelectedMemoryId(updated.id);
    updateMemoryQueryParam(updated.id);
    setDialog(null);
    setEditingContent(null);
    if (!isUpdate) {
      onRefresh();
    }
  };

  const saveInlineEdit = async () => {
    if (!selectedMemory || editingContent === null) {
      return;
    }
    setBusyId(selectedMemory.id);
    try {
      await submit({ path: selectedMemory.path, content: editingContent }, selectedMemory);
    } catch (error) {
      setMemories((current) => ({ ...current, error: errorMessage(error) }));
    } finally {
      setBusyId(null);
    }
  };

  const downloadSelectedMemory = async () => {
    if (!selectedMemory) {
      return;
    }
    setBusyId(selectedMemory.id);
    try {
      const memory =
        typeof selectedMemory.content === 'string'
          ? selectedMemory
          : await retrieveMemory(store.id, selectedMemory.id, workspaceId);
      setFullMemory({ loading: false, error: null, data: memory });
      downloadTextFile(memoryFileName(memory.path), memory.content ?? '');
    } catch (error) {
      setMemories((current) => ({ ...current, error: errorMessage(error) }));
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (memory: MemoryApiResponse) => {
    setBusyId(memory.id);
    try {
      await deleteMemory(store.id, memory.id, workspaceId);
      setMemories((current) => ({ ...current, data: current.data.filter((item) => item.id !== memory.id) }));
      setFolderBranches((current) => removeMemoryFromBranches(current, memory.id));
      setSelectedMemoryId((current) => (current === memory.id ? null : current));
      updateMemoryQueryParam(null);
      setConfirmAction(null);
      onRefresh();
    } catch (error) {
      setMemories((current) => ({ ...current, error: errorMessage(error) }));
      setConfirmAction(null);
    } finally {
      setBusyId(null);
    }
  };

  const addMemoryButton = (
    <Button type="button" size="lg" onClick={() => setDialog({ mode: 'create' })}>
      <Plus className="size-4" aria-hidden />
      Add memory
    </Button>
  );

  const storeCopyButton = (
    <Button
      type="button"
      variant="outline"
      size="xs"
      className="max-w-[260px] font-mono text-[13px] text-foreground"
      aria-label={`Copy ${store.id}`}
      onClick={() => void copyText(store.id)}
    >
      <Copy className="size-3.5" aria-hidden />
      <span className="truncate">{compactEntityId(store.id)}</span>
    </Button>
  );

  return (
    <>
      {confirmAction ? (
        <ConfirmEntityDialog
          action={confirmAction.action}
          section="memory-stores"
          entity={confirmAction.memory}
          labelOverride={msg('managedAgents.memoryStores.memoryKind', 'memory')}
          busy={busyId === confirmAction.memory.id}
          onCancel={() => {
            if (!busyId) {
              setConfirmAction(null);
            }
          }}
          onConfirm={() => {
            void remove(confirmAction.memory);
          }}
        />
      ) : null}
      {variant === 'page' ? (
        <>
          <ManagedDetailBreadcrumb
            listHref={listHref ?? managedEntityListHref(workspaceId, 'memory-stores')}
            listLabel={msg('managedAgents.memoryStores.title', 'Memory stores')}
            currentLabel={store.name || store.id}
            className="mb-5"
          />

          <header className="mb-4 flex flex-wrap items-start justify-between gap-4">
            <div className="min-w-0">
              <h1 className="truncate text-[28px] font-semibold leading-tight text-foreground">
                {store.name || store.id}
              </h1>
              <div className="mt-3">
                <StatusPill>{entityStatusLabel(store)}</StatusPill>
              </div>
            </div>
            {addMemoryButton}
          </header>

          {store.description ? (
            <p className="mb-3 max-w-[760px] text-sm leading-5 text-muted-foreground">{store.description}</p>
          ) : null}
          <div className="mb-6 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            {storeCopyButton}
            <span>Created {relativeTime(store.created_at)}</span>
          </div>
        </>
      ) : (
        <section className="mb-7">
          <div className="mb-4 flex flex-wrap items-start justify-between gap-4">
            <div className="min-w-0">
              {store.description ? (
                <p className="max-w-[760px] text-sm leading-5 text-muted-foreground">{store.description}</p>
              ) : null}
              <div className="mt-3 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
                {storeCopyButton}
                <span>Created {relativeTime(store.created_at)}</span>
              </div>
            </div>
            {addMemoryButton}
          </div>
        </section>
      )}

      {memories.error ? (
        <Alert variant="destructive" className="mb-4 max-w-xl">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertDescription>{memories.error}</AlertDescription>
        </Alert>
      ) : null}

      <Card className="overflow-hidden py-0">
        <CardContent className="grid min-h-[520px] p-0 lg:grid-cols-[280px_minmax(0,1fr)]">
          <aside className="border-b border-border bg-card-raised lg:border-b-0 lg:border-r">
            <div className="flex h-12 items-center justify-between gap-3 border-b border-border px-4 text-sm font-semibold text-foreground">
              <span>Memories</span>
              {folderPaths.length ? (
                <Button
                  type="button"
                  aria-label={allFoldersExpanded ? 'Collapse all' : 'Expand all'}
                  disabled={treeBusy}
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground hover:bg-secondary hover:text-foreground disabled:cursor-wait disabled:text-muted-foreground/70"
                  onClick={() => void toggleAllFolders()}
                >
                  {allFoldersExpanded ? (
                    <ChevronDown className="size-4" aria-hidden />
                  ) : (
                    <ChevronRight className="size-4" aria-hidden />
                  )}
                </Button>
              ) : null}
            </div>
            {memories.loading ? (
              <div className="px-4 py-8 text-sm text-muted-foreground">Loading memories...</div>
            ) : treeNodes.length ? (
              <div className="max-h-[560px] overflow-auto p-2">
                {treeNodes.map((node) =>
                  node.type === 'folder' ? (
                    <MemoryTreeFolderButton
                      key={`folder:${node.path}`}
                      node={node}
                      onToggle={() => toggleFolder(node.path)}
                    />
                  ) : (
                    <MemoryTreeMemoryButton
                      key={`memory:${node.memory.id}`}
                      node={node}
                      selected={selectedMemory?.id === node.memory.id}
                      onSelect={() => {
                        setSelectedMemoryId(node.memory.id);
                        updateMemoryQueryParam(node.memory.id);
                      }}
                    />
                  ),
                )}
              </div>
            ) : (
              <div className="px-4 py-10 text-center text-sm text-muted-foreground">No memories yet</div>
            )}
          </aside>

          <div className="min-w-0 bg-card">
            {selectedMemory ? (
              editingContent !== null ? (
                <>
                  <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border px-5 py-4">
                    <div className="min-w-0">
                      <h2 className="truncate text-[22px] font-semibold leading-7 text-foreground">
                        {selectedMemory.path}
                      </h2>
                      <div className="mt-2 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
                        <Button
                          type="button"
                          variant="outline"
                          size="xs"
                          className="max-w-[240px] font-mono text-[13px] text-foreground"
                          aria-label={`Copy ${selectedMemory.id}`}
                          onClick={() => void copyText(selectedMemory.id)}
                        >
                          <Copy className="size-3.5" aria-hidden />
                          <span className="truncate">{compactEntityId(selectedMemory.id)}</span>
                        </Button>
                        <span>
                          Updated{' '}
                          {selectedMemory.updated_at
                            ? relativeTime(selectedMemory.updated_at)
                            : relativeTime(selectedMemory.created_at)}
                        </span>
                        <span>
                          {formatBytes(selectedMemory.content_size_bytes ?? selectedMemory.content?.length ?? 0)}
                        </span>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button type="button" variant="secondary" size="lg" onClick={() => setEditingContent(null)}>
                        <X className="size-4" aria-hidden />
                        Cancel
                      </Button>
                      <Button
                        type="button"
                        disabled={busyId === selectedMemory.id}
                        size="lg"
                        onClick={() => void saveInlineEdit()}
                      >
                        {busyId === selectedMemory.id ? 'Saving...' : 'Save'}
                      </Button>
                    </div>
                  </div>

                  <div className="px-5 py-5">
                    {fullMemory.error ? (
                      <Alert variant="destructive" className="mb-4 max-w-xl">
                        <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                        <AlertDescription>{fullMemory.error}</AlertDescription>
                      </Alert>
                    ) : null}
                    <p className="mb-2 text-xs text-muted-foreground">
                      Tab inserts indentation. Press Escape then Tab to move focus out of the editor.
                    </p>
                    <Textarea
                      aria-label="Memory content"
                      className="min-h-[320px] resize-y px-4 py-3 font-mono leading-6"
                      value={editingContent}
                      onChange={(event) => setEditingContent(event.target.value)}
                    />
                    <p className="mt-2 text-right text-xs text-muted-foreground">
                      {formatKilobytes(editingContent.length)} / 100kB
                    </p>
                  </div>
                </>
              ) : (
                <Tabs
                  value={viewMode}
                  onValueChange={(nextValue) => setViewMode(nextValue as MemoryViewMode)}
                  className="gap-0"
                >
                  <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border px-5 py-4">
                    <div className="min-w-0">
                      <h2 className="truncate text-[22px] font-semibold leading-7 text-foreground">
                        {selectedMemory.path}
                      </h2>
                      <div className="mt-2 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
                        <Button
                          type="button"
                          variant="outline"
                          size="xs"
                          className="max-w-[240px] font-mono text-[13px] text-foreground"
                          aria-label={`Copy ${selectedMemory.id}`}
                          onClick={() => void copyText(selectedMemory.id)}
                        >
                          <Copy className="size-3.5" aria-hidden />
                          <span className="truncate">{compactEntityId(selectedMemory.id)}</span>
                        </Button>
                        <span>
                          Updated{' '}
                          {selectedMemory.updated_at
                            ? relativeTime(selectedMemory.updated_at)
                            : relativeTime(selectedMemory.created_at)}
                        </span>
                        <span>
                          {formatBytes(selectedMemory.content_size_bytes ?? selectedMemory.content?.length ?? 0)}
                        </span>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <TabsList aria-label="View mode" className="h-9">
                        <TabsTrigger value="preview" className="gap-1.5 px-3">
                          <Eye className="size-4" aria-hidden />
                          Preview
                        </TabsTrigger>
                        <TabsTrigger value="source" className="gap-1.5 px-3">
                          <Code2 className="size-4" aria-hidden />
                          Source
                        </TabsTrigger>
                      </TabsList>
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <Button
                              type="button"
                              variant="outline"
                              size="icon-lg"
                              aria-label="More actions"
                              className="text-foreground"
                            />
                          }
                        >
                          <MoreVertical className="size-4" aria-hidden />
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-[164px]">
                          <DropdownMenuItem
                            disabled={busyId === selectedMemory.id}
                            onClick={() => void downloadSelectedMemory()}
                          >
                            <Download className="size-4" aria-hidden />
                            Download
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            variant="destructive"
                            disabled={busyId === selectedMemory.id}
                            onClick={() => setConfirmAction({ action: 'delete', memory: selectedMemory })}
                          >
                            <X className="size-4" aria-hidden />
                            Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                      <Button
                        type="button"
                        variant="secondary"
                        size="lg"
                        onClick={() => setEditingContent(selectedMemory.content ?? '')}
                      >
                        <Pencil className="size-4" aria-hidden />
                        Edit
                      </Button>
                    </div>
                  </div>

                  <div className="px-5 py-5">
                    {fullMemory.error ? (
                      <Alert variant="destructive" className="mb-4 max-w-xl">
                        <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                        <AlertDescription>{fullMemory.error}</AlertDescription>
                      </Alert>
                    ) : null}
                    {fullMemory.loading ? (
                      <Card size="sm">
                        <CardContent className="px-4 py-12 text-center text-sm text-muted-foreground">
                          Loading memory...
                        </CardContent>
                      </Card>
                    ) : viewMode === 'preview' ? (
                      <TabsContent value="preview" className="mt-0">
                        <div className="max-h-[460px] overflow-auto whitespace-pre-wrap break-words text-sm leading-6 text-foreground">
                          {memoryPreviewContent(selectedMemory)}
                        </div>
                      </TabsContent>
                    ) : (
                      <TabsContent value="source" className="mt-0">
                        <pre className="max-h-[460px] overflow-auto whitespace-pre-wrap break-words font-mono text-sm leading-6 text-foreground">
                          {selectedMemory.content || ''}
                        </pre>
                      </TabsContent>
                    )}
                  </div>
                </Tabs>
              )
            ) : (
              <div className="grid min-h-[520px] place-items-center px-6 text-center text-sm text-muted-foreground">
                <div>
                  <Database className="mx-auto mb-3 size-8 text-muted-foreground/70" aria-hidden />
                  <div className="font-semibold text-foreground">Select a memory</div>
                  <p className="mt-1 max-w-[300px]">Choose a file from the tree to view its contents.</p>
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {dialog ? (
        <MemoryDialog
          memory={dialog.memory}
          onClose={() => setDialog(null)}
          onSubmit={(values) => submit(values, dialog.memory)}
        />
      ) : null}
    </>
  );
}

export function MemoryTreeFolderButton({
  node,
  onToggle,
}: {
  node: Extract<MemoryTreeNode, { type: 'folder' }>;
  onToggle: () => void;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      aria-expanded={node.expanded}
      aria-label={`${node.expanded ? 'Collapse' : 'Expand'} folder ${node.label}`}
      className={clsx(
        'mb-1 h-auto w-full min-w-0 justify-start gap-2 rounded-md py-1.5 pr-2 text-left text-sm text-foreground hover:bg-accent',
        node.error && 'text-destructive',
      )}
      style={{ paddingLeft: `${8 + node.depth * 18}px` }}
      onClick={onToggle}
    >
      {node.expanded ? (
        <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      ) : (
        <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      )}
      {node.expanded ? (
        <FolderOpen className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      ) : (
        <Folder className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      )}
      <span className="truncate">{node.label}</span>
      {node.loading ? (
        <Loader2 className="ml-auto size-3.5 shrink-0 animate-spin text-muted-foreground/70" aria-hidden />
      ) : null}
    </Button>
  );
}

export function MemoryTreeMemoryButton({
  node,
  selected,
  onSelect,
}: {
  node: Extract<MemoryTreeNode, { type: 'memory' }>;
  selected: boolean;
  onSelect: () => void;
}) {
  const size = formatBytes(node.memory.content_size_bytes ?? node.memory.content?.length ?? 0);
  return (
    <Button
      type="button"
      variant="ghost"
      className={clsx(
        'mb-1 h-auto w-full min-w-0 justify-between gap-3 rounded-md py-2 pr-2 text-left text-sm',
        selected ? 'bg-accent text-foreground hover:bg-accent' : 'text-foreground hover:bg-accent',
      )}
      style={{ paddingLeft: `${28 + node.depth * 18}px` }}
      onClick={onSelect}
    >
      <span className="flex min-w-0 items-center gap-2">
        <FileText className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="truncate">{node.label}</span>
      </span>
      <span className="shrink-0 text-xs text-muted-foreground/70">{size}</span>
    </Button>
  );
}

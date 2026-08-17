import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Dialog, DialogContent } from '../../../shared/ui/dialog';
import { Label } from '../../../shared/ui/label';
import { toast } from '../../../shared/ui/sonner';
import { useWorkspace } from '../../../shared/workspaces/context';
import { Bot, BriefcaseBusiness, Cloud, Database, LockKeyhole, MessageCircle, Moon, Plus, Trash2 } from 'lucide-react';
import { type FormEvent, type ReactNode, useEffect, useState } from 'react';
import { AgentDetailPage, AgentsResourcePage } from '../agents/AgentsResourcePage';
import { archiveDream, cancelDream, createDream, listDreams, listManagedEntities } from '../api';
import { CompactChip, ManagedErrorAlert, ManagedSelectField, ManagedTextField, StatusPill } from '../components/common';
import { SessionDetailPage } from '../sessions/SessionDetailPage';
import {
  type DreamApiResponse,
  type DreamFormValues,
  type DreamStatus,
  type ManagedAgentSection,
  type ManagedEntitySection,
  type MemoryStoreApiResponse,
  type ResourceConfig,
} from '../types';
import { currentPathname, errorMessage, managedAgentIdFromPath, managedEntityIdFromPath } from '../utils';
import { ManagedDialogCloseControl, ManagedDialogHeader } from './dialog-components';
import { ManagedEntityDetailPage } from './detail';
import { ManagedEntitiesPage } from './entities';

export * from './detail';
export * from './dialogs';
export * from './entities';
export * from './model';

export const deploymentRows: Array<Record<string, ReactNode>> = [
  {
    ID: 'dep_7be...RBZ1oL',
    Name: 'deployment-sandbox-1781760043...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_FKb8Gkiy3...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_zcT...rRpT1v',
    Name: 'go-sdk-manual-run-deployment-1...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_PIXRdYnbh...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_yeM...swefHi',
    Name: 'Updated order triage',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_3Ss9giOjp...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_9Bw...19F73P',
    Name: 'deployment-sandbox-1781759888...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_UiozOnkBS...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
  {
    ID: 'dep_u2f...wxMWqs',
    Name: 'go-sdk-manual-run-deployment-1...',
    Status: <StatusPill>Archived</StatusPill>,
    Agent: <CompactChip icon={Bot}>agent_TqIatVd2cE...</CompactChip>,
    Trigger: 'Manual',
    Created: '2 hours ago',
  },
];

export const agentRows: Array<Record<string, ReactNode>> = [
  {
    ID: 'agent_pyFfN...yKtN6c',
    Name: 'Structured extractor',
    Model: '—',
    Status: <StatusPill>Active</StatusPill>,
    Created: '7 minutes ago',
    'Last updated': '7 minutes ago',
  },
  {
    ID: 'agent_p5M3v...1Mcu0R',
    Name: 'agent_d7f1f3b8e6a6_1',
    Model: '—',
    Status: <StatusPill>Active</StatusPill>,
    Created: '15 minutes ago',
    'Last updated': '15 minutes ago',
  },
  {
    ID: 'agent_jR13P...BHjtj8',
    Name: 'agent_5e2f4a9c0b12_2',
    Model: '—',
    Status: <StatusPill>Active</StatusPill>,
    Created: '34 minutes ago',
    'Last updated': '33 minutes ago',
  },
];

export const resourceConfigs: Record<Exclude<ManagedAgentSection, 'quickstart' | 'dreams'>, ResourceConfig> = {
  agents: {
    section: 'agents',
    title: 'Agents',
    description: 'Create and manage autonomous agents.',
    createLabel: 'Create agent',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Created  All time', 'Status  Active'],
    columns: ['ID', 'Name', 'Model', 'Status', 'Created', 'Last updated'],
    emptyTitle: 'No agents yet',
    emptyAction: 'Get started with agents',
    emptyIcon: Bot,
    rows: agentRows,
  },
  sessions: {
    section: 'sessions',
    title: 'Sessions',
    description: 'Trace and debug Claude Managed Agents sessions.',
    createLabel: 'Create session',
    searchPrefix: 'ID',
    searchPlaceholder: 'Search by session ID',
    filters: ['Created  All time', 'Agent  All', 'Deployment  All', 'Status  Active'],
    columns: ['', 'ID', 'Name', 'Status', 'Agent', 'Created'],
    emptyTitle: 'No sessions yet',
    emptyBody: 'Sessions will appear here once created through the API.',
    emptyIcon: MessageCircle,
  },
  deployments: {
    section: 'deployments',
    title: 'Deployments',
    description: 'A deployment binds an agent to credentials, an environment, and a schedule so it can run on its own.',
    createLabel: 'Create deployment',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Agent  All', 'Status  All'],
    columns: ['ID', 'Name', 'Status', 'Agent', 'Trigger', 'Created'],
    emptyTitle: 'No deployments yet',
    emptyBody: 'Deployments will appear after an agent is deployed.',
    emptyIcon: BriefcaseBusiness,
    rows: deploymentRows,
  },
  environments: {
    section: 'environments',
    title: 'Environments',
    description: 'Configuration template for containers, such as sessions or code execution.',
    createLabel: 'Create environment',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Status  All'],
    columns: ['ID', 'Name', 'Status', 'Type', 'Updated at'],
    emptyTitle: 'No environments yet',
    emptyBody: 'Create your first environment to get started.',
    emptyIcon: Cloud,
  },
  'credential-vaults': {
    section: 'credential-vaults',
    title: 'Credential vaults',
    description: 'Manage credential vaults that provide your agents with access to MCP servers and other tools.',
    createLabel: 'Create vault',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Status  All'],
    columns: ['ID', 'Name', 'Status', 'Created'],
    emptyTitle: 'No vaults yet',
    emptyBody: 'Create your first vault to get started.',
    emptyIcon: LockKeyhole,
  },
  'memory-stores': {
    section: 'memory-stores',
    title: 'Memory stores',
    description: 'Browse and manage persistent memory for your agents.',
    createLabel: 'Create memory store',
    searchPlaceholder: 'Search by name or exact ID',
    filters: ['Created  All time', 'Status  Active'],
    columns: ['', 'ID', 'Name', 'Status', 'Created'],
    emptyTitle: 'No memory stores yet',
    emptyBody: 'Memory stores give agents persistent, cross-session memory.',
    emptyIcon: Database,
  },
};

export function ManagedResourcePage({
  config,
  routeWorkspaceId,
}: {
  config: ResourceConfig;
  routeWorkspaceId?: string;
}) {
  if (config.section !== 'agents') {
    const entityConfig = config as ResourceConfig & { section: ManagedEntitySection };
    const detailId = managedEntityIdFromPath(entityConfig.section);
    if (detailId) {
      if (entityConfig.section === 'sessions') {
        return <SessionDetailPage config={entityConfig} sessionId={detailId} />;
      }
      return <ManagedEntityDetailPage config={entityConfig} entityId={detailId} />;
    }
    return <ManagedEntitiesPage config={entityConfig} />;
  }

  const agentId = managedAgentIdFromPath(currentPathname());
  if (agentId) {
    return <AgentDetailPage agentId={agentId} routeWorkspaceId={routeWorkspaceId} />;
  }

  return <AgentsResourcePage config={config} routeWorkspaceId={routeWorkspaceId} />;
}

function dreamStatusLabel(status: DreamStatus, msg: ReturnType<typeof useI18n>['msg']) {
  switch (status) {
    case 'pending':
      return msg('managedAgents.dreams.status.pending', 'Pending');
    case 'running':
      return msg('managedAgents.dreams.status.running', 'Running');
    case 'succeeded':
      return msg('managedAgents.dreams.status.succeeded', 'Succeeded');
    case 'failed':
      return msg('managedAgents.dreams.status.failed', 'Failed');
    case 'cancelled':
      return msg('managedAgents.dreams.status.cancelled', 'Cancelled');
    case 'archived':
      return msg('managedAgents.dreams.status.archived', 'Archived');
  }
}

function dreamInputSummary(dream: DreamApiResponse, msg: ReturnType<typeof useI18n>['msg']) {
  const store = dream.inputs.find((input) => input.type === 'memory_store');
  const sessions = dream.inputs.find((input) => input.type === 'sessions');
  return msg('managedAgents.dreams.inputSummary', '{store} · {count} sessions', {
    store: store?.memory_store_id || msg('managedAgents.dreams.unknownStore', 'unknown store'),
    count: sessions?.session_ids?.length ?? 0,
  });
}

function DreamCreateDialog({
  memoryStoreOptions,
  sessionOptions,
  onClose,
  onSubmit,
}: {
  memoryStoreOptions: Array<{ id: string; label: string }>;
  sessionOptions: Array<{ id: string; label: string }>;
  onClose: () => void;
  onSubmit: (values: DreamFormValues) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [values, setValues] = useState<DreamFormValues>({
    memoryStoreId: memoryStoreOptions[0]?.id ?? '',
    sessionIds: [],
    model: 'claude-opus-4-8',
  });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const toggleSession = (id: string) => {
    setValues((current) => ({
      ...current,
      sessionIds: current.sessionIds.includes(id)
        ? current.sessionIds.filter((item) => item !== id)
        : [...current.sessionIds, id],
    }));
  };
  const canSubmit =
    values.memoryStoreId.trim().length > 0 &&
    values.sessionIds.length > 0 &&
    values.model.trim().length > 0 &&
    !submitting;

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
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
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[560px]" showCloseButton={false}>
        <form onSubmit={submit}>
          <ManagedDialogCloseControl />
          <ManagedDialogHeader
            title={msg('managedAgents.dreams.createDialog.title', 'Create dream')}
            subtitle={msg(
              'managedAgents.dreams.createDialog.description',
              'Distill a memory store and sessions into a new memory store.',
            )}
          />
          <div className="subtle-scrollbar mt-5 space-y-4 overflow-y-auto pr-1">
            <ManagedSelectField
              label={msg('managedAgents.dreams.createDialog.memoryStore', 'Memory store')}
              value={values.memoryStoreId}
              placeholder={msg('managedAgents.dreams.createDialog.selectMemoryStore', 'Select a memory store')}
              options={memoryStoreOptions}
              onChange={(memoryStoreId) => setValues((current) => ({ ...current, memoryStoreId }))}
            />
            {sessionOptions.length ? (
              <div>
                <div className="text-sm font-medium text-foreground">
                  {msg('managedAgents.dreams.createDialog.sessions', 'Sessions')}
                </div>
                <div className="mt-2 max-h-56 space-y-1 overflow-y-auto rounded-lg border border-border bg-secondary p-2">
                  {sessionOptions.map((option) => {
                    const selected = values.sessionIds.includes(option.id);
                    return (
                      <Label
                        key={option.id}
                        htmlFor={`dream-session-${option.id}`}
                        className="flex h-9 w-full cursor-pointer items-center gap-3 rounded-md px-2 text-left text-sm font-normal text-foreground transition hover:bg-accent"
                      >
                        <Checkbox
                          id={`dream-session-${option.id}`}
                          checked={selected}
                          indeterminate={false}
                          onCheckedChange={() => toggleSession(option.id)}
                        />
                        <span className="truncate">{option.label}</span>
                      </Label>
                    );
                  })}
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                {msg(
                  'managedAgents.dreams.createDialog.noSessions',
                  'No sessions available. Create a session first to use as dream input.',
                )}
              </p>
            )}
            <ManagedTextField
              label={msg('managedAgents.dreams.createDialog.model', 'Model')}
              value={values.model}
              placeholder="claude-opus-4-8"
              onChange={(model) => setValues((current) => ({ ...current, model }))}
            />
          </div>
          {error ? <p className="mt-4 text-sm text-destructive">{error}</p> : null}
          <div className="mt-5 flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={submitting} onClick={onClose}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={!canSubmit}>
              {submitting
                ? msg('common.saving', 'Saving...')
                : msg('managedAgents.dreams.createDialog.submit', 'Create dream')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function DreamingPage() {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  const [dreams, setDreams] = useState<DreamApiResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [memoryStoreOptions, setMemoryStoreOptions] = useState<Array<{ id: string; label: string }>>([]);
  const [sessionOptions, setSessionOptions] = useState<Array<{ id: string; label: string }>>([]);
  const [loadingOptions, setLoadingOptions] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);

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
        const page = await listDreams(activeWorkspaceId);
        if (active) {
          setDreams(page.data ?? []);
          setLoading(false);
        }
      } catch (error) {
        if (active) {
          setDreams([]);
          setLoadError(errorMessage(error));
          setLoading(false);
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [activeWorkspaceId, refreshKey]);

  const loadOptions = async () => {
    setLoadingOptions(true);
    try {
      const [memoryStores, sessions] = await Promise.all([
        listManagedEntities('memory-stores', activeWorkspaceId),
        listManagedEntities('sessions', activeWorkspaceId),
      ]);
      setMemoryStoreOptions(
        (memoryStores.data as MemoryStoreApiResponse[]).map((store) => ({
          id: store.id,
          label: store.name || store.id,
        })),
      );
      setSessionOptions(
        (sessions.data as Array<{ id: string; title?: string | null }>).map((session) => ({
          id: session.id,
          label: session.title || session.id,
        })),
      );
    } finally {
      setLoadingOptions(false);
    }
  };

  const handleCreate = async (values: DreamFormValues) => {
    await createDream(
      {
        model: values.model.trim(),
        inputs: [
          { type: 'memory_store', memory_store_id: values.memoryStoreId },
          { type: 'sessions', session_ids: values.sessionIds },
        ],
      },
      activeWorkspaceId,
    );
    toast.success(msg('managedAgents.dreams.toastCreated', 'Dream created'));
    setCreateOpen(false);
    setRefreshKey((value) => value + 1);
  };

  const handleCancel = async (dream: DreamApiResponse) => {
    setMutationError(null);
    try {
      const updated = await cancelDream(dream.id, activeWorkspaceId);
      setDreams((current) => current.map((item) => (item.id === updated.id ? updated : item)));
    } catch (error) {
      setMutationError(errorMessage(error));
    }
  };

  const handleArchive = async (dream: DreamApiResponse) => {
    setMutationError(null);
    try {
      const updated = await archiveDream(dream.id, activeWorkspaceId);
      setDreams((current) => current.map((item) => (item.id === updated.id ? updated : item)));
    } catch (error) {
      setMutationError(errorMessage(error));
    }
  };

  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5 flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[28px] font-semibold leading-tight text-foreground">
            {msg('managedAgents.dreams.title', 'Dreaming')}
          </h1>
          <p className="mt-2 max-w-[760px] text-[15px] leading-5 text-muted-foreground">
            {msg(
              'managedAgents.dreams.description',
              'Review recent sessions to verify memory and surface new learnings.',
            )}
          </p>
        </div>
        <Button
          type="button"
          className="h-9 shrink-0"
          disabled={loadingOptions}
          onClick={() => {
            void loadOptions().then(() => setCreateOpen(true));
          }}
        >
          <Plus className="size-4" aria-hidden />
          {msg('managedAgents.dreams.createLabel', 'Create dream')}
        </Button>
      </header>

      {loadError ? <ManagedErrorAlert className="mb-3">{loadError}</ManagedErrorAlert> : null}
      {mutationError ? <ManagedErrorAlert className="mb-3">{mutationError}</ManagedErrorAlert> : null}

      {loading ? (
        <div className="mt-9 rounded-lg border border-border bg-popover px-6 py-6 text-[15px] text-foreground">
          {msg('managedAgents.dreams.loading', 'Captured Dreaming assets are loading.')}
        </div>
      ) : dreams.length ? (
        <div className="space-y-3">
          {dreams.map((dream) => (
            <div
              key={dream.id}
              className="flex items-center justify-between gap-4 rounded-lg border border-border bg-popover px-5 py-4"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <Moon className="size-4 text-muted-foreground" aria-hidden />
                  <span className="truncate font-medium text-foreground">{dream.id}</span>
                  <StatusPill tone={dream.status === 'succeeded' ? 'success' : 'neutral'}>
                    {dreamStatusLabel(dream.status, msg)}
                  </StatusPill>
                </div>
                <p className="mt-1 text-sm text-muted-foreground">{dreamInputSummary(dream, msg)}</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {msg('managedAgents.dreams.model', 'Model: {model}', { model: dream.model })}
                </p>
                {dream.outputs.length > 0 ? (
                  <p className="mt-1 text-sm text-muted-foreground">
                    {msg('managedAgents.dreams.output', 'Output store: {store}', {
                      store: dream.outputs[0].memory_store_id,
                    })}
                  </p>
                ) : null}
                {dream.error ? (
                  <p className="mt-1 text-sm text-destructive">
                    {msg('managedAgents.dreams.error', 'Error: {error}', { error: dream.error })}
                  </p>
                ) : null}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {dream.status === 'pending' || dream.status === 'running' ? (
                  <Button type="button" variant="outline" size="sm" onClick={() => void handleCancel(dream)}>
                    {msg('managedAgents.dreams.cancel', 'Cancel')}
                  </Button>
                ) : null}
                {dream.status === 'succeeded' || dream.status === 'failed' || dream.status === 'cancelled' ? (
                  <Button type="button" variant="ghost" size="sm" onClick={() => void handleArchive(dream)}>
                    <Trash2 aria-hidden />
                    {msg('managedAgents.dreams.archive', 'Archive')}
                  </Button>
                ) : null}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="mt-9 rounded-lg border border-border bg-popover px-6 py-6 text-[15px] text-foreground">
          {msg('managedAgents.dreams.empty', 'No dreams yet. Create a dream to distill sessions into memory.')}
        </div>
      )}

      {createOpen ? (
        <DreamCreateDialog
          memoryStoreOptions={memoryStoreOptions}
          sessionOptions={sessionOptions}
          onClose={() => setCreateOpen(false)}
          onSubmit={handleCreate}
        />
      ) : null}
    </section>
  );
}

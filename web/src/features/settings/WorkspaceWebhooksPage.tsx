import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import clsx from 'clsx';
import {
  AlertCircle,
  Ban,
  Check,
  Copy,
  Loader2,
  MoreVertical,
  Pencil,
  Plus,
  Power,
  RotateCcw,
  Trash2,
  Webhook,
  X,
} from 'lucide-react';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Badge } from '../../shared/ui/badge';
import { Card, CardContent } from '../../shared/ui/card';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import { Sheet, SheetClose, SheetContent, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { useI18n } from '../../shared/i18n';
import { defaultWorkspace, type Workspace } from '../../shared/workspaces/api';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';
import {
  createWebhookEndpoint,
  deleteWebhookEndpoint,
  listWebhookEndpoints,
  regenerateWebhookSigningSecret,
  updateWebhookEndpoint,
  updateWebhookEndpointStatus,
  type CreateWebhookEndpointInput,
  type UpdateWebhookEndpointInput,
  type WebhookEndpoint,
  type WebhookEndpointStatus,
} from './webhooksApi';

type WorkspaceWebhooksContentProps = {
  routeWorkspaceId?: string;
};

type WebhookAction = 'enable' | 'disable' | 'regenerate' | 'delete';

type PendingAction = {
  action: WebhookAction;
  webhook: WebhookEndpoint;
};

type SecretDisclosure = {
  webhook: WebhookEndpoint;
  source: 'created' | 'regenerated';
};

type WebhookEvent = {
  type: string;
  label: string;
};

type WebhookEventGroup = {
  label: string;
  events: WebhookEvent[];
};

type WebhookEventSummaryGroup = {
  label: string;
  labels: string[];
};

const webhookEventGroups: WebhookEventGroup[] = [
  {
    label: 'Session lifecycle',
    events: [
      { label: 'Run started', type: 'session.status_run_started' },
      { label: 'Rescheduled', type: 'session.status_rescheduled' },
      { label: 'Idled', type: 'session.status_idled' },
      { label: 'Terminated', type: 'session.status_terminated' },
    ],
  },
  {
    label: 'Threads',
    events: [
      { label: 'Created', type: 'session.thread_created' },
      { label: 'Idled', type: 'session.thread_idled' },
      { label: 'Terminated', type: 'session.thread_terminated' },
    ],
  },
  {
    label: 'Outcomes',
    events: [{ label: 'Evaluation ended', type: 'session.outcome_evaluation_ended' }],
  },
  {
    label: 'Vault lifecycle',
    events: [
      { label: 'Created', type: 'vault.created' },
      { label: 'Archived', type: 'vault.archived' },
      { label: 'Deleted', type: 'vault.deleted' },
    ],
  },
  {
    label: 'Credential lifecycle',
    events: [
      { label: 'Created', type: 'vault_credential.created' },
      { label: 'Archived', type: 'vault_credential.archived' },
      { label: 'Deleted', type: 'vault_credential.deleted' },
      { label: 'Refresh failed', type: 'vault_credential.refresh_failed' },
    ],
  },
];

const allWebhookEventTypes = webhookEventGroups.flatMap((group) => group.events.map((event) => event.type));

const webhookDetailEventGroups: WebhookEventGroup[] = [
  ...webhookEventGroups.slice(0, 3),
  {
    label: 'Session record',
    events: [
      { label: 'Updated', type: 'session.updated' },
      { label: 'Deleted', type: 'session.deleted' },
      { label: 'Updated', type: 'session.record_updated' },
      { label: 'Deleted', type: 'session.record_deleted' },
    ],
  },
  ...webhookEventGroups.slice(3),
];

const knownDetailEventTypes = new Set(
  webhookDetailEventGroups.flatMap((group) => group.events.map((event) => event.type)),
);

export function WorkspaceWebhooksPage() {
  const location = useLocation();
  return <WorkspaceWebhooksContent routeWorkspaceId={workspaceIdFromPath(location.pathname)} />;
}

export function WorkspaceWebhooksContent({ routeWorkspaceId }: WorkspaceWebhooksContentProps) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { orgUuid, workspaces, activeWorkspace, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [secretDisclosure, setSecretDisclosure] = useState<SecretDisclosure | null>(null);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);
  const [selectedWebhookId, setSelectedWebhookId] = useState<string | null>(null);
  const workspace = useMemo(
    () => resolveWorkspace(routeWorkspaceId, workspaces, activeWorkspace),
    [activeWorkspace, routeWorkspaceId, workspaces],
  );
  const queryKey = useMemo(
    () => ['console', 'workspace-webhooks', orgUuid, workspace.id] as const,
    [orgUuid, workspace.id],
  );

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  const webhooksQuery = useQuery({
    queryKey,
    queryFn: listWebhookEndpoints,
    enabled: Boolean(workspace.id),
    retry: false,
  });

  const createMutation = useMutation({
    mutationFn: createWebhookEndpoint,
    onSuccess: async (webhook) => {
      setCreateOpen(false);
      setSecretDisclosure({ webhook, source: 'created' });
      queryClient.setQueryData<WebhookEndpoint[]>(queryKey, (current) => upsertWebhook(current ?? [], webhook));
      await queryClient.invalidateQueries({ queryKey });
    },
  });

  const statusMutation = useMutation({
    mutationFn: ({ webhook, status }: { webhook: WebhookEndpoint; status: WebhookEndpointStatus }) =>
      updateWebhookEndpointStatus(webhook.id, status),
    onSuccess: (updatedWebhook) => {
      queryClient.setQueryData<WebhookEndpoint[]>(queryKey, (current) => upsertWebhook(current ?? [], updatedWebhook));
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  const updateDetailsMutation = useMutation({
    mutationFn: ({ webhook, input }: { webhook: WebhookEndpoint; input: UpdateWebhookEndpointInput }) =>
      updateWebhookEndpoint(webhook.id, input),
    onSuccess: (updatedWebhook) => {
      queryClient.setQueryData<WebhookEndpoint[]>(queryKey, (current) => upsertWebhook(current ?? [], updatedWebhook));
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (webhook: WebhookEndpoint) => deleteWebhookEndpoint(webhook.id),
    onSuccess: (_deleted, webhook) => {
      queryClient.setQueryData<WebhookEndpoint[]>(queryKey, (current) =>
        (current ?? []).filter((item) => item.id !== webhook.id),
      );
      setSelectedWebhookId((current) => (current === webhook.id ? null : current));
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  const regenerateSecretMutation = useMutation({
    mutationFn: (webhook: WebhookEndpoint) => regenerateWebhookSigningSecret(webhook.id),
    onSuccess: (response, webhook) => {
      setSecretDisclosure({
        webhook: { ...webhook, signing_secret: response.signing_secret },
        source: 'regenerated',
      });
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  const webhooks = webhooksQuery.data ?? [];
  const selectedWebhook = selectedWebhookId
    ? (webhooks.find((webhook) => webhook.id === selectedWebhookId) ?? null)
    : null;
  const errorMessage = readableError(webhooksQuery.error);
  const updateDetailsError = readableError(updateDetailsMutation.error);
  const actionError = readableError(
    pendingAction?.action === 'delete'
      ? deleteMutation.error
      : pendingAction?.action === 'regenerate'
        ? regenerateSecretMutation.error
        : statusMutation.error,
  );

  const handleCreate = async (input: Omit<CreateWebhookEndpointInput, 'name'> & { name?: string }) => {
    await createMutation.mutateAsync({
      ...input,
      name: input.name?.trim() || deriveWebhookName(input.url),
    });
  };

  const handleSaveWebhookDetails = async (webhook: WebhookEndpoint, input: UpdateWebhookEndpointInput) => {
    await updateDetailsMutation.mutateAsync({ webhook, input });
  };

  const handleConfirmAction = async () => {
    if (!pendingAction) {
      return;
    }
    if (pendingAction.action === 'delete') {
      await deleteMutation.mutateAsync(pendingAction.webhook);
    } else if (pendingAction.action === 'regenerate') {
      await regenerateSecretMutation.mutateAsync(pendingAction.webhook);
    } else {
      await statusMutation.mutateAsync({
        webhook: pendingAction.webhook,
        status: pendingAction.action === 'enable' ? 'enabled' : 'disabled',
      });
    }
    setPendingAction(null);
  };

  useEffect(() => {
    if (
      selectedWebhookId &&
      !webhooksQuery.isLoading &&
      !webhooks.some((webhook) => webhook.id === selectedWebhookId)
    ) {
      setSelectedWebhookId(null);
    }
  }, [selectedWebhookId, webhooks, webhooksQuery.isLoading]);

  return (
    <section className="w-full max-w-none" data-testid="workspace-webhooks-page">
      <div className="min-w-0" data-testid="workspace-webhooks-list">
        <div className="mb-7 flex items-start justify-between gap-4">
          <div>
            <h1 className="text-[28px] font-semibold leading-tight tracking-normal text-foreground">
              {msg('webhooks.title', 'Webhooks')}
            </h1>
            <p className="mt-2 max-w-[760px] text-sm leading-5 text-muted-foreground">
              {msg(
                'webhooks.description',
                'Webhook endpoints receive event notifications when things happen in your workspace.',
              )}
            </p>
          </div>
          <Button type="button" size="lg" className="shrink-0" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden />
            {msg('webhooks.addEndpoint', 'Add webhook endpoint')}
          </Button>
        </div>

        <div className="border-t border-border">
          <Table className="min-w-[880px] table-fixed text-left">
            <colgroup>
              <col className="w-[18%]" />
              <col className="w-[44%]" />
              <col className="w-[14%]" />
              <col className="w-[16%]" />
              <col className="w-[56px]" />
            </colgroup>
            <TableHeader className="text-[13px] text-muted-foreground">
              <TableRow className="border-border hover:bg-transparent">
                <TableHead className="px-3 py-3 text-muted-foreground">{msg('webhooks.table.id', 'ID')}</TableHead>
                <TableHead className="px-3 py-3 text-muted-foreground">{msg('webhooks.table.name', 'Name')}</TableHead>
                <TableHead className="px-3 py-3 text-muted-foreground">
                  {msg('webhooks.table.status', 'Status')}
                </TableHead>
                <TableHead className="px-3 py-3 text-muted-foreground">
                  {msg('webhooks.table.createdAt', 'Created at')}
                </TableHead>
                <TableHead className="px-3 py-3 text-right text-muted-foreground">
                  <span className="sr-only">{msg('common.actions', 'Actions')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {webhooksQuery.isLoading ? (
                <WebhooksState text={msg('webhooks.loading', 'Loading webhooks...')} />
              ) : errorMessage ? (
                <WebhooksState tone="error" text={errorMessage} />
              ) : webhooks.length > 0 ? (
                webhooks.map((webhook) => (
                  <WebhookRow
                    key={webhook.id}
                    webhook={webhook}
                    selected={webhook.id === selectedWebhookId}
                    onSelect={() => setSelectedWebhookId(webhook.id)}
                    onAction={(action) => setPendingAction({ action, webhook })}
                  />
                ))
              ) : (
                <WebhooksState
                  text={msg('webhooks.empty', 'No webhook endpoints have been created for {workspaceName}.', {
                    workspaceName: workspace.name,
                  })}
                />
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      {selectedWebhook ? (
        <WebhookDetailInspector
          webhook={selectedWebhook}
          isUpdating={updateDetailsMutation.isPending}
          updateError={updateDetailsError}
          onClose={() => setSelectedWebhookId(null)}
          onAction={(action) => setPendingAction({ action, webhook: selectedWebhook })}
          onSave={(input) => handleSaveWebhookDetails(selectedWebhook, input)}
        />
      ) : null}

      <CreateWebhookDialog
        open={createOpen}
        isSubmitting={createMutation.isPending}
        onClose={() => setCreateOpen(false)}
        onCreate={handleCreate}
      />

      <WebhookSecretDialog disclosure={secretDisclosure} onClose={() => setSecretDisclosure(null)} />

      {pendingAction ? (
        <ConfirmWebhookActionDialog
          pendingAction={pendingAction}
          isSubmitting={statusMutation.isPending || deleteMutation.isPending || regenerateSecretMutation.isPending}
          error={actionError}
          onClose={() => setPendingAction(null)}
          onConfirm={handleConfirmAction}
        />
      ) : null}
    </section>
  );
}

function WebhookRow({
  webhook,
  selected,
  onSelect,
  onAction,
}: {
  webhook: WebhookEndpoint;
  selected: boolean;
  onSelect: () => void;
  onAction: (action: WebhookAction) => void;
}) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  const disabled = webhook.status === 'disabled';

  const copyId = async () => {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(webhook.id);
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <TableRow
      className={clsx(
        'h-[58px] cursor-pointer border-border text-sm text-foreground hover:bg-accent',
        selected && 'bg-secondary',
      )}
      aria-selected={selected}
      onClick={onSelect}
    >
      <TableCell className="min-w-0 px-3 py-2.5 align-middle">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-auto max-w-full justify-start px-0 py-1 pr-1 text-left font-mono text-xs text-muted-foreground hover:bg-transparent hover:text-foreground"
          aria-label={msg('webhooks.copyId', 'Copy {id}', { id: webhook.id })}
          onClick={(event) => {
            event.stopPropagation();
            void copyId();
          }}
        >
          <span className="truncate">{truncateWebhookId(webhook.id)}</span>
          <span role="status" className="sr-only">
            {copied ? msg('common.copied', 'Copied') : ''}
          </span>
        </Button>
      </TableCell>
      <TableCell className="min-w-0 px-3 py-2.5 align-middle">
        <Button
          type="button"
          variant="ghost"
          className="h-auto min-w-0 justify-start px-0 py-0 text-left hover:bg-transparent"
          aria-label={`${webhook.name || webhook.url} ${webhook.url}`}
          onClick={(event) => {
            event.stopPropagation();
            onSelect();
          }}
        >
          <span className="block truncate font-medium">
            {webhook.name || msg('webhooks.untitled', 'Untitled webhook')}
          </span>
          <span className="mt-1 block truncate font-mono text-xs text-muted-foreground">{webhook.url}</span>
        </Button>
      </TableCell>
      <TableCell className="px-3 py-2.5 align-middle">
        <WebhookStatusBadge status={webhook.status} />
      </TableCell>
      <TableCell className="px-3 py-2.5 align-middle text-muted-foreground">
        <time dateTime={webhook.created_at} title={formatAbsoluteDate(webhook.created_at)}>
          {formatRelativeTime(webhook.created_at)}
        </time>
      </TableCell>
      <TableCell
        className="px-3 py-2.5 text-right align-middle"
        onClick={(event) => event.stopPropagation()}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon"
                className="text-muted-foreground hover:text-foreground"
                aria-label={msg('webhooks.actions', 'Webhook actions')}
                onClick={(event) => event.stopPropagation()}
              />
            }
          >
            <MoreVertical className="size-4" aria-hidden />
          </DropdownMenuTrigger>
          <WebhookActionsMenuContent disabled={disabled} onAction={onAction} />
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

function WebhookDetailInspector({
  webhook,
  isUpdating,
  updateError,
  onClose,
  onAction,
  onSave,
}: {
  webhook: WebhookEndpoint;
  isUpdating: boolean;
  updateError?: string | null;
  onClose: () => void;
  onAction: (action: WebhookAction) => void;
  onSave: (input: UpdateWebhookEndpointInput) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [editing, setEditing] = useState(false);
  const [copied, setCopied] = useState<'id' | 'url' | null>(null);
  const disabled = webhook.status === 'disabled';

  useEffect(() => {
    setEditing(false);
    setCopied(null);
  }, [webhook.id]);

  const copyValue = async (value: string, target: 'id' | 'url') => {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
    }
    setCopied(target);
    window.setTimeout(() => setCopied(null), 1400);
  };

  const displayName = webhook.name || msg('webhooks.untitled', 'Untitled webhook');

  return (
    <Sheet
      open
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
      }}
    >
      <SheetContent
        side="right"
        showOverlay={false}
        showCloseButton={false}
        data-testid="webhook-detail-inspector"
        className="webhook-detail-inspector subtle-scrollbar z-[70] gap-0 overflow-y-auto px-6 py-7 shadow-xl sm:px-8"
      >
        <SheetHeader className="gap-0">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <SheetTitle className="min-w-0 truncate text-xl font-semibold leading-tight tracking-normal">
                  {displayName}
                </SheetTitle>
                <WebhookStatusBadge status={webhook.status} />
              </div>
              <div className="mt-3 flex min-w-0 flex-wrap items-center gap-2 text-sm text-muted-foreground">
                <span>
                  {msg('webhooks.createdRelative', 'Created {time}', {
                    time: formatRelativeTime(webhook.created_at),
                  })}
                </span>
                <span aria-hidden>·</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  className="h-auto px-0 py-0 font-mono text-xs text-muted-foreground hover:bg-transparent hover:text-foreground"
                  aria-label={msg('webhooks.copyId', 'Copy {id}', { id: webhook.id })}
                  onClick={() => void copyValue(webhook.id, 'id')}
                >
                  {truncateWebhookId(webhook.id)}
                </Button>
                <span role="status" className="sr-only">
                  {copied === 'id' ? msg('common.copied', 'Copied') : ''}
                </span>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              {!editing ? (
                <>
                  <Button
                    type="button"
                    aria-label={msg('webhooks.edit', 'Edit webhook')}
                    variant="ghost"
                    size="icon"
                    className="text-muted-foreground hover:text-foreground"
                    onClick={() => setEditing(true)}
                  >
                    <Pencil className="size-4" aria-hidden />
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon"
                          className="text-muted-foreground hover:text-foreground"
                          aria-label={msg('webhooks.moreActions', 'More actions')}
                        />
                      }
                    >
                      <MoreVertical className="size-4" aria-hidden />
                    </DropdownMenuTrigger>
                    <WebhookActionsMenuContent disabled={disabled} onAction={onAction} />
                  </DropdownMenu>
                </>
              ) : null}
              <SheetClose
                render={
                  <Button
                    type="button"
                    aria-label={msg('webhooks.closeInspector', 'Close inspector')}
                    variant="ghost"
                    size="icon"
                    className="text-muted-foreground hover:text-foreground"
                  />
                }
              >
                <X className="size-4" aria-hidden />
              </SheetClose>
            </div>
          </div>
        </SheetHeader>

        <div className="mt-8 space-y-8">
          <WebhookEndpointDisplay
            url={webhook.url}
            copied={copied === 'url'}
            onCopy={() => void copyValue(webhook.url, 'url')}
          />

          {editing ? (
            <WebhookDetailEditForm
              webhook={webhook}
              isSubmitting={isUpdating}
              error={updateError}
              onCancel={() => setEditing(false)}
              onSave={async (input) => {
                await onSave(input);
                setEditing(false);
              }}
            />
          ) : (
            <WebhookSubscribedEvents events={webhook.enabled_events} />
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function WebhookEndpointDisplay({ url, copied, onCopy }: { url: string; copied: boolean; onCopy: () => void }) {
  const { msg } = useI18n();
  return (
    <section className="border-t border-border pt-8 first:border-t-0 first:pt-0">
      <h3 className="text-base font-semibold leading-tight text-foreground">{msg('webhooks.endpoint', 'Endpoint')}</h3>
      <p className="mt-2 text-sm leading-5 text-muted-foreground">
        {msg('webhooks.endpointHelp', 'Events are delivered to this URL via HTTPS POST.')}
      </p>
      <Card size="sm" className="mt-5 py-0">
        <CardContent className="flex min-h-[58px] items-center gap-3 px-4 py-3 text-foreground">
          <span className="min-w-0 flex-1 break-all font-mono text-sm leading-5">{url}</span>
          <Button
            type="button"
            aria-label={msg('webhooks.copyEndpointUrl', 'Copy endpoint URL')}
            variant="ghost"
            size="icon"
            className="text-muted-foreground hover:text-foreground"
            onClick={onCopy}
          >
            {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
          </Button>
        </CardContent>
      </Card>
    </section>
  );
}

function WebhookSubscribedEvents({ events }: { events: string[] }) {
  const { msg } = useI18n();
  const groups = summarizeWebhookEvents(events);

  return (
    <section className="border-t border-border pt-8">
      <div className="flex items-center gap-2">
        <h3 className="text-base font-semibold leading-tight text-foreground">
          {msg('webhooks.subscribedEvents', 'Subscribed events')}
        </h3>
        <Badge variant="secondary" className="h-6 min-w-6 rounded-full px-2 font-semibold">
          {events.length}
        </Badge>
      </div>
      <p className="mt-2 text-sm leading-5 text-muted-foreground">
        {msg(
          'webhooks.subscribedEventsHelp',
          'A POST request is sent to the endpoint each time one of these events occurs in this workspace.',
        )}
      </p>

      {groups.length > 0 ? (
        <div className="mt-5">
          {groups.map((group) => (
            <div
              key={group.label}
              className="grid min-h-[48px] grid-cols-[minmax(120px,0.7fr)_minmax(0,1.3fr)] items-start gap-4 border-t border-border py-3 text-sm leading-6 first:border-t-0"
            >
              <div className="text-muted-foreground">{group.label}</div>
              <div className="min-w-0 text-foreground">{group.labels.join(' · ')}</div>
            </div>
          ))}
        </div>
      ) : (
        <p className="mt-5 text-sm text-muted-foreground">
          {msg('webhooks.noSubscribedEvents', 'No events selected.')}
        </p>
      )}
    </section>
  );
}

function WebhookDetailEditForm({
  webhook,
  isSubmitting,
  error,
  onCancel,
  onSave,
}: {
  webhook: WebhookEndpoint;
  isSubmitting: boolean;
  error?: string | null;
  onCancel: () => void;
  onSave: (input: UpdateWebhookEndpointInput) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [name, setName] = useState(webhook.name ?? '');
  const [description, setDescription] = useState(webhook.description ?? '');
  const [selectedEvents, setSelectedEvents] = useState<string[]>(() => orderedEvents(new Set(webhook.enabled_events)));
  const canSubmit = selectedEvents.length > 0 && !isSubmitting;

  useEffect(() => {
    setName(webhook.name ?? '');
    setDescription(webhook.description ?? '');
    setSelectedEvents(orderedEvents(new Set(webhook.enabled_events)));
  }, [webhook]);

  const toggleEvent = (eventType: string) => {
    setSelectedEvents((current) => {
      const next = new Set(current);
      if (next.has(eventType)) {
        next.delete(eventType);
      } else {
        next.add(eventType);
      }
      return orderedEvents(next);
    });
  };

  const toggleGroup = (group: WebhookEventGroup) => {
    setSelectedEvents((current) => {
      const next = new Set(current);
      const allSelected = group.events.every((event) => next.has(event.type));
      group.events.forEach((event) => {
        if (allSelected) {
          next.delete(event.type);
        } else {
          next.add(event.type);
        }
      });
      return orderedEvents(next);
    });
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    await onSave({
      name: name.trim(),
      description: description.trim(),
      enabled_events: selectedEvents,
    });
  };

  return (
    <form className="border-t border-border pt-8" onSubmit={(event) => void handleSubmit(event).catch(() => undefined)}>
      <div className="space-y-4">
        <Label className="block" htmlFor="webhook-detail-name">
          {msg('webhooks.nameOptional', 'Name (optional)')}
        </Label>
        <Input
          id="webhook-detail-name"
          value={name}
          placeholder={msg('webhooks.namePlaceholder', 'My webhook endpoint')}
          onChange={(event) => setName(event.target.value)}
        />

        <Label className="block" htmlFor="webhook-detail-description">
          {msg('webhooks.descriptionOptional', 'Description (optional)')}
        </Label>
        <Textarea
          id="webhook-detail-description"
          value={description}
          placeholder={msg('webhooks.descriptionPlaceholder', 'Receives session lifecycle events')}
          className="min-h-[78px] resize-y"
          onChange={(event) => setDescription(event.target.value)}
        />
      </div>

      <fieldset className="mt-6">
        <legend className="mb-3 text-sm font-medium text-foreground">
          {msg('webhooks.eventsToSubscribe', 'Events to subscribe')}
        </legend>
        <div className="space-y-3 border-t border-border pt-3">
          {webhookEventGroups.map((group) => {
            const selectedCount = group.events.filter((event) => selectedEvents.includes(event.type)).length;
            return (
              <div key={group.label}>
                <div className="flex min-h-7 items-center justify-between gap-3 text-sm">
                  <span className="flex min-w-0 items-center gap-2">
                    <GroupCheckbox
                      checked={selectedCount === group.events.length}
                      indeterminate={selectedCount > 0 && selectedCount < group.events.length}
                      ariaLabel={`${group.label} events`}
                      onChange={() => toggleGroup(group)}
                    />
                    <span className="truncate font-medium text-foreground">{group.label}</span>
                  </span>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {selectedCount} of {group.events.length}
                  </span>
                </div>
                <div className="ml-6 mt-1 space-y-1">
                  {group.events.map((event) => (
                    <Label key={event.type} className="flex min-h-7 items-center gap-2 text-sm text-foreground">
                      <Checkbox
                        checked={selectedEvents.includes(event.type)}
                        onCheckedChange={() => toggleEvent(event.type)}
                      />
                      <span className="min-w-0 flex-1 truncate">{event.label}</span>
                    </Label>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      </fieldset>

      {error ? <InlineError>{error}</InlineError> : null}

      <div className="mt-6 flex justify-end gap-2">
        <Button type="button" variant="outline" size="lg" onClick={onCancel} disabled={isSubmitting}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button type="submit" disabled={!canSubmit} size="lg" className="min-w-[82px]">
          {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
          {msg('common.save', 'Save')}
        </Button>
      </div>
    </form>
  );
}

function WebhookStatusBadge({ status }: { status: WebhookEndpointStatus }) {
  const { msg } = useI18n();
  const disabled = status === 'disabled';
  return (
    <Badge
      variant="secondary"
      className={clsx(
        'h-6 rounded-md px-2 font-medium',
        disabled ? 'bg-secondary text-muted-foreground' : 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
      )}
    >
      {disabled ? msg('webhooks.disabled', 'Disabled') : msg('webhooks.enabled', 'Enabled')}
    </Badge>
  );
}

function WebhookActionsMenuContent({
  disabled,
  onAction,
}: {
  disabled: boolean;
  onAction: (action: WebhookAction) => void;
}) {
  const { msg } = useI18n();

  return (
    <DropdownMenuContent aria-label={msg('webhooks.actions', 'Webhook actions')} align="end" className="w-[232px]">
      <DropdownMenuItem onClick={() => onAction(disabled ? 'enable' : 'disable')}>
        {disabled ? <Power className="size-4" aria-hidden /> : <Ban className="size-4" aria-hidden />}
        <span>{disabled ? msg('webhooks.enable', 'Enable') : msg('webhooks.disable', 'Disable')}</span>
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => onAction('regenerate')}>
        <RotateCcw className="size-4" aria-hidden />
        <span>{msg('webhooks.regenerateSecret', 'Regenerate signing secret')}</span>
      </DropdownMenuItem>
      <DropdownMenuItem variant="destructive" onClick={() => onAction('delete')}>
        <Trash2 className="size-4" aria-hidden />
        <span>{msg('webhooks.delete', 'Delete')}</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  );
}

function CreateWebhookDialog({
  open,
  isSubmitting,
  onClose,
  onCreate,
}: {
  open: boolean;
  isSubmitting: boolean;
  onClose: () => void;
  onCreate: (input: Omit<CreateWebhookEndpointInput, 'name'> & { name?: string }) => Promise<void>;
}) {
  const { msg } = useI18n();
  const urlRef = useRef<HTMLInputElement>(null);
  const [url, setUrl] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selectedEvents, setSelectedEvents] = useState<string[]>(allWebhookEventTypes);
  const [error, setError] = useState('');
  const canSubmit = url.trim().length > 0 && selectedEvents.length > 0 && !isSubmitting;

  useEffect(() => {
    if (!open) {
      setUrl('');
      setName('');
      setDescription('');
      setSelectedEvents(allWebhookEventTypes);
      setError('');
    }
  }, [open]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setError('');
    try {
      await onCreate({
        url: url.trim(),
        name: name.trim(),
        description: description.trim(),
        enabled_events: selectedEvents,
      });
    } catch (createError) {
      setError(readableError(createError) ?? msg('webhooks.createFailed', 'Failed to create webhook endpoint.'));
    }
  };

  const toggleEvent = (eventType: string) => {
    setSelectedEvents((current) => {
      const next = new Set(current);
      if (next.has(eventType)) {
        next.delete(eventType);
      } else {
        next.add(eventType);
      }
      return orderedEvents(next);
    });
  };

  const toggleGroup = (group: WebhookEventGroup) => {
    setSelectedEvents((current) => {
      const next = new Set(current);
      const allSelected = group.events.every((event) => next.has(event.type));
      group.events.forEach((event) => {
        if (allSelected) {
          next.delete(event.type);
        } else {
          next.add(event.type);
        }
      });
      return orderedEvents(next);
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
      }}
    >
      <DialogContent
        className="max-h-[min(720px,calc(100vh-48px))] gap-0 overflow-hidden p-0 sm:max-w-[540px]"
        initialFocus={urlRef}
      >
        <DialogHeader className="px-4 py-4">
          <DialogTitle>{msg('webhooks.createTitle', 'Create webhook endpoint')}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="subtle-scrollbar-auto max-h-[min(588px,calc(100vh-192px))] overflow-y-auto pl-4 pr-2 py-4 space-y-4">
            <Label className="block" htmlFor="webhook-url">
              {msg('webhooks.endpointUrl', 'Endpoint URL')}
            </Label>
            <Input
              ref={urlRef}
              id="webhook-url"
              value={url}
              placeholder="https://example.com/webhooks"
              onChange={(event) => setUrl(event.target.value)}
            />

            <Label className="block" htmlFor="webhook-name">
              {msg('webhooks.nameOptional', 'Name (optional)')}
            </Label>
            <Input
              id="webhook-name"
              value={name}
              placeholder={msg('webhooks.namePlaceholder', 'My webhook endpoint')}
              onChange={(event) => setName(event.target.value)}
            />

            <Label className="block" htmlFor="webhook-description">
              {msg('webhooks.descriptionOptional', 'Description (optional)')}
            </Label>
            <Textarea
              id="webhook-description"
              value={description}
              placeholder={msg('webhooks.descriptionPlaceholder', 'Receives session lifecycle events')}
              className="min-h-[78px] resize-y"
              onChange={(event) => setDescription(event.target.value)}
            />

            <fieldset>
              <legend className="mb-3 text-sm font-medium text-foreground">
                {msg('webhooks.eventsToSubscribe', 'Events to subscribe')}
              </legend>
              <div className="space-y-3 border-t border-border pt-3">
                {webhookEventGroups.map((group) => {
                  const selectedCount = group.events.filter((event) => selectedEvents.includes(event.type)).length;
                  return (
                    <div key={group.label}>
                      <div className="flex min-h-7 items-center justify-between gap-3 text-sm">
                        <span className="flex min-w-0 items-center gap-2">
                          <GroupCheckbox
                            checked={selectedCount === group.events.length}
                            indeterminate={selectedCount > 0 && selectedCount < group.events.length}
                            ariaLabel={`${group.label} events`}
                            onChange={() => toggleGroup(group)}
                          />
                          <span className="truncate font-medium text-foreground">{group.label}</span>
                        </span>
                        <span className="shrink-0 text-xs text-muted-foreground">
                          {selectedCount} of {group.events.length}
                        </span>
                      </div>
                      <div className="ml-6 mt-1 space-y-1">
                        {group.events.map((event) => (
                          <Label key={event.type} className="flex min-h-7 items-center gap-2 text-sm text-foreground">
                            <Checkbox
                              checked={selectedEvents.includes(event.type)}
                              onCheckedChange={() => toggleEvent(event.type)}
                            />
                            <span className="min-w-0 flex-1 truncate">{event.label}</span>
                            <span className="hidden shrink-0 font-mono text-xs text-muted-foreground sm:inline">
                              {event.type}
                            </span>
                          </Label>
                        ))}
                      </div>
                    </div>
                  );
                })}
              </div>
            </fieldset>

            {error ? <InlineError>{error}</InlineError> : null}
          </div>

          <DialogFooter className="px-4 py-4">
            <Button type="submit" disabled={!canSubmit} size="lg" className="min-w-[82px]">
              {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
              {msg('common.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function GroupCheckbox({
  checked,
  indeterminate,
  ariaLabel,
  onChange,
}: {
  checked: boolean;
  indeterminate: boolean;
  ariaLabel: string;
  onChange: () => void;
}) {
  return <Checkbox aria-label={ariaLabel} checked={checked} indeterminate={indeterminate} onCheckedChange={onChange} />;
}

function WebhookSecretDialog({ disclosure, onClose }: { disclosure: SecretDisclosure | null; onClose: () => void }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  const secret = disclosure?.webhook.signing_secret ?? '';
  const title =
    disclosure?.source === 'regenerated'
      ? msg('webhooks.regeneratedTitle', 'Signing secret regenerated')
      : msg('webhooks.createdTitle', 'Webhook endpoint created');

  useEffect(() => {
    if (disclosure) {
      setCopied(false);
    }
  }, [disclosure]);

  const handleCopy = async () => {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(secret);
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <Dialog
      open={Boolean(disclosure?.webhook.signing_secret)}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
      }}
    >
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription className="mt-3 text-sm leading-6 text-muted-foreground">
            {msg(
              'webhooks.copySecret',
              "Copy this signing secret now. You won't be able to view it again after closing this window.",
            )}
          </DialogDescription>
        </DialogHeader>
        <Card size="sm" className="mt-5 py-0">
          <CardContent className="px-3 py-3">
            <div className="break-all font-mono text-sm leading-6 text-foreground">{secret}</div>
          </CardContent>
        </Card>
        <DialogFooter>
          <Button type="button" variant="outline" size="lg" onClick={() => void handleCopy()}>
            {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
            {copied ? msg('common.copied', 'Copied') : msg('common.copy', 'Copy')}
          </Button>
          <Button type="button" size="lg" onClick={onClose}>
            {msg('common.done', 'Done')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ConfirmWebhookActionDialog({
  pendingAction,
  isSubmitting,
  error,
  onClose,
  onConfirm,
}: {
  pendingAction: PendingAction;
  isSubmitting: boolean;
  error?: string | null;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const destructive = pendingAction.action === 'delete';
  const regenerating = pendingAction.action === 'regenerate';
  const action = actionLabel(pendingAction.action, msg);
  const icon =
    pendingAction.action === 'delete' ? (
      <Trash2 className="size-5" aria-hidden />
    ) : pendingAction.action === 'regenerate' ? (
      <RotateCcw className="size-5" aria-hidden />
    ) : pendingAction.action === 'disable' ? (
      <Ban className="size-5" aria-hidden />
    ) : (
      <Power className="size-5" aria-hidden />
    );
  const title = destructive
    ? msg('webhooks.deleteTitle', 'Delete webhook endpoint')
    : regenerating
      ? msg('webhooks.regenerateTitle', 'Regenerate signing secret?')
      : msg('webhooks.statusTitle', '{action} webhook endpoint?', { action });
  const body = destructive
    ? msg('webhooks.deleteBody', "Are you sure you want to delete {name}? This action can't be undone.", {
        name: pendingAction.webhook.name || pendingAction.webhook.id,
      })
    : regenerating
      ? msg(
          'webhooks.regenerateBody',
          'This will replace the current signing secret for {name}. Existing receivers must be updated to verify future deliveries.',
          {
            name: pendingAction.webhook.name || pendingAction.webhook.id,
          },
        )
      : msg('webhooks.statusBody', 'Are you sure you want to {action} {name}?', {
          action: action.toLowerCase(),
          name: pendingAction.webhook.name || pendingAction.webhook.id,
        });

  return (
    <AlertDialog
      open
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia className={destructive ? 'bg-destructive/10 text-destructive' : 'text-muted-foreground'}>
            {icon}
          </AlertDialogMedia>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{body}</AlertDialogDescription>
        </AlertDialogHeader>
        {error ? <InlineError>{error}</InlineError> : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isSubmitting} size="lg">
            {msg('common.cancel', 'Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            variant={destructive ? 'destructive' : 'default'}
            size="lg"
            className="min-w-[82px]"
            onClick={() => void onConfirm().catch(() => undefined)}
            disabled={isSubmitting}
          >
            {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
            {action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function InlineError({ children }: { children: ReactNode }) {
  return (
    <Alert variant="destructive" className="mt-4">
      <AlertCircle className="size-4 shrink-0" aria-hidden />
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

function WebhooksState({ text, tone = 'muted' }: { text: string; tone?: 'muted' | 'error' }) {
  return (
    <TableRow className="border-border hover:bg-transparent">
      <TableCell colSpan={5} className="h-[156px] px-4 py-8 text-center text-sm text-muted-foreground">
        {tone === 'error' ? (
          <Alert variant="destructive" className="mx-auto max-w-lg text-left">
            <AlertCircle className="size-4 shrink-0" aria-hidden />
            <AlertDescription>{text}</AlertDescription>
          </Alert>
        ) : (
          <div>
            <Webhook className="mx-auto mb-3 size-6 text-muted-foreground/70" aria-hidden />
            {text}
          </div>
        )}
      </TableCell>
    </TableRow>
  );
}

function resolveWorkspace(routeWorkspaceId: string | undefined, workspaces: Workspace[], activeWorkspace: Workspace) {
  if (!routeWorkspaceId) {
    return activeWorkspace;
  }
  const known = workspaces.find((workspace) => workspace.id === routeWorkspaceId);
  if (known) {
    return known;
  }
  if (routeWorkspaceId === defaultWorkspace.id) {
    return defaultWorkspace;
  }
  return {
    ...defaultWorkspace,
    id: routeWorkspaceId,
    name: routeWorkspaceId,
  };
}

function upsertWebhook(current: WebhookEndpoint[], webhook: WebhookEndpoint) {
  let replaced = false;
  const next = current.map((item) => {
    if (item.id !== webhook.id) {
      return item;
    }
    replaced = true;
    return webhook;
  });
  return replaced ? next : [webhook, ...next];
}

function orderedEvents(events: Set<string>) {
  const orderedKnownEvents = allWebhookEventTypes.filter((eventType) => events.has(eventType));
  const extraEvents = Array.from(events)
    .filter((eventType) => !allWebhookEventTypes.includes(eventType))
    .sort((left, right) => left.localeCompare(right));
  return [...orderedKnownEvents, ...extraEvents];
}

function summarizeWebhookEvents(events: string[]): WebhookEventSummaryGroup[] {
  const selected = new Set(events);
  const consumed = new Set<string>();
  const groups: WebhookEventSummaryGroup[] = [];

  webhookDetailEventGroups.forEach((group) => {
    const labels: string[] = [];
    group.events.forEach((event) => {
      if (!selected.has(event.type)) {
        return;
      }
      consumed.add(event.type);
      if (!labels.includes(event.label)) {
        labels.push(event.label);
      }
    });
    if (labels.length > 0) {
      groups.push({ label: group.label, labels });
    }
  });

  const unknownLabels = events
    .filter((eventType) => !consumed.has(eventType) && !knownDetailEventTypes.has(eventType))
    .map(prettyWebhookEventType);
  if (unknownLabels.length > 0) {
    groups.push({ label: 'Other', labels: unknownLabels });
  }

  return groups;
}

function prettyWebhookEventType(eventType: string) {
  return eventType
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function deriveWebhookName(rawUrl: string) {
  try {
    const parsed = new URL(rawUrl);
    return parsed.hostname || rawUrl;
  } catch {
    return rawUrl;
  }
}

function truncateWebhookId(id: string) {
  if (id.length <= 16) {
    return id;
  }
  return `${id.slice(0, 7)}...${id.slice(-6)}`;
}

function formatRelativeTime(value?: string | null) {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const diffMs = date.getTime() - Date.now();
  const absMs = Math.abs(diffMs);
  const formatter = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
  const minute = 60 * 1000;
  const hour = 60 * minute;
  const day = 24 * hour;
  if (absMs < minute) {
    return formatter.format(Math.round(diffMs / 1000), 'second');
  }
  if (absMs < hour) {
    return formatter.format(Math.round(diffMs / minute), 'minute');
  }
  if (absMs < day) {
    return formatter.format(Math.round(diffMs / hour), 'hour');
  }
  if (absMs < 30 * day) {
    return formatter.format(Math.round(diffMs / day), 'day');
  }
  return formatAbsoluteDate(value);
}

function formatAbsoluteDate(value?: string | null) {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat('en', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(date);
}

function actionLabel(action: WebhookAction, msg: ReturnType<typeof useI18n>['msg']) {
  switch (action) {
    case 'enable':
      return msg('webhooks.action.enable', 'Enable');
    case 'disable':
      return msg('webhooks.action.disable', 'Disable');
    case 'regenerate':
      return msg('webhooks.action.regenerate', 'Regenerate');
    case 'delete':
      return msg('webhooks.action.delete', 'Delete');
  }
}

function readableError(error: unknown) {
  if (!error) {
    return null;
  }
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === 'object' && error !== null && 'message' in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === 'string') {
      return message;
    }
  }
  return 'Request failed.';
}

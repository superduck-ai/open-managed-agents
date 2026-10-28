import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FlaskConical, Pencil, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { canManageLLMProviders } from '../../shared/permissions/llm-providers';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { toast } from '../../shared/ui/sonner';
import { Switch } from '../../shared/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { useWorkspace } from '../../shared/workspaces/context';
import {
  createTelemetryDestination,
  deleteTelemetryDestination,
  listTelemetryDestinations,
  testTelemetryDestination,
  updateTelemetryDestination,
  type DestinationInput,
  type TelemetryDestination,
} from './api';
import { DestinationFormDialog } from './DestinationFormDialog';
import { deliveryErrorMessage, requestErrorMessage } from './errors';
import { emptyForm, formFromDestination } from './form-model';

type Msg = ReturnType<typeof useI18n>['msg'];
type Editor = { mode: 'create' } | { mode: 'edit'; destination: TelemetryDestination } | null;

const DESTINATION_LIMIT = 10;

const DISABLED_REASONS: Record<string, [string, string]> = {
  caller: ['telemetry.disabled.caller', 'Disabled'],
  sustained_failure: ['telemetry.disabled.sustainedFailure', 'Disabled after 24h of failures'],
  invalid_address: ['telemetry.disabled.invalidAddress', 'Disabled: non-public address'],
  redirect: ['telemetry.disabled.redirect', 'Disabled: endpoint redirected'],
};

function stateLabel(destination: TelemetryDestination, msg: Msg) {
  if (destination.state.type === 'enabled') return msg('telemetry.enabled', 'Enabled');
  const reason = DISABLED_REASONS[destination.state.reason ?? 'caller'] ?? DISABLED_REASONS.caller;
  return msg(reason[0], reason[1]);
}

function testFailure(error: string | null, msg: Msg) {
  const summary = msg('telemetry.testFailed', 'Test span was not delivered');
  return error
    ? msg('telemetry.error.detail', '{summary}: {detail}', { summary, detail: deliveryErrorMessage(error, msg) })
    : summary;
}

function HealthCell({ destination, locale }: { destination: TelemetryDestination; locale: string }) {
  const { msg } = useI18n();
  const health = destination.delivery_health;
  const failedLast =
    health.last_error_at &&
    (!health.last_success_at || Date.parse(health.last_error_at) > Date.parse(health.last_success_at));
  if (failedLast)
    return (
      <span className="block max-w-56 truncate text-destructive" title={health.last_error ?? ''}>
        {deliveryErrorMessage(health.last_error ?? '', msg)}
      </span>
    );
  if (health.last_success_at)
    return (
      <span className="text-muted-foreground">
        {msg('telemetry.lastSuccess', 'Delivered {time}', {
          time: new Date(health.last_success_at).toLocaleString(locale),
        })}
      </span>
    );
  return <span className="text-muted-foreground">{msg('telemetry.noDeliveries', 'No deliveries yet')}</span>;
}

function DestinationRow({
  destination,
  canManage,
  onEdit,
  onDelete,
}: {
  destination: TelemetryDestination;
  canManage: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { locale, msg } = useI18n();
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['telemetry-destinations'] });
  const toggle = useMutation({
    mutationFn: (enabled: boolean) =>
      updateTelemetryDestination(destination.id, { state: { type: enabled ? 'enabled' : 'disabled' } }),
    onError: (error) =>
      toast.error(
        requestErrorMessage(error, msg('telemetry.toggleFailed', 'Could not change the destination state'), msg),
      ),
    onSettled: refresh,
  });
  const test = useMutation({
    mutationFn: () => testTelemetryDestination(destination.id),
    onSuccess: (result) =>
      result.success
        ? toast.success(msg('telemetry.testSucceeded', 'Test span delivered'))
        : toast.error(testFailure(result.error, msg)),
    onError: (error) =>
      toast.error(requestErrorMessage(error, msg('telemetry.testFailed', 'Test span was not delivered'), msg)),
    onSettled: refresh,
  });
  return (
    <TableRow>
      <TableCell className="max-w-64">
        <div className="truncate font-medium">{destination.display_name || destination.id}</div>
        <div className="truncate text-xs text-muted-foreground">{destination.traces.endpoint_url}</div>
      </TableCell>
      <TableCell>
        <Badge variant={destination.state.type === 'enabled' ? 'secondary' : 'outline'}>
          {stateLabel(destination, msg)}
        </Badge>
      </TableCell>
      <TableCell className="text-muted-foreground">
        {destination.traces.content_mode === 'content'
          ? msg('telemetry.contentMode.content', 'Metadata and content')
          : msg('telemetry.contentMode.metadata', 'Metadata only')}
      </TableCell>
      <TableCell>
        <HealthCell destination={destination} locale={locale} />
        {test.data && (
          <span
            role="status"
            className={`block max-w-56 truncate text-xs ${test.data.success ? 'text-muted-foreground' : 'text-destructive'}`}
          >
            {test.data.success
              ? msg('telemetry.testSucceeded', 'Test span delivered')
              : testFailure(test.data.error, msg)}
          </span>
        )}
      </TableCell>
      <TableCell>
        <div className="flex items-center justify-end gap-1">
          <Switch
            aria-label={msg('telemetry.toggle', 'Enable destination')}
            checked={destination.state.type === 'enabled'}
            disabled={!canManage || toggle.isPending}
            onCheckedChange={(checked) => toggle.mutate(checked)}
          />
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={msg('telemetry.test', 'Send test span')}
            disabled={!canManage || test.isPending}
            onClick={() => test.mutate()}
          >
            <FlaskConical />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={msg('telemetry.edit', 'Edit destination')}
            disabled={!canManage}
            onClick={onEdit}
          >
            <Pencil />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={msg('telemetry.delete', 'Delete destination')}
            disabled={!canManage}
            onClick={onDelete}
          >
            <Trash2 />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

function creationForm(workspaceId?: string) {
  return workspaceId ? { ...emptyForm(), scope: 'include' as const, workspaceIds: [workspaceId] } : emptyForm();
}

export function TelemetryDestinationsPage({ workspaceId }: { workspaceId?: string }) {
  const { msg } = useI18n();
  const { account } = useAuth();
  const { orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const canManage = canManageLLMProviders(account, orgUuid);
  const [editor, setEditor] = useState<Editor>(null);
  const [deleting, setDeleting] = useState<TelemetryDestination | null>(null);
  const query = useQuery({
    queryKey: ['telemetry-destinations', orgUuid, workspaceId],
    queryFn: () => listTelemetryDestinations(workspaceId),
    enabled: !!orgUuid,
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['telemetry-destinations'] });
  const atLimit = !workspaceId && (query.data?.length ?? 0) >= DESTINATION_LIMIT;
  const save = useMutation({
    mutationFn: (input: DestinationInput) =>
      editor?.mode === 'edit'
        ? updateTelemetryDestination(editor.destination.id, input)
        : createTelemetryDestination(input),
    onSuccess: () => {
      setEditor(null);
      void refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteTelemetryDestination(id),
    onError: (error) =>
      toast.error(requestErrorMessage(error, msg('telemetry.deleteFailed', 'Could not delete the destination'), msg)),
    onSettled: () => {
      setDeleting(null);
      void refresh();
    },
  });
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="text-xl font-semibold">{msg('telemetry.title', 'OpenTelemetry')}</h1>
          <p className="text-sm text-muted-foreground">
            {workspaceId
              ? msg(
                  'telemetry.workspaceDescription',
                  "Destinations that export this workspace's agent turn, model request and tool spans. Destinations belong to the organization and can cover several workspaces.",
                )
              : msg(
                  'telemetry.description',
                  'Export agent turn, model request and tool spans to your own OTLP backend. The platform does not store spans.',
                )}
          </p>
        </div>
        {canManage && (
          <Button onClick={() => setEditor({ mode: 'create' })} disabled={atLimit}>
            <Plus />
            {msg('telemetry.add', 'Add destination')}
          </Button>
        )}
      </div>
      {canManage && atLimit && (
        <p className="text-sm text-muted-foreground">
          {msg(
            'telemetry.limitReachedHint',
            'This organization has reached the limit of {limit} destinations. Delete one before adding another.',
            { limit: DESTINATION_LIMIT },
          )}
        </p>
      )}
      {query.isError && (
        <p role="alert" className="text-sm text-destructive">
          {msg('telemetry.loadError', 'Could not load telemetry destinations.')}
        </p>
      )}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{msg('telemetry.destination', 'Destination')}</TableHead>
            <TableHead>{msg('telemetry.state', 'State')}</TableHead>
            <TableHead>{msg('telemetry.contentMode', 'Content')}</TableHead>
            <TableHead>{msg('telemetry.health', 'Delivery')}</TableHead>
            <TableHead className="text-right">{msg('telemetry.actions', 'Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {query.data?.map((destination) => (
            <DestinationRow
              key={destination.id}
              destination={destination}
              canManage={canManage}
              onEdit={() => setEditor({ mode: 'edit', destination })}
              onDelete={() => setDeleting(destination)}
            />
          ))}
        </TableBody>
      </Table>
      {query.isSuccess && !query.data.length && (
        <p className="text-sm text-muted-foreground">{msg('telemetry.empty', 'No telemetry destinations yet.')}</p>
      )}
      {editor && (
        <DestinationFormDialog
          open
          editing={editor.mode === 'edit'}
          storedHeaderNames={editor.mode === 'edit' ? editor.destination.header_names : []}
          title={
            editor.mode === 'edit'
              ? msg('telemetry.editTitle', 'Edit destination')
              : msg('telemetry.createTitle', 'Add destination')
          }
          initial={editor.mode === 'edit' ? formFromDestination(editor.destination) : creationForm(workspaceId)}
          isSubmitting={save.isPending}
          onClose={() => setEditor(null)}
          onSubmit={async (input) => {
            await save.mutateAsync(input);
          }}
        />
      )}
      <AlertDialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{msg('telemetry.deleteTitle', 'Delete destination?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {msg('telemetry.deleteDescription', 'Spans will no longer be sent to {name}.', {
                name: deleting?.display_name || deleting?.id || '',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => deleting && remove.mutate(deleting.id)}>
              {msg('common.delete', 'Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

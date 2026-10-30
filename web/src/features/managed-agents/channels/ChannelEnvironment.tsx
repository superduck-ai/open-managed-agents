import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowRight } from 'lucide-react';
import { channelApi } from './api';
import { useChannelEnvironment, type EnvironmentSettings } from './useChannelEnvironment';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { errorMessage } from '../utils';

export function ChannelEnvironment({
  agentId,
  workspaceId,
  canEdit,
}: {
  agentId: string;
  workspaceId: string;
  canEdit: boolean;
}) {
  const { msg } = useI18n();
  const client = useQueryClient();
  const environment = useChannelEnvironment(agentId, workspaceId);
  const mutation = useMutation({
    mutationFn: (environmentId: string) =>
      channelApi<EnvironmentSettings>(agentId, workspaceId, '/settings', 'POST', {
        environment_id: environmentId,
      }),
    onSuccess: (data) => client.setQueryData(environment.queryKey, data),
  });
  const value = environment.selectedEnvironment?.id ?? null;
  const items = environment.environments.map((env) => ({ value: env.id, label: env.name || env.id }));
  const error = mutation.error || environment.error;
  return (
    <div className="space-y-3">
      <Field>
        <div className="flex items-center justify-between gap-3">
          <FieldLabel htmlFor="channel-environment">{msg('managedAgents.environments.kindTitle', '环境')}</FieldLabel>
          <a
            href={`/workspaces/${workspaceId}/environments`}
            className="inline-flex shrink-0 items-center gap-1 text-sm text-primary"
          >
            {msg('managedAgents.channels.manageEnvironments', '前往环境管理')}
            <ArrowRight className="size-4" />
          </a>
        </div>
        <Select
          value={value}
          items={items}
          disabled={!canEdit || environment.isPending || !!environment.error || mutation.isPending}
          onValueChange={(next) => {
            if (next && next !== value) mutation.mutate(next);
          }}
        >
          <SelectTrigger id="channel-environment" className="h-11 w-full">
            <SelectValue placeholder={msg('managedAgents.quickstart.selectEnvironment', '请选择环境')} />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            {items.map((item) => (
              <SelectItem key={item.value} value={item.value} label={item.label}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage(error)}</AlertDescription>
        </Alert>
      )}
    </div>
  );
}

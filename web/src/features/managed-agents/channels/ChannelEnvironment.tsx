import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowRight } from 'lucide-react';
import { channelApi } from './api';
import { useChannelEnvironment, type EnvironmentSettings } from './useChannelEnvironment';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Field, FieldLabel } from '../../../shared/ui/field';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '../../../shared/ui/select';
import type { EnvironmentApiResponse } from '../types';
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
  const queryKey = ['channel-environment', workspaceId, agentId];
  const settings = useChannelEnvironment(agentId, workspaceId);
  const environments = useQuery({
    queryKey: ['channel-environment-options', workspaceId],
    queryFn: async () => {
      const result: EnvironmentApiResponse[] = [];
      let cursor: string | null | undefined;
      do {
        const page = await anthropicBetaApi.environments.list<EnvironmentApiResponse>(
          { limit: 100, include_archived: false, ...(cursor ? { page: cursor } : {}) },
          workspaceId,
        );
        result.push(...page.data);
        cursor = page.next_page;
      } while (cursor);
      return result;
    },
    retry: false,
  });
  const mutation = useMutation({
    mutationFn: (environmentId: string) =>
      channelApi<EnvironmentSettings>(agentId, workspaceId, '/settings', 'POST', {
        environment_id: environmentId,
      }),
    onSuccess: (data) => client.setQueryData(queryKey, data),
  });
  const options = environments.data ?? [];
  const value = options.some((env) => env.id === settings.data?.environment_id)
    ? (settings.data?.environment_id ?? null)
    : null;
  const items = options.map((env) => ({ value: env.id, label: env.name || env.id }));
  const error = mutation.error || settings.error || environments.error;
  return (
    <div className="space-y-3">
      <Field>
        <FieldLabel htmlFor="channel-environment">{msg('managedAgents.environments.kindTitle', '环境')}</FieldLabel>
        <Select
          value={value}
          items={items}
          disabled={
            !canEdit ||
            settings.isPending ||
            settings.isError ||
            environments.isPending ||
            environments.isError ||
            mutation.isPending
          }
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
            <SelectSeparator />
            <a
              href={`/workspaces/${workspaceId}/environments`}
              className="flex items-center justify-center gap-2 p-3 text-sm text-primary"
            >
              {msg('managedAgents.channels.manageEnvironments', '前往环境管理')}
              <ArrowRight className="size-4" />
            </a>
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

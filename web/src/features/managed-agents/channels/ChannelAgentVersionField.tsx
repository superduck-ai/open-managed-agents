import { useId } from 'react';
import { useQuery } from '@tanstack/react-query';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { useI18n } from '../../../shared/i18n';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import type { AgentPageResponse } from '../types';
import { errorMessage } from '../utils';

export function ChannelAgentVersionField({
  workspaceId,
  agentId,
  value,
  onChange,
}: {
  workspaceId: string;
  agentId: string;
  value?: number | null;
  onChange: (version: number | null) => void;
}) {
  const { msg } = useI18n();
  const id = useId();
  const versions = useQuery({
    queryKey: ['channel-agent-versions', workspaceId, agentId],
    enabled: Boolean(agentId),
    queryFn: async () => {
      const values: number[] = [];
      let cursor: string | undefined;
      do {
        const page = (await anthropicBetaApi.agents.versions.list<AgentPageResponse['data'][number]>(
          agentId,
          { limit: 100, ...(cursor ? { page: cursor } : {}) },
          workspaceId,
        )) as AgentPageResponse;
        values.push(...page.data.map((agent) => agent.version));
        cursor = page.next_page ?? undefined;
      } while (cursor);
      return values;
    },
  });
  const values = [...new Set([...(versions.data ?? []), ...(value ? [value] : [])])].sort((a, b) => b - a);
  const items = [
    { value: 'latest', label: msg('managedAgents.channels.latestVersionShort', '默认最新') },
    ...values.map((version) => ({ value: String(version), label: `v${version}` })),
  ];
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{msg('managedAgents.channels.agentVersion', '智能体版本')}</Label>
      <Select
        value={value ? String(value) : 'latest'}
        disabled={!agentId || versions.isPending}
        items={items}
        onValueChange={(next) => onChange(next && next !== 'latest' ? Number(next) : null)}
      >
        <SelectTrigger id={id} className="h-10 w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {versions.error ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(versions.error)}
        </p>
      ) : null}
    </div>
  );
}

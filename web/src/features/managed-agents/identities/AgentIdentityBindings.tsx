import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { listAgentIdentityBindings } from './api';

export function AgentIdentityBindings({
  orgUuid,
  workspaceId,
  agentId,
}: {
  orgUuid?: string;
  workspaceId: string;
  agentId: string;
}) {
  const { msg } = useI18n();
  const query = useQuery({
    queryKey: ['agent-identity-bindings', orgUuid, workspaceId, agentId],
    enabled: Boolean(orgUuid && workspaceId && agentId),
    queryFn: () => listAgentIdentityBindings(orgUuid ?? '', workspaceId, agentId),
  });
  const rows = query.data?.data ?? [];
  return (
    <section className="mb-6 rounded-lg border p-4">
      <h2 className="mb-3 text-sm font-medium">{msg('managedAgents.identities.bindings', '执行身份绑定')}</h2>
      {query.isError ? (
        <p className="text-sm text-muted-foreground">
          {msg('managedAgents.identities.bindingsError', '绑定暂时无法加载。')}
        </p>
      ) : null}
      {!query.isError && rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {msg('managedAgents.identities.bindingsEmpty', '还没有范围配对或固定入口使用这个 Agent。')}
        </p>
      ) : null}
      {rows.length > 0 ? (
        <ul className="space-y-2 text-sm">
          {rows.map((row) => (
            <li
              key={`${row.pairing_id}-${row.channel_id}-${row.remote_id}`}
              className="flex flex-wrap justify-between gap-2"
            >
              <span>
                {row.display_name || row.identity_id} · {row.scope_kind} · {row.remote_id || row.channel_id}
              </span>
              <span className="font-mono text-xs text-muted-foreground">{row.status}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

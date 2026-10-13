import type { AgentApiResponse } from '../types';
import { AgentQuickView } from './AgentQuickView';
import { AgentStartSessionDialog } from './AgentStartSessionDialog';

export function AgentListOverlays({
  workspaceId,
  orgUuid,
  agents,
  quickViewAgentId,
  sessionAgent,
  onSelect,
  onStartSession,
}: {
  workspaceId: string;
  orgUuid?: string;
  agents: AgentApiResponse[];
  quickViewAgentId: string | null;
  sessionAgent: AgentApiResponse | null;
  onSelect: (agentId: string | null) => void;
  onStartSession: (agent: AgentApiResponse | null) => void;
}) {
  return (
    <>
      {quickViewAgentId ? (
        <AgentQuickView
          agentId={quickViewAgentId}
          agentIds={agents.map((agent) => agent.id)}
          workspaceId={workspaceId}
          orgUuid={orgUuid ?? ''}
          onSelect={onSelect}
          onClose={() => onSelect(null)}
          onStartSession={(agent) => {
            onSelect(null);
            onStartSession(agent);
          }}
        />
      ) : null}
      {sessionAgent ? (
        <AgentStartSessionDialog agent={sessionAgent} workspaceId={workspaceId} onClose={() => onStartSession(null)} />
      ) : null}
    </>
  );
}

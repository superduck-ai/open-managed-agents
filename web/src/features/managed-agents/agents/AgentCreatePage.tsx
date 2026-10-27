import { useEffect } from 'react';
import { createAgent } from '../api';
import type { AgentApiResponse, CreateAgentInput } from '../types';
import { navigateToAgentConfig, navigateToInternalHref } from '../utils';
import { CreateAgentEditor } from './create-dialog';
import { agentCreateHref } from './creation-route';

export function AgentCreatePage({
  workspaceId,
  onClose,
  onCreate,
}: {
  workspaceId: string;
  onClose?: () => void;
  onCreate?: (input: CreateAgentInput) => Promise<AgentApiResponse>;
}) {
  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get('create_agent') === '1') {
      window.history.replaceState(window.history.state, '', agentCreateHref(workspaceId));
      window.dispatchEvent(new window.PopStateEvent('popstate'));
    }
  }, [workspaceId]);
  return (
    <CreateAgentEditor
      workspaceId={workspaceId}
      onCreate={onCreate ?? ((input) => createAgent(input, workspaceId))}
      onCreated={(agent) => {
        onClose?.();
        navigateToAgentConfig(workspaceId, agent.id);
      }}
      onClose={() => {
        onClose?.();
        navigateToInternalHref(`/workspaces/${encodeURIComponent(workspaceId || 'default')}/agents`);
      }}
    />
  );
}

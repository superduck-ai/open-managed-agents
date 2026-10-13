import { useEffect, useState } from 'react';
import type { AgentApiResponse } from '../types';

export function useAgentListSelection(agents: AgentApiResponse[], workspaceId: string) {
  const scope = `${workspaceId}:${agents.map((agent) => `${agent.id}:${agent.archived_at ?? ''}`).join(',')}`;
  const [selection, setSelection] = useState<{ scope: string; ids: string[] }>({ scope: '', ids: [] });
  useEffect(() => setSelection({ scope, ids: [] }), [scope]);
  const selectedIds = selection.scope === scope ? selection.ids : [];
  const selectableIds = agents.filter((agent) => !agent.archived_at).map((agent) => agent.id);
  const allSelected = selectableIds.length > 0 && selectableIds.every((id) => selectedIds.includes(id));
  const toggleAll = (checked: boolean) => setSelection({ scope, ids: checked ? selectableIds : [] });
  const toggleAgent = (agentId: string, checked: boolean) => {
    setSelection((current) => {
      const ids = current.scope === scope ? current.ids : [];
      return { scope, ids: checked ? [...new Set([...ids, agentId])] : ids.filter((id) => id !== agentId) };
    });
  };
  return { selectedIds, selectableIds, allSelected, toggleAll, toggleAgent };
}

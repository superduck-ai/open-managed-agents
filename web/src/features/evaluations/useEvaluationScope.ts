import { useWorkspace } from '../../shared/workspaces/context';

export function useEvaluationScope() {
  const { orgUuid, activeWorkspaceId } = useWorkspace();
  return { orgUuid: orgUuid ?? '', workspaceId: activeWorkspaceId, ready: Boolean(orgUuid && activeWorkspaceId) };
}

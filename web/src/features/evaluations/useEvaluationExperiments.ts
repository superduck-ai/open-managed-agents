import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useAuth } from '../../shared/auth/context';
import {
  compareEvaluationExperiments,
  createEvaluationExperiment,
  getEvaluationExperiment,
  listEvaluationExperiments,
  startEvaluationExperiment,
  type CompareEvaluationExperimentsInput,
  type CreateEvaluationExperimentInput,
} from './api';
import { experimentIdFromPath, useEvaluationWorkspace } from './useEvaluationDatasets';

export function useEvaluationExperimentList() {
  const workspace = useEvaluationWorkspace();
  const query = useQuery({
    queryKey: evaluationExperimentKey(workspace.orgUuid, workspace.workspaceId),
    enabled: workspace.workspaceReady,
    queryFn: () => listEvaluationExperiments(workspace.orgUuid ?? '', workspace.workspaceId),
  });
  return { workspace, query };
}

export function useEvaluationExperimentBaseline(experimentId: string, enabled: boolean) {
  const workspace = useEvaluationWorkspace();
  return useQuery({
    queryKey: evaluationExperimentDetailKey(workspace.orgUuid, workspace.workspaceId, experimentId),
    enabled: enabled && workspace.workspaceReady && experimentId.length > 0,
    queryFn: () => getEvaluationExperiment(workspace.orgUuid ?? '', workspace.workspaceId, experimentId),
  });
}

export function useEvaluationExperiment() {
  const workspace = useEvaluationWorkspace();
  const experimentId = experimentIdFromPath();
  const query = useQuery({
    queryKey: evaluationExperimentDetailKey(workspace.orgUuid, workspace.workspaceId, experimentId),
    enabled: workspace.workspaceReady && experimentId.length > 0,
    queryFn: () => getEvaluationExperiment(workspace.orgUuid ?? '', workspace.workspaceId, experimentId),
    refetchInterval: (query) => evaluationExperimentRefreshInterval(query.state.data),
  });
  return { workspace, experimentId, query };
}

export function useCreateEvaluationExperiment() {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationExperimentInput) =>
      createEvaluationExperiment(workspace.orgUuid ?? '', workspace.workspaceId, input, csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: evaluationExperimentKey(workspace.orgUuid, workspace.workspaceId),
      });
    },
  });
}

export function useStartEvaluationExperiment(experimentId: string) {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      startEvaluationExperiment(workspace.orgUuid ?? '', workspace.workspaceId, experimentId, csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: evaluationExperimentDetailKey(workspace.orgUuid, workspace.workspaceId, experimentId),
      });
    },
  });
}

export function useCompareEvaluationExperiments() {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  return useMutation({
    mutationFn: (input: CompareEvaluationExperimentsInput) =>
      compareEvaluationExperiments(workspace.orgUuid ?? '', workspace.workspaceId, input, csrfToken),
  });
}

function evaluationExperimentKey(orgUuid: string | undefined, workspaceId: string) {
  return ['evaluation-experiments', orgUuid, workspaceId];
}

function evaluationExperimentDetailKey(orgUuid: string | undefined, workspaceId: string, experimentId: string) {
  return ['evaluation-experiment', orgUuid, workspaceId, experimentId];
}

function evaluationExperimentRefreshInterval(experiment: { status: string; item_count: number; evaluator_count: number; items?: unknown[]; evaluators?: unknown[]; scores?: unknown[] } | undefined) {
  if (!experiment) {
    return false;
  }
  if (experiment.status === 'pending' || experiment.status === 'running') {
    return 2000;
  }
  const items = Math.max(experiment.items?.length ?? 0, experiment.item_count);
  const evaluators = Math.max(experiment.evaluators?.length ?? 0, experiment.evaluator_count);
  const expected = items > 0 && evaluators > 0 ? items * evaluators : 0;
  if (expected > (experiment.scores?.length ?? 0)) {
    return 2000;
  }
  return false;
}

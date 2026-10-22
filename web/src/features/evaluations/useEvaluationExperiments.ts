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

export function useEvaluationExperiment() {
  const workspace = useEvaluationWorkspace();
  const experimentId = experimentIdFromPath();
  const query = useQuery({
    queryKey: evaluationExperimentDetailKey(workspace.orgUuid, workspace.workspaceId, experimentId),
    enabled: workspace.workspaceReady && experimentId.length > 0,
    queryFn: () => getEvaluationExperiment(workspace.orgUuid ?? '', workspace.workspaceId, experimentId),
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

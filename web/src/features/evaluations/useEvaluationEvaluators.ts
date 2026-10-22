import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useAuth } from '../../shared/auth/context';
import {
  createEvaluationEvaluator,
  createEvaluationEvaluatorFromPreset,
  createEvaluationEvaluatorVersion,
  getEvaluationEvaluator,
  listEvaluationEvaluatorPresets,
  listEvaluationEvaluators,
  type CreateEvaluationEvaluatorFromPresetInput,
  type CreateEvaluationEvaluatorInput,
  type CreateEvaluationEvaluatorVersionInput,
} from './api';
import { useEvaluationWorkspace } from './useEvaluationDatasets';

export function useEvaluationEvaluatorList() {
  const workspace = useEvaluationWorkspace();
  const query = useQuery({
    queryKey: evaluationEvaluatorKey(workspace.orgUuid, workspace.workspaceId),
    enabled: workspace.workspaceReady,
    queryFn: () => listEvaluationEvaluators(workspace.orgUuid ?? '', workspace.workspaceId),
  });
  return { workspace, query };
}

export function useEvaluationEvaluatorPresetList() {
  const workspace = useEvaluationWorkspace();
  const query = useQuery({
    queryKey: ['evaluation-evaluator-presets', workspace.orgUuid, workspace.workspaceId],
    enabled: workspace.workspaceReady,
    queryFn: () => listEvaluationEvaluatorPresets(workspace.orgUuid ?? '', workspace.workspaceId),
  });
  return { workspace, query };
}

export function evaluatorIdFromPath() {
  const match = window.location.pathname.match(/\/evaluations\/evaluators\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

export function useEvaluationEvaluator(evaluatorId: string) {
  const workspace = useEvaluationWorkspace();
  const query = useQuery({
    queryKey: ['evaluation-evaluator', workspace.orgUuid, workspace.workspaceId, evaluatorId],
    enabled: workspace.workspaceReady && evaluatorId.length > 0,
    queryFn: () => getEvaluationEvaluator(workspace.orgUuid ?? '', workspace.workspaceId, evaluatorId),
  });
  return { workspace, query };
}

export function useCreateEvaluationEvaluatorFromPreset() {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationEvaluatorFromPresetInput) =>
      createEvaluationEvaluatorFromPreset(workspace.orgUuid ?? '', workspace.workspaceId, input, csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: evaluationEvaluatorKey(workspace.orgUuid, workspace.workspaceId),
      });
    },
  });
}

export function useCreateEvaluationEvaluatorVersion(evaluatorId: string) {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationEvaluatorVersionInput) =>
      createEvaluationEvaluatorVersion(workspace.orgUuid ?? '', workspace.workspaceId, evaluatorId, input, csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['evaluation-evaluator', workspace.orgUuid, workspace.workspaceId, evaluatorId],
      });
      await queryClient.invalidateQueries({
        queryKey: evaluationEvaluatorKey(workspace.orgUuid, workspace.workspaceId),
      });
    },
  });
}

export function useCreateEvaluationEvaluator() {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationEvaluatorInput) =>
      createEvaluationEvaluator(workspace.orgUuid ?? '', workspace.workspaceId, input, csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: evaluationEvaluatorKey(workspace.orgUuid, workspace.workspaceId),
      });
    },
  });
}

function evaluationEvaluatorKey(orgUuid: string | undefined, workspaceId: string) {
  return ['evaluation-evaluators', orgUuid, workspaceId];
}

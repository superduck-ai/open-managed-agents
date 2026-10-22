import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useAuth } from '../../shared/auth/context';
import { useWorkspace } from '../../shared/workspaces/context';
import {
  createEvaluationCase,
  createEvaluationCaseFromSession,
  createEvaluationCasesFromSessions,
  createEvaluationDataset,
  getEvaluationDataset,
  importEvaluationCases,
  listEvaluationCases,
  listEvaluationDatasetPresetCases,
  listEvaluationDatasetPresets,
  listEvaluationDatasets,
  type CreateEvaluationCaseFromSessionInput,
  type CreateEvaluationCaseInput,
  type CreateEvaluationCasesFromSessionsInput,
  type CreateEvaluationDatasetInput,
} from './api';

export function useEvaluationWorkspace() {
  const { orgUuid, activeWorkspace, activeWorkspaceId, workspaces } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  return {
    orgUuid,
    workspaceId,
    workspaceReady: Boolean(orgUuid) && (!routeWorkspaceId || routeWorkspaceId === activeWorkspaceId),
    workspaceName: workspaces.find((workspace) => workspace.id === workspaceId)?.name ?? activeWorkspace.name,
  };
}

export function useEvaluationDatasetList() {
  const workspace = useEvaluationWorkspace();
  const query = useQuery({
    queryKey: evaluationDatasetKey(workspace.orgUuid, workspace.workspaceId),
    enabled: workspace.workspaceReady,
    queryFn: () => listEvaluationDatasets(workspace.orgUuid ?? '', workspace.workspaceId),
  });
  return { workspace, query };
}

export function useEvaluationDatasetPresetList() {
  const workspace = useEvaluationWorkspace();
  const query = useQuery({
    queryKey: ['evaluation-dataset-presets', workspace.orgUuid, workspace.workspaceId],
    enabled: workspace.workspaceReady,
    queryFn: () => listEvaluationDatasetPresets(workspace.orgUuid ?? '', workspace.workspaceId),
  });
  return { workspace, query };
}

export function useEvaluationDatasetPresetCases(presetId: string, versionId: string) {
  const workspace = useEvaluationWorkspace();
  return useQuery({
    queryKey: ['evaluation-dataset-preset-cases', workspace.orgUuid, workspace.workspaceId, presetId, versionId],
    enabled: workspace.workspaceReady && presetId.length > 0 && versionId.length > 0,
    queryFn: () =>
      listEvaluationDatasetPresetCases(workspace.orgUuid ?? '', workspace.workspaceId, presetId, versionId),
  });
}

export function useCreateEvaluationDataset() {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationDatasetInput) =>
      createEvaluationDataset(workspace.orgUuid ?? '', workspace.workspaceId, input, csrfToken),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: evaluationDatasetKey(workspace.orgUuid, workspace.workspaceId),
      });
    },
  });
}

export function useEvaluationCaseList(datasetId: string) {
  const workspace = useEvaluationWorkspace();
  const datasetQuery = useQuery({
    queryKey: ['evaluation-dataset', workspace.orgUuid, workspace.workspaceId, datasetId],
    enabled: workspace.workspaceReady && datasetId.length > 0,
    queryFn: () => getEvaluationDataset(workspace.orgUuid ?? '', workspace.workspaceId, datasetId),
  });
  const casesQuery = useQuery({
    queryKey: evaluationCaseKey(workspace.orgUuid, workspace.workspaceId, datasetId),
    enabled: workspace.workspaceReady && datasetId.length > 0,
    queryFn: () => listEvaluationCases(workspace.orgUuid ?? '', workspace.workspaceId, datasetId),
  });
  return { workspace, datasetQuery, casesQuery };
}

export function useCreateEvaluationCase(datasetId: string) {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationCaseInput) =>
      createEvaluationCase(workspace.orgUuid ?? '', workspace.workspaceId, datasetId, input, csrfToken),
    onSuccess: async () => {
      await invalidateCases(queryClient, workspace.orgUuid, workspace.workspaceId, datasetId);
    },
  });
}

export function useCreateEvaluationCaseFromSession(datasetId: string) {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationCaseFromSessionInput) =>
      createEvaluationCaseFromSession(workspace.orgUuid ?? '', workspace.workspaceId, datasetId, input, csrfToken),
    onSuccess: async () => {
      await invalidateCases(queryClient, workspace.orgUuid, workspace.workspaceId, datasetId);
    },
  });
}

export function useCreateEvaluationCasesFromSessions(datasetId: string) {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvaluationCasesFromSessionsInput) =>
      createEvaluationCasesFromSessions(workspace.orgUuid ?? '', workspace.workspaceId, datasetId, input, csrfToken),
    onSuccess: async () => {
      await invalidateCases(queryClient, workspace.orgUuid, workspace.workspaceId, datasetId);
    },
  });
}

export function useImportEvaluationCases(datasetId: string) {
  const workspace = useEvaluationWorkspace();
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (jsonl: string) =>
      importEvaluationCases(workspace.orgUuid ?? '', workspace.workspaceId, datasetId, jsonl, csrfToken),
    onSuccess: async () => {
      await invalidateCases(queryClient, workspace.orgUuid, workspace.workspaceId, datasetId);
    },
  });
}

export function datasetIdFromPath() {
  const match = window.location.pathname.match(/\/evaluations\/datasets\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
}

export function evaluationsIndexHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/evaluations`;
}

export function evaluationDatasetHref(workspaceId: string, datasetId: string) {
  return `${evaluationsIndexHref(workspaceId)}/datasets/${encodeURIComponent(datasetId)}`;
}

export function evaluationEvaluatorsHref(workspaceId: string) {
  return `${evaluationsIndexHref(workspaceId)}/evaluators`;
}

export function evaluationExperimentsHref(workspaceId: string) {
  return `${evaluationsIndexHref(workspaceId)}/experiments`;
}

export function evaluationExperimentNewHref(workspaceId: string) {
  return `${evaluationExperimentsHref(workspaceId)}/new`;
}

export function evaluationExperimentCompareHref(workspaceId: string) {
  return `${evaluationExperimentsHref(workspaceId)}/compare`;
}

export function evaluationExperimentHref(workspaceId: string, experimentId: string) {
  return `${evaluationExperimentsHref(workspaceId)}/${encodeURIComponent(experimentId)}`;
}

export function experimentIdFromPath() {
  const match = window.location.pathname.match(/\/evaluations\/experiments\/([^/]+)$/);
  if (!match || match[1] === 'new' || match[1] === 'compare') {
    return '';
  }
  return decodeURIComponent(match[1]);
}

function workspaceIdFromPath() {
  return window.location.pathname.match(/^\/workspaces\/([^/]+)\/evaluations(?:\/|$)/)?.[1] ?? '';
}

function evaluationDatasetKey(orgUuid: string | undefined, workspaceId: string) {
  return ['evaluation-datasets', orgUuid, workspaceId];
}

function evaluationCaseKey(orgUuid: string | undefined, workspaceId: string, datasetId: string) {
  return ['evaluation-cases', orgUuid, workspaceId, datasetId];
}

function invalidateCases(
  queryClient: ReturnType<typeof useQueryClient>,
  orgUuid: string | undefined,
  workspaceId: string,
  datasetId: string,
) {
  return queryClient.invalidateQueries({ queryKey: evaluationCaseKey(orgUuid, workspaceId, datasetId) });
}

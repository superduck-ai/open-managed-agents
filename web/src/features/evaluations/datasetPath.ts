export function datasetPath(workspaceId: string, datasetId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/evaluations/datasets/${encodeURIComponent(datasetId)}`;
}

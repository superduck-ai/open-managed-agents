export function taskPath(workspaceId: string, taskId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/evaluations/tasks/${encodeURIComponent(taskId)}`;
}

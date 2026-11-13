export function quickstartModelSetupHref(workspaceId: string, search: string) {
  const workspacePath = `/workspaces/${encodeURIComponent(workspaceId)}`;
  const mode = new URLSearchParams(search).get('mode') === 'api' ? 'api' : 'chat';
  return `/settings/llm?returnTo=${encodeURIComponent(`${workspacePath}/agent-quickstart?mode=${mode}`)}`;
}

export function quickstartModelSetupReturn(search: string, workspaceId: string) {
  const returnTo = new URLSearchParams(search).get('returnTo');
  if (!returnTo || !returnTo.startsWith('/') || returnTo.startsWith('//') || returnTo.includes('\\')) return null;
  const target = new URL(returnTo, 'https://oma.local');
  const path = `/workspaces/${encodeURIComponent(workspaceId)}/agent-quickstart`;
  if (target.origin !== 'https://oma.local' || target.pathname !== path) return null;
  const mode = target.searchParams.get('mode') === 'api' ? 'api' : 'chat';
  return `${path}?mode=${mode}`;
}

export function agentCreateHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/agents/new`;
}

export function isAgentCreateLocation() {
  return (
    /\/agents\/new\/?$/.test(window.location.pathname) ||
    new URLSearchParams(window.location.search).get('create_agent') === '1'
  );
}

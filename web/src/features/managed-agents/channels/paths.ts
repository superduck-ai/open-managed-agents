export function channelsListPath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/channels`;
}

export function channelCreatePath(workspaceId: string) {
  return `${channelsListPath(workspaceId)}/new`;
}

export function channelDetailPath(workspaceId: string, agentId: string, channelId: string) {
  return `${channelsListPath(workspaceId)}/${encodeURIComponent(agentId)}/${encodeURIComponent(channelId)}`;
}

export function channelEditPath(workspaceId: string, agentId: string, channelId: string) {
  return `${channelDetailPath(workspaceId, agentId, channelId)}/edit`;
}

export function channelSessionsTabPath(workspaceId: string, agentId: string, channelId: string) {
  return `${channelDetailPath(workspaceId, agentId, channelId)}?tab=sessions`;
}

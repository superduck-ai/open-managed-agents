import { type ApiError } from '../../shared/api/client';
import { copyText } from '../../shared/lib/clipboard';
import { type MouseEvent } from 'react';
import { type ManagedEntitySection } from './types';

export { copyText };

export function parseToolInput(inputJson: string, fallback: Record<string, unknown>) {
  if (!inputJson.trim()) {
    return fallback;
  }
  try {
    return JSON.parse(inputJson) as Record<string, unknown>;
  } catch {
    return fallback;
  }
}

export function toRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

export function errorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error) {
    const message = (error as ApiError | Error).message;
    if (typeof message === 'string' && message.trim()) {
      return message;
    }
  }
  return 'Request failed. Please try again.';
}

export function currentPathname() {
  return typeof window === 'undefined' ? '' : window.location.pathname;
}

export function managedWorkspaceIdFromPath(pathname: string) {
  const match = pathname.match(
    /^\/workspaces\/([^/]+)\/(?:agent-quickstart|agents|sessions|deployments|environments|vaults|memory-stores|dreams)(?:\/|$)/
  );
  return match ? decodeURIComponent(match[1]) : undefined;
}

export function managedAgentIdFromPath(pathname: string) {
  const workspaceMatch = pathname.match(/^\/workspaces\/[^/]+\/agents\/([^/]+)(?:\/|$)/);
  const directMatch = pathname.match(/^\/agents\/([^/]+)(?:\/|$)/);
  const raw = workspaceMatch?.[1] ?? directMatch?.[1];
  return raw ? decodeURIComponent(raw) : null;
}

export function downloadTextFile(filename: string, content: string) {
  if (typeof document === 'undefined' || typeof window === 'undefined') {
    return;
  }
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
  const url = window.URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.URL.revokeObjectURL(url);
}

export function isContentSha256(value?: string | null) {
  return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
}

export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B';
  }
  if (bytes < 1024) {
    return `${Math.round(bytes)} B`;
  }
  const kb = bytes / 1024;
  if (kb < 1024) {
    return `${kb.toFixed(kb >= 10 ? 0 : 1)} KB`;
  }
  const mb = kb / 1024;
  return `${mb.toFixed(mb >= 10 ? 0 : 1)} MB`;
}

export function formatKilobytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0.0kB';
  }
  return `${(Math.ceil(bytes / 100) / 10).toFixed(1)}kB`;
}

export function managedEntityIdFromPath(section: ManagedEntitySection) {
  if (typeof window === 'undefined') {
    return null;
  }
  const parts = window.location.pathname.split('/').filter(Boolean);
  const segment = sectionPathSegment(section);
  const index = parts.lastIndexOf(segment);
  if (index === -1) {
    return null;
  }
  const id = parts[index + 1];
  return id && !id.includes('/') ? decodeURIComponent(id) : null;
}

export function managedEntityListHref(workspaceId: string, section: ManagedEntitySection) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/${sectionPathSegment(section)}`;
}

export function managedEntityDetailHref(workspaceId: string, section: ManagedEntitySection, entityId: string) {
  return `${managedEntityListHref(workspaceId, section)}/${encodeURIComponent(entityId)}`;
}

export function sectionPathSegment(section: ManagedEntitySection) {
  switch (section) {
    case 'credential-vaults':
      return 'vaults';
    case 'memory-stores':
      return 'memory-stores';
    default:
      return section;
  }
}

export function agentDetailHref(workspaceId: string, agentId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/agents/${encodeURIComponent(agentId)}`;
}

export function navigateToAgentConfig(workspaceId: string, agentId: string) {
  if (typeof window === 'undefined') {
    return;
  }
  const href = `${agentDetailHref(workspaceId, agentId)}?tab=config`;
  navigateToInternalHref(href);
}

export function handleInternalLinkClick(event: MouseEvent<HTMLAnchorElement>, href: string) {
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.currentTarget.target ||
    event.metaKey ||
    event.altKey ||
    event.ctrlKey ||
    event.shiftKey ||
    typeof window === 'undefined'
  ) {
    return;
  }

  const targetUrl = new URL(href, window.location.href);
  if (targetUrl.origin !== window.location.origin) {
    return;
  }

  event.preventDefault();
  navigateToInternalHref(`${targetUrl.pathname}${targetUrl.search}${targetUrl.hash}`);
}

export function navigateToInternalHref(href: string) {
  if (typeof window === 'undefined') {
    return;
  }
  window.history.pushState(null, '', href);
  const event =
    typeof window.PopStateEvent === 'function' ? new window.PopStateEvent('popstate') : new window.Event('popstate');
  window.dispatchEvent(event);
}

export function cloneJsonValue<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

export function objectRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

export function compactEntityId(id: string) {
  if (id.length <= 20) {
    return id;
  }
  return `${id.slice(0, 8)}...${id.slice(-6)}`;
}

export function titleCase(value: string) {
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : value;
}

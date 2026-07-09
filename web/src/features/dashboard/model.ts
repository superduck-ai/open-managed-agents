import type { ComponentType } from 'react';
import { copyText } from '@/shared/lib/clipboard';
import { anthropicBetaApi } from '../../shared/api/anthropic';
import {
  filesRequestHeaders,
  messageBatchesRequestHeaders,
  skillsRequestHeaders
} from '../../shared/api/client';
import type { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';

export type IconComponent = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;

export type ConsoleFile = {
  id: string;
  type: 'file';
  filename: string;
  mime_type: string;
  size_bytes: number;
  created_at: string;
  downloadable: boolean;
};

export type FilesListResponse = {
  data: ConsoleFile[];
  has_more: boolean;
  first_id?: string | null;
  last_id?: string | null;
};

export type FilesPageCursor = {
  afterId?: string;
  beforeId?: string;
};

export type MessageBatchRequestCounts = {
  processing: number;
  succeeded: number;
  errored: number;
  canceled: number;
  expired: number;
};

export type ConsoleMessageBatch = {
  id: string;
  type: 'message_batch';
  processing_status: string;
  request_counts: MessageBatchRequestCounts;
  created_at: string;
  expires_at: string;
  ended_at?: string | null;
  cancel_initiated_at?: string | null;
  archived_at?: string | null;
  results_url?: string | null;
};

export type MessageBatchesListResponse = {
  data: ConsoleMessageBatch[];
  has_more: boolean;
  first_id?: string | null;
  last_id?: string | null;
};

export type MessageBatchesPageCursor = {
  afterId?: string;
  beforeId?: string;
};

export type ConsoleSkill = {
  id: string;
  type: 'skill';
  display_title: string;
  latest_version: string;
  source: string;
  created_at: string;
  updated_at: string;
};

export type ConsoleSkillVersion = {
  id: string;
  type: 'skill_version';
  description: string;
  directory: string;
  name: string;
  skill_id: string;
  version: string;
  created_at: string;
};

export type SkillsListResponse = {
  data: ConsoleSkill[];
  has_more: boolean;
  next_page?: string | null;
};

export type SkillVersionsListResponse = {
  data: ConsoleSkillVersion[];
  has_more: boolean;
  next_page?: string | null;
};

export type EnrichedConsoleSkill = ConsoleSkill & {
  description?: string;
  directory?: string;
  versionName?: string;
};

export type EnrichedSkillsListResponse = Omit<SkillsListResponse, 'data'> & {
  data: EnrichedConsoleSkill[];
};

const filesPageLimit = 20;
const messageBatchesPageLimit = 20;
const skillsPageLimit = 100;

function workspaceRequestHeaders(workspaceId: string) {
  const headers = new Headers();
  if (workspaceId) {
    headers.set('X-Workspace-ID', workspaceId);
  }
  return headers;
}

export function useDashboardWorkspaceScope() {
  const { activeWorkspace, activeWorkspaceId, workspaces } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromCurrentPath();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  const routeWorkspace = workspaces.find((workspace) => workspace.id === workspaceId);
  return {
    workspaceId,
    workspaceName: routeWorkspace?.name || (workspaceId === activeWorkspaceId ? activeWorkspace.name : workspaceId || 'current')
  };
}

function workspaceIdFromCurrentPath() {
  if (typeof window === 'undefined') {
    return '';
  }
  const match = window.location.pathname.match(/^\/workspaces\/([^/]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}
export function listFiles(cursor: FilesPageCursor, workspaceId: string) {
  const params: Record<string, string | number> = {
    limit: filesPageLimit
  };
  if (cursor.afterId) {
    params.after_id = cursor.afterId;
  }
  if (cursor.beforeId) {
    params.before_id = cursor.beforeId;
  }
  return anthropicBetaApi.files.list<ConsoleFile>(params, workspaceId) as Promise<FilesListResponse>;
}

export function listMessageBatches(cursor: MessageBatchesPageCursor, workspaceId: string) {
  const params: Record<string, string | number> = {
    limit: messageBatchesPageLimit
  };
  if (cursor.afterId) {
    params.after_id = cursor.afterId;
  }
  if (cursor.beforeId) {
    params.before_id = cursor.beforeId;
  }
  return anthropicBetaApi.messageBatches.list<ConsoleMessageBatch>(
    params,
    workspaceId
  ) as Promise<MessageBatchesListResponse>;
}

export function retrieveMessageBatch(batchId: string, workspaceId: string) {
  return anthropicBetaApi.messageBatches.retrieve<ConsoleMessageBatch>(batchId, workspaceId);
}

export function cancelMessageBatch(batchId: string, workspaceId: string) {
  return anthropicBetaApi.messageBatches.cancel<ConsoleMessageBatch>(batchId, workspaceId);
}

export async function listSkills(pageToken: string | undefined, workspaceId: string): Promise<SkillsListResponse> {
  const params: Record<string, string | number> = {
    limit: skillsPageLimit
  };
  if (pageToken) {
    params.page = pageToken;
  }
  return (await anthropicBetaApi.skills.list<ConsoleSkill>(
    params,
    workspaceId
  )) as SkillsListResponse;
}

export function retrieveSkill(skillId: string, workspaceId: string) {
  return anthropicBetaApi.skills.retrieve<ConsoleSkill>(skillId, workspaceId);
}

export function listSkillVersions(skillId: string, workspaceId: string) {
  return anthropicBetaApi.skills.versions.list<ConsoleSkillVersion>(
    skillId,
    { limit: 50 },
    workspaceId
  ) as Promise<SkillVersionsListResponse>;
}

export async function createSkillPackage(
  skillId: string | undefined,
  files: FileList | File[],
  workspaceId: string,
  displayTitle?: string
) {
  const uploadFiles = Array.from(files).map(skillUploadFile);
  if (skillId) {
    return anthropicBetaApi.skills.versions.create<ConsoleSkillVersion>(
      skillId,
      { files: uploadFiles },
      workspaceId
    );
  }
  return anthropicBetaApi.skills.create<ConsoleSkill>(
    {
      display_title: displayTitle?.trim() || null,
      files: uploadFiles
    },
    workspaceId
  );
}

function skillUploadFile(file: File) {
  const filename = skillUploadPath(file);
  if (filename === file.name) {
    return file;
  }
  return new File([file], filename, {
    type: file.type,
    lastModified: file.lastModified
  });
}

function skillUploadPath(file: File) {
  const skillPath = (file as File & { __skillPath?: string }).__skillPath;
  return skillPath || file.webkitRelativePath || file.name;
}

export async function deleteSkill(skillId: string, workspaceId: string) {
  const response = await fetch(`/v1/skills/${encodeURIComponent(skillId)}?beta=true`, {
    method: 'DELETE',
    headers: skillsRequestHeaders(workspaceRequestHeaders(workspaceId)),
    credentials: 'include'
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return response.json() as Promise<{ id: string; type: string }>;
}

export async function deleteSkillVersion(skillId: string, version: string, workspaceId: string) {
  const response = await fetch(
    `/v1/skills/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}?beta=true`,
    {
      method: 'DELETE',
      headers: skillsRequestHeaders(workspaceRequestHeaders(workspaceId)),
      credentials: 'include'
    }
  );
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  return response.json() as Promise<{ id: string; type: string }>;
}

async function responseErrorMessage(response: Response) {
  try {
    const payload = (await response.json()) as Record<string, unknown>;
    const error = payload.error;
    if (error && typeof error === 'object' && typeof (error as Record<string, unknown>).message === 'string') {
      return (error as Record<string, string>).message;
    }
    if (typeof payload.message === 'string') {
      return payload.message;
    }
  } catch {
    // Fall through to status text.
  }
  return response.statusText || 'Request failed.';
}

export function formatFileId(fileId: string) {
  if (fileId.length <= 16) {
    return fileId;
  }
  return `${fileId.slice(0, 5)}...${fileId.slice(-8)}`;
}

export function formatMessageBatchId(batchId: string) {
  if (batchId.length <= 24) {
    return batchId;
  }
  return `${batchId.slice(0, 9)}...${batchId.slice(-8)}`;
}

export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes < 0) {
    return '0 B';
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes / 1024;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const formatted = value >= 10 ? Math.round(value).toString() : value.toFixed(1).replace(/\.0$/, '');
  return `${formatted} ${units[unitIndex]}`;
}

export function formatBatchStatus(status: string, msg?: ReturnType<typeof useI18n>['msg']) {
  if (!msg) {
    return titleize(status.replace(/_/g, ' '));
  }
  switch (status) {
    case 'ended':
      return msg('batches.status.ended', 'Ended');
    case 'in_progress':
      return msg('batches.status.inProgress', 'In progress');
    case 'canceling':
      return msg('batches.status.canceling', 'Canceling');
    case 'canceled':
      return msg('batches.status.canceled', 'Canceled');
    default:
      return titleize(status.replace(/_/g, ' '));
  }
}

export function batchStatusClass(status: string) {
  switch (status) {
    case 'ended':
      return 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400';
    case 'in_progress':
      return 'bg-secondary text-secondary-foreground';
    case 'canceling':
      return 'bg-amber-500/10 text-amber-600 dark:text-amber-400';
    default:
      return 'bg-secondary text-muted-foreground';
  }
}

export function countBatchRequests(counts: MessageBatchRequestCounts) {
  return counts.processing + counts.succeeded + counts.errored + counts.canceled + counts.expired;
}

export function formatBatchRequestProgress(batch: ConsoleMessageBatch) {
  return `${batch.request_counts.succeeded} / ${countBatchRequests(batch.request_counts)}`;
}

export function batchRequestProgressClass(batch: ConsoleMessageBatch) {
  const totalRequests = countBatchRequests(batch.request_counts);
  if (totalRequests > 0 && batch.request_counts.succeeded === totalRequests) {
    return 'bg-emerald-500';
  }
  return 'bg-destructive';
}

export function canCancelBatch(batch: ConsoleMessageBatch) {
  return batch.processing_status === 'in_progress';
}

export function formatRelativeTime(value: string) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return value;
  }
  const diffSeconds = Math.round((timestamp - Date.now()) / 1000);
  const absoluteSeconds = Math.abs(diffSeconds);
  const formatter = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
  if (absoluteSeconds < 60) {
    return 'less than a minute ago';
  }
  if (absoluteSeconds < 3600) {
    return formatter.format(Math.round(diffSeconds / 60), 'minute');
  }
  if (absoluteSeconds < 86_400) {
    return formatter.format(Math.round(diffSeconds / 3600), 'hour');
  }
  if (absoluteSeconds < 2_592_000) {
    return formatter.format(Math.round(diffSeconds / 86_400), 'day');
  }
  return new Intl.DateTimeFormat('en', {
    month: 'short',
    day: 'numeric',
    year: new Date(timestamp).getFullYear() === new Date().getFullYear() ? undefined : 'numeric'
  }).format(timestamp);
}

export function formatBatchDateTime(value?: string | null) {
  if (!value) {
    return '-';
  }
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return value;
  }
  return new Intl.DateTimeFormat('en', {
    month: 'short',
    day: 'numeric',
    year: new Date(timestamp).getFullYear() === new Date().getFullYear() ? undefined : 'numeric',
    hour: 'numeric',
    minute: '2-digit'
  }).format(timestamp);
}

export function formatSkillSource(source: string, msg?: ReturnType<typeof useI18n>['msg']) {
  const normalized = source.trim().toLowerCase();
  if (normalized === 'anthropic') {
    return 'Anthropic';
  }
  if (normalized === 'custom') {
    return msg ? msg('skills.source.custom', 'Custom') : 'Custom';
  }
  return titleize(normalized || 'custom');
}

export function errorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Try refreshing the page.';
}

export { copyText };

export async function downloadFile(file: ConsoleFile, workspaceId: string) {
  const response = await fetch(`/v1/files/${encodeURIComponent(file.id)}/content?beta=true`, {
    headers: filesRequestHeaders(workspaceRequestHeaders(workspaceId)),
    credentials: 'include'
  });
  if (!response.ok) {
    throw new Error('File could not be downloaded.');
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = file.filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export async function downloadMessageBatchResults(batch: ConsoleMessageBatch, workspaceId: string) {
  const response = await fetch(`/v1/messages/batches/${encodeURIComponent(batch.id)}/results?beta=true`, {
    headers: messageBatchesRequestHeaders(workspaceRequestHeaders(workspaceId)),
    credentials: 'include'
  });
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `${batch.id}.jsonl`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export async function downloadSkillVersion(skillId: string, version: string, directory: string | undefined, workspaceId: string) {
  const response = await fetch(
    `/v1/skills/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}/content?beta=true`,
    {
      headers: skillsRequestHeaders(workspaceRequestHeaders(workspaceId)),
      credentials: 'include'
    }
  );
  if (!response.ok) {
    throw new Error(await responseErrorMessage(response));
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `${directory || skillId}.skill`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function skillsIndexHref() {
  const workspaceMatch = window.location.pathname.match(/^\/workspaces\/([^/]+)/);
  if (workspaceMatch) {
    return `/workspaces/${workspaceMatch[1]}/skills`;
  }
  return '/skills';
}

export function skillDetailHref(skillId: string) {
  const params = new URLSearchParams(window.location.search);
  params.set('skill', skillId);
  return `${skillsIndexHref()}?${params.toString()}`;
}

export function createSkillHref() {
  return `${skillsIndexHref()}/new`;
}

export function currentSkillId() {
  const querySkill = new URLSearchParams(window.location.search).get('skill');
  if (querySkill) {
    return querySkill;
  }
  const match = window.location.pathname.match(/\/skills\/([^/?#]+)/);
  if (!match || match[1] === 'new') {
    return '';
  }
  return decodeURIComponent(match[1]);
}

export function currentBatchId() {
  return new URLSearchParams(window.location.search).get('batch') ?? '';
}

export function batchDetailHref(batchId: string) {
  const params = new URLSearchParams(window.location.search);
  params.set('batch', batchId);
  return `${window.location.pathname}?${params.toString()}`;
}

export function clearBatchDetailHref() {
  const params = new URLSearchParams(window.location.search);
  params.delete('batch');
  const search = params.toString();
  return search ? `${window.location.pathname}?${search}` : window.location.pathname;
}
export function formatRole(role?: string, msg?: ReturnType<typeof useI18n>['msg']) {
  if (!role) {
    return msg ? msg('members.role.member', 'Member') : 'Member';
  }
  return role
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
    .join(' ');
}

export function titleize(section: string) {
  return section
    .split('-')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

import { consoleApi, type ApiError } from '../../shared/api/client';
import { normalizeReturnTo } from '../../shared/auth/redirects';

export function invitationReturnTo(value?: string) {
  const target = normalizeReturnTo(value);
  return target.split(/[?#]/)[0].replace(/\/$/, '') === '/invites' ? '/' : target;
}

const organizationRoleLabels: Record<string, string> = {
  admin: '管理员',
  developer: '开发者',
  billing: '财务',
  user: '用户',
  claude_code_user: 'Claude Code 用户',
};

export function organizationRoleLabel(role?: string) {
  return organizationRoleLabels[role ?? ''] ?? '成员';
}

export function invitationErrorMessage(error: unknown, fallback = '操作失败，请重试。') {
  const apiError = error as Partial<ApiError> | null;
  switch (apiError?.status) {
    case 401:
      return '登录已失效，请重新登录。';
    case 403:
      return '你无权处理此邀请。';
    case 404:
      return '此邀请不存在或已不可用。';
    case 409:
      if (apiError.message === 'Invitation has expired') return '此邀请已过期，请联系组织管理员重新邀请。';
      if (apiError.message === 'Invitation has been revoked') return '此邀请已被撤销，请联系组织管理员。';
      if (apiError.message === 'Invitation can no longer be processed') return '此邀请已处理，无需重复操作。';
      return '此邀请状态已变更，请刷新后重试。';
    default:
      return fallback;
  }
}

export type Invitation = {
  id: string;
  organization_uuid: string;
  organization_name: string;
  role: string;
  invited_at: string;
  expires_at: string;
};

export function listInvitations() {
  return consoleApi<{ data: Invitation[] }>('/api/invitations', { context: {} });
}

export function respondToInvitation(id: string, action: 'accept' | 'decline') {
  return consoleApi<{ id: string; status: string; organization_uuid: string }>(
    `/api/invitations/${encodeURIComponent(id)}/${action}`,
    { method: 'POST', context: {} },
  );
}

import type { I18nContextValue } from '../../shared/i18n/context';
import { consoleApi, type ApiError } from '../../shared/api/client';
import { normalizeReturnTo } from '../../shared/auth/redirects';

export function invitationReturnTo(value?: string) {
  const target = normalizeReturnTo(value);
  return target.split(/[?#]/)[0].replace(/\/$/, '') === '/invites' ? '/' : target;
}

const organizationRoleLabels: Record<string, string> = {
  admin: '管理员',
  user: '用户',
};

export function organizationRoleLabel(role?: string, msg?: I18nContextValue['msg']) {
  const fallback = organizationRoleLabels[role ?? ''] ?? '成员';
  const id =
    role === 'admin'
      ? 'account.role.workspace_admin'
      : role === 'user'
        ? 'account.role.workspace_user'
        : 'members.role.member';
  return msg?.(id, fallback) ?? fallback;
}

export function invitationErrorMessage(error: unknown, fallback?: string, msg?: I18nContextValue['msg']) {
  const text = (id: string, value: string) => msg?.(id, value) ?? value;
  const apiError = error as Partial<ApiError> | null;
  switch (apiError?.status) {
    case 401:
      return text('invitations.error.sessionExpired', '登录已失效，请重新登录。');
    case 403:
      return text('invitations.error.forbidden', '你无权处理此邀请。');
    case 404:
      return text('invitations.error.unavailable', '此邀请不存在或已不可用。');
    case 409:
      if (apiError.message === 'Invitation has expired')
        return text('invitations.error.expired', '此邀请已过期，请联系组织管理员重新邀请。');
      if (apiError.message === 'Invitation has been revoked')
        return text('invitations.error.revoked', '此邀请已被撤销，请联系组织管理员。');
      if (apiError.message === 'Invitation can no longer be processed')
        return text('invitations.error.processed', '此邀请已处理，无需重复操作。');
      return text('invitations.error.changed', '此邀请状态已变更，请刷新后重试。');
    default:
      return fallback ?? text('invitations.error.generic', '操作失败，请重试。');
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

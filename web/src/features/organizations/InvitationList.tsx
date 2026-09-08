import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useAuth } from '../../shared/auth/context';
import { Button } from '../../shared/ui/button';
import { invitationErrorMessage, organizationRoleLabel, respondToInvitation, type Invitation } from './api';

export function InvitationList({
  invitations,
  error,
  loading,
  retry,
  enter,
  standalone = false,
}: {
  invitations?: Invitation[];
  error: unknown;
  loading: boolean;
  retry: () => void;
  enter: (orgUuid: string) => void;
  standalone?: boolean;
}) {
  const { account, refresh } = useAuth();
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [accepted, setAccepted] = useState<Invitation[]>([]);
  const [refreshFailed, setRefreshFailed] = useState(false);
  const queryKey = ['invitations', account?.uuid];
  const refreshAfterResponse = async () => {
    setRefreshFailed(false);
    setActionError(null);
    try {
      await Promise.all([refresh(), queryClient.invalidateQueries({ queryKey, exact: true })]);
    } catch (cause) {
      setRefreshFailed(true);
      setActionError(invitationErrorMessage(cause, '邀请已处理，但账户信息刷新失败，请重试刷新。'));
    }
  };
  const respond = async (invitation: Invitation, action: 'accept' | 'decline') => {
    if (pending) return;
    setPending(invitation.id);
    setActionError(null);
    try {
      const result = await respondToInvitation(invitation.id, action);
      // 先隔离在途旧列表，防止其在写请求完成后重新填入已处理邀请。
      await queryClient.cancelQueries({ queryKey, exact: true });
      queryClient.setQueryData<{ data: Invitation[] }>(queryKey, (current) => ({
        data: current?.data.filter((item) => item.id !== result.id) ?? [],
      }));
      if (action === 'accept') {
        setAccepted((current) => [...current.filter((item) => item.id !== invitation.id), invitation]);
      }
      await refreshAfterResponse();
    } catch (cause) {
      setActionError(invitationErrorMessage(cause));
    } finally {
      setPending(null);
    }
  };
  return (
    <div className="max-h-[60vh] space-y-3 overflow-y-auto">
      {loading && <p role="status">正在加载邀请…</p>}
      {error ? (
        <div role="alert">
          {invitationErrorMessage(error, '加载邀请失败。')}
          <Button variant="outline" onClick={retry}>
            重试
          </Button>
        </div>
      ) : null}
      {actionError && (
        <p role="alert" className="text-destructive">
          {actionError}
        </p>
      )}
      {refreshFailed && (
        <Button variant="outline" onClick={() => void refreshAfterResponse()}>
          重试刷新
        </Button>
      )}
      {!loading && !error && !invitations?.length && <p>没有待处理邀请。</p>}
      {invitations?.map((invitation) => (
        <div key={invitation.id} className={standalone ? 'space-y-4 py-3' : 'space-y-2 rounded-md border p-3'}>
          <p className="break-words font-medium">
            {standalone && '管理员邀请你加入 '}
            {invitation.organization_name}
          </p>
          <p className="text-xs text-muted-foreground">
            {organizationRoleLabel(invitation.role)} · 有效期至 {new Date(invitation.expires_at).toLocaleString()}
          </p>
          <div className={standalone ? 'mx-auto flex max-w-xs flex-col gap-2' : 'flex gap-2'}>
            <Button disabled={Boolean(pending)} onClick={() => void respond(invitation, 'accept')}>
              接受
            </Button>
            <Button
              variant={standalone ? 'link' : 'outline'}
              disabled={Boolean(pending)}
              onClick={() => void respond(invitation, 'decline')}
            >
              拒绝
            </Button>
          </div>
        </div>
      ))}
      {accepted.map((invitation) => (
        <div key={invitation.id} className="flex items-center justify-between gap-2 rounded-md border p-3">
          <span className="min-w-0 truncate">已加入 {invitation.organization_name}</span>
          <Button variant="outline" onClick={() => enter(invitation.organization_uuid)}>
            进入组织
          </Button>
        </div>
      ))}
    </div>
  );
}

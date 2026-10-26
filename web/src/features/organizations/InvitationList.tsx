import { useI18n, useFormatters } from '../../shared/i18n';
import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useAuth } from '../../shared/auth/context';
import { Button } from '../../shared/ui/button';
import { invitationErrorMessage, organizationRoleLabel, respondToInvitation, type Invitation } from './api';

export function InvitationList({
  invitations,
  error,
  loading,
  retry,
  onResolved,
  standalone = false,
}: {
  invitations?: Invitation[];
  error: unknown;
  loading: boolean;
  retry: () => void;
  onResolved?: (action: 'accept' | 'decline' | null, remaining: number) => Promise<void>;
  standalone?: boolean;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const { account, refresh } = useAuth();
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [completedAction, setCompletedAction] = useState<'accept' | 'decline' | null>(null);
  const [refreshFailed, setRefreshFailed] = useState(false);
  const queryKey = ['invitations', account?.uuid];
  const refreshAfterResponse = async (action = completedAction) => {
    setRefreshFailed(false);
    setActionError(null);
    try {
      await Promise.all([refresh(), queryClient.invalidateQueries({ queryKey, exact: true }, { throwOnError: true })]);
      const remaining = queryClient.getQueryData<{ data: Invitation[] }>(queryKey)?.data.length;
      if (remaining !== undefined) await onResolved?.(action, remaining);
      setCompletedAction(null);
    } catch (cause) {
      setRefreshFailed(true);
      setActionError(
        invitationErrorMessage(
          cause,
          msg(
            'invitations.refreshFailed',
            'The invitation was processed, but account information could not be refreshed. Retry the refresh.',
          ),
          msg,
        ),
      );
    }
  };
  useEffect(() => {
    if (!standalone || loading || error || pending || completedAction || refreshFailed || invitations?.length !== 0)
      return;
    void onResolved?.(null, 0).catch((cause) => {
      setRefreshFailed(true);
      setActionError(
        invitationErrorMessage(
          cause,
          msg('invitations.returnFailed', 'Could not return to the console. Try again.'),
          msg,
        ),
      );
    });
  }, [standalone, loading, error, pending, completedAction, refreshFailed, invitations, onResolved, msg]);
  const respond = async (invitation: Invitation, action: 'accept' | 'decline') => {
    if (pending || refreshFailed) return;
    setPending(invitation.id);
    setActionError(null);
    try {
      const result = await respondToInvitation(invitation.id, action);
      setCompletedAction(action);
      await queryClient.cancelQueries({ queryKey, exact: true });
      queryClient.setQueryData<{ data: Invitation[] }>(queryKey, (current) => ({
        data: current?.data.filter((item) => item.id !== result.id) ?? [],
      }));
      await refreshAfterResponse(action);
    } catch (cause) {
      setActionError(invitationErrorMessage(cause, undefined, msg));
    } finally {
      setPending(null);
    }
  };
  return (
    <div
      className={`subtle-scrollbar min-h-0 min-w-0 space-y-3 overflow-y-auto text-sm [overflow-wrap:anywhere] ${standalone ? 'max-h-[60vh]' : ''}`}
    >
      {loading && <p role="status">{msg('invitations.loading', 'Loading invitations…')}</p>}
      {error ? (
        <div role="alert" className="space-y-3">
          <p>{invitationErrorMessage(error, msg('invitations.loadFailed', 'Invitations could not be loaded.'), msg)}</p>
          <Button variant="outline" onClick={retry}>
            {msg('common.retry', 'Retry')}
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
          {msg('invitations.retryRefresh', 'Retry refresh')}
        </Button>
      )}
      {!loading && !error && !invitations?.length && <p>{msg('invitations.empty', 'No pending invitations.')}</p>}
      {invitations?.map((invitation) => (
        <div
          key={invitation.id}
          className={standalone ? 'space-y-4 py-3' : 'space-y-2 rounded-md border-[0.5px] border-border p-3'}
        >
          <p className="break-words font-medium">
            {standalone
              ? msg('invitations.invitedToJoin', 'An administrator invited you to join {organization}.', {
                  organization: invitation.organization_name,
                })
              : invitation.organization_name}
          </p>
          <div
            className={`flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground ${standalone ? 'justify-center' : ''}`}
          >
            <span>{organizationRoleLabel(invitation.role, msg)}</span>
            <span>
              {msg('invitations.expiresAt', 'Expires {date}', {
                date: formatters.date(invitation.expires_at, { dateStyle: 'medium', timeStyle: 'short' }),
              })}
            </span>
          </div>
          <div className={standalone ? 'mx-auto flex max-w-xs flex-col gap-2' : 'flex gap-2'}>
            <Button disabled={Boolean(pending) || refreshFailed} onClick={() => void respond(invitation, 'accept')}>
              {msg('invitations.accept', 'Accept')}
            </Button>
            <Button
              variant={standalone ? 'link' : 'outline'}
              disabled={Boolean(pending) || refreshFailed}
              onClick={() => void respond(invitation, 'decline')}
            >
              {msg('invitations.decline', 'Decline')}
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}

import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Outlet, useLocation, useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../../shared/auth/context';
import { returnToFromSearch } from '../../shared/auth/redirects';
import { Button } from '../../shared/ui/button';
import { invitationReturnTo, listInvitations } from './api';

// 每次打开应用、每个登录身份只做一次前置检查，避免打断正在进行的业务操作。
export function InvitationEntryGate() {
  const { status, account } = useAuth();
  if (status !== 'authenticated' || !account) return <Outlet />;
  return (
    <AuthenticatedInvitationEntry key={account.uuid} accountUuid={account.uuid}>
      <Outlet />
    </AuthenticatedInvitationEntry>
  );
}

function AuthenticatedInvitationEntry({ accountUuid, children }: { accountUuid: string; children: ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const [checked, setChecked] = useState(location.pathname === '/invites');
  const redirecting = useRef(false);
  const [navigationError, setNavigationError] = useState(false);
  const invitations = useQuery({
    queryKey: ['invitations', accountUuid],
    queryFn: listInvitations,
    enabled: !checked,
    retry: false,
    refetchOnMount: 'always',
  });
  useEffect(() => {
    if (checked || redirecting.current || !invitations.isFetchedAfterMount || invitations.isFetching) return;
    if (invitations.isError) return;
    if (!invitations.data?.data.length) {
      setChecked(true);
      return;
    }
    redirecting.current = true;
    const target = location.pathname === '/login' ? returnToFromSearch(location.searchStr) : location.href;
    void navigate({ to: '/invites', search: { returnTo: invitationReturnTo(target) }, replace: true })
      .then(() => setChecked(true))
      .catch(() => {
        redirecting.current = false;
        setNavigationError(true);
      });
  }, [
    checked,
    invitations.isFetchedAfterMount,
    invitations.isFetching,
    invitations.isError,
    invitations.data,
    location,
    navigate,
  ]);

  if (checked) return children;
  if (invitations.isError || navigationError)
    return (
      <main className="grid min-h-screen place-items-center bg-background p-6 text-foreground">
        <div role="alert" className="space-y-4 text-center">
          <p>组织邀请检查失败。</p>
          <Button
            onClick={() => {
              setNavigationError(false);
              void invitations.refetch();
            }}
          >
            重试
          </Button>
          <Button variant="ghost" onClick={() => setChecked(true)}>
            继续控制台
          </Button>
        </div>
      </main>
    );
  return (
    <main role="status" className="grid min-h-screen place-items-center bg-background text-foreground">
      正在检查组织邀请…
    </main>
  );
}

import { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../../shared/auth/context';
import { loginHrefForReturnTo, returnToFromSearch } from '../../shared/auth/redirects';
import { useOrganizations } from '../../shared/organizations/context';
import { Button } from '../../shared/ui/button';
import { InvitationList } from './InvitationList';
import { invitationReturnTo, listInvitations } from './api';

export function InvitationsPage() {
  const { status, refresh } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const redirecting = useRef(false);
  useEffect(() => {
    if (status !== 'anonymous' || redirecting.current) return;
    redirecting.current = true;
    void navigate({ href: loginHrefForReturnTo(location.href), replace: true });
  }, [status, location.href, navigate]);
  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="px-6 py-8 text-center text-lg font-semibold">Open Managed Agents</header>
      <main className="flex flex-1 items-center justify-center px-6 pb-24">
        <section className="w-full max-w-md space-y-6 text-center" aria-label="组织邀请">
          {status === 'authenticated' ? (
            <AuthenticatedInvitationsPage />
          ) : status === 'error' ? (
            <div role="alert">
              登录信息加载失败。<Button onClick={() => void refresh().catch(() => undefined)}>重试</Button>
            </div>
          ) : (
            <p role="status">正在验证登录身份…</p>
          )}
        </section>
      </main>
    </div>
  );
}

function AuthenticatedInvitationsPage() {
  const [entryFailed, setEntryFailed] = useState(false);
  const { account, logout } = useAuth();
  const organizations = useOrganizations();
  const location = useLocation();
  const navigate = useNavigate();
  const invitations = useQuery({ queryKey: ['invitations', account?.uuid], queryFn: listInvitations, retry: false });
  const returnTo = invitationReturnTo(returnToFromSearch(location.searchStr));
  const enter = async (orgUuid: string) => {
    if (!organizations) return;
    setEntryFailed(false);
    if (!(await organizations.switchOrganization(orgUuid))) {
      setEntryFailed(true);
      return;
    }
    // 加入目标组织后进入首页，不能携带旧组织的资源详情路径。
    await navigate({ href: '/', replace: true });
  };
  return (
    <>
      <h1 className="text-xl font-medium">接受组织邀请</h1>
      {entryFailed && <p role="alert">进入组织失败，请重试“进入组织”。</p>}
      <InvitationList
        invitations={invitations.data?.data}
        error={invitations.error}
        loading={invitations.isLoading}
        retry={() => void invitations.refetch()}
        enter={(orgUuid) => void enter(orgUuid)}
        standalone
      />
      <div className="flex flex-col items-center gap-2">
        <Button variant="ghost" onClick={() => void navigate({ href: returnTo, replace: true })}>
          {invitations.data?.data.length ? '稍后处理，返回控制台' : '返回控制台'}
        </Button>
        <Button variant="link" onClick={() => void logout()}>
          退出登录
        </Button>
      </div>
    </>
  );
}

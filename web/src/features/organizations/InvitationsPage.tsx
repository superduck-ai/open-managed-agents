import { useI18n } from '../../shared/i18n';
import { BrandLogo } from '@/shared/brand/BrandLogo';
import { useCallback, useEffect, useRef } from 'react';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../../shared/auth/context';
import { loginHrefForReturnTo, returnToFromSearch } from '../../shared/auth/redirects';
import { Button } from '../../shared/ui/button';
import { InvitationList } from './InvitationList';
import { invitationReturnTo, listInvitations } from './api';

export function InvitationsPage() {
  const { msg } = useI18n();
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
      <header className="flex justify-center px-6 py-8">
        <a href="/" aria-label="Open Managed Agents" className="rounded-sm">
          <BrandLogo decorative className="h-10 w-[119px]" />
        </a>
      </header>
      <main className="flex flex-1 items-center justify-center px-6 pb-24">
        <section
          className="w-full max-w-md space-y-6 text-center text-sm"
          aria-label={msg('invitations.title', 'Organization invitations')}
        >
          {status === 'authenticated' ? (
            <AuthenticatedInvitationsPage />
          ) : status === 'error' ? (
            <div role="alert" className="space-y-3">
              <p>{msg('invitations.auth.loadError', 'Sign-in information could not be loaded.')}</p>
              <Button onClick={() => void refresh().catch(() => undefined)}>{msg('common.retry', 'Retry')}</Button>
            </div>
          ) : (
            <p role="status">{msg('invitations.auth.checking', 'Checking sign-in status…')}</p>
          )}
        </section>
      </main>
    </div>
  );
}

function AuthenticatedInvitationsPage() {
  const { msg } = useI18n();
  const { account, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const invitations = useQuery({ queryKey: ['invitations', account?.uuid], queryFn: listInvitations, retry: false });
  const returnTo = invitationReturnTo(returnToFromSearch(location.searchStr));
  const onResolved = useCallback(
    async (action: 'accept' | 'decline' | null, remaining: number) => {
      if (action === 'accept' || remaining === 0) await navigate({ href: '/', replace: true });
    },
    [navigate],
  );
  return (
    <>
      <h1 className="text-2xl font-medium leading-8">
        {msg('invitations.page.title', 'Accept organization invitations')}
      </h1>
      <InvitationList
        invitations={invitations.data?.data}
        error={invitations.error}
        loading={invitations.isLoading}
        retry={() => void invitations.refetch()}
        onResolved={onResolved}
        standalone
      />
      <div className="flex flex-col items-center gap-2">
        <Button variant="ghost" onClick={() => void navigate({ href: returnTo, replace: true })}>
          {invitations.data?.data.length
            ? msg('invitations.page.later', 'Return to console')
            : msg('invitations.backToConsole', 'Return to console')}
        </Button>
        <Button variant="link" onClick={() => void logout()}>
          {msg('account.logout', 'Log out')}
        </Button>
      </div>
    </>
  );
}

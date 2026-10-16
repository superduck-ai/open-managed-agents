import { useI18n } from '../../shared/i18n';
import { useEffect, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { useAuth } from '../../shared/auth/context';
import { Button } from '../../shared/ui/button';
import { PlatformAdminShell } from './PlatformAdminShell';
import { PlatformAdminContext } from './context';
import type { PlatformBootstrap } from './types';

export function PlatformAdminLayout() {
  const { account } = useAuth();
  return <IsolatedAdmin key={account?.uuid ?? 'independent-session'} />;
}

function IsolatedAdmin() {
  const [queries] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } }));
  const [denied, setDenied] = useState(0);
  const [client] = useState(() => new PlatformAdminClient(setDenied));
  useEffect(
    () => () => {
      client.invalidate();
      queries.clear();
    },
    [client, queries],
  );
  useEffect(() => {
    if (denied) queries.clear();
  }, [denied, queries]);
  return (
    <QueryClientProvider client={queries}>
      <AdminGuard client={client} denied={denied} />
    </QueryClientProvider>
  );
}

function AdminGuard({ client, denied }: { client: PlatformAdminClient; denied: number }) {
  const { msg } = useI18n();

  const navigate = useNavigate();
  const query = useQuery({
    queryKey: ['platform-admin', 'bootstrap'],
    queryFn: async ({ signal }) => {
      const result = await client.request<PlatformBootstrap>('/bootstrap', { signal });
      client.setCSRFToken(result.csrf_token);
      return result;
    },
    enabled: !denied,
    refetchInterval: 30_000,
    refetchOnWindowFocus: 'always',
  });
  useEffect(() => {
    if (denied === 401) void navigate({ to: '/login', search: { returnTo: '/platform-admin' }, replace: true });
  }, [denied, navigate]);
  let message = '';
  if (denied === 401)
    message = msg(
      'platformAdmin.yourSessionExpiredRedirectingToSignIn',
      'Your session expired. Redirecting to sign in…',
    );
  else if (denied === 403)
    message = msg(
      'platformAdmin.thisAccountDoesNotHavePlatformAccessOrMust',
      'This account does not have platform access, or must sign in again with an email verification code.',
    );
  else if (query.isPending) message = msg('platformAdmin.checkingPlatformAccess', 'Checking platform access…');
  else if (query.isError)
    message = msg(
      'platformAdmin.platformAdministrationIsTemporarilyUnavailablePleaseTryAgain',
      'Platform administration is temporarily unavailable. Please try again.',
    );
  else if (!query.data?.platform_access.permissions.includes('platform:access'))
    message = msg('platformAdmin.thisAccountDoesNotHavePlatformAccess', 'This account does not have platform access.');
  if (message)
    return (
      <main className="grid min-h-screen place-items-center bg-background p-6 text-foreground">
        <div className="space-y-4 text-center">
          <p role={query.isPending && !denied ? 'status' : 'alert'}>{message}</p>
          {!denied && query.isError && (
            <Button onClick={() => void query.refetch()}>{msg('platformAdmin.retry', 'Retry')}</Button>
          )}
          <p>
            <Link to="/" className="underline">
              {msg('platformAdmin.backToConsole', 'Back to console')}
            </Link>
          </p>
        </div>
      </main>
    );
  if (!query.data) return null;
  return (
    <PlatformAdminContext.Provider value={{ client, bootstrap: query.data }}>
      <PlatformAdminShell email={query.data.account.email} />
    </PlatformAdminContext.Provider>
  );
}

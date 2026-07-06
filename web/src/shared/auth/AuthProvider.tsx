import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo, type ReactNode } from 'react';
import { fetchBootstrap, logout, type BootstrapResponse } from './api';
import { AuthContext, type AuthContextValue, type AuthStatus } from './context';

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const bootstrapQuery = useQuery({
    queryKey: ['auth', 'bootstrap'],
    queryFn: fetchBootstrap,
    retry: false
  });

  const account = bootstrapQuery.data?.account ?? null;
  const status: AuthStatus = bootstrapQuery.isLoading
    ? 'loading'
    : account
      ? 'authenticated'
      : 'anonymous';

  const value = useMemo<AuthContextValue>(
    () => ({
      account,
      status,
      csrfToken: bootstrapQuery.data?.csrf_token,
      refresh: async () => {
        const result = await bootstrapQuery.refetch();
        return result.data;
      },
      logout: async () => {
        await logout();
        queryClient.setQueryData<BootstrapResponse>(['auth', 'bootstrap'], {
          account: null
        });
      }
    }),
    [account, bootstrapQuery, queryClient, status]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

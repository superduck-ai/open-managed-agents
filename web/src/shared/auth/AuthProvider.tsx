import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, type ReactNode } from 'react';
import { cancelScopeRequests, onApiAuthFailure, setConsoleRequestContext, type ApiError } from '../api/client';
import { fetchBootstrap, logout, type BootstrapResponse } from './api';
import { AuthContext, type AuthContextValue, type AuthStatus } from './context';

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const bootstrapQuery = useQuery({
    queryKey: ['auth', 'bootstrap'],
    queryFn: fetchBootstrap,
    retry: false,
  });

  const account = bootstrapQuery.data?.account ?? null;
  useEffect(
    () =>
      onApiAuthFailure((status) => {
        if (status !== 401) return;
        cancelScopeRequests();
        setConsoleRequestContext({});
        queryClient.setQueryData(['auth', 'bootstrap'], { account: null });
      }),
    [queryClient],
  );
  const failed = bootstrapQuery.isError && (bootstrapQuery.error as unknown as ApiError).status !== 401;
  const status: AuthStatus = bootstrapQuery.isLoading
    ? 'loading'
    : account
      ? 'authenticated'
      : failed
        ? 'error'
        : 'anonymous';

  const value = useMemo<AuthContextValue>(
    () => ({
      account,
      status,
      csrfToken: bootstrapQuery.data?.csrf_token,
      refresh: async () => {
        const result = await bootstrapQuery.refetch({ throwOnError: true });
        return result.data;
      },
      logout: async () => {
        await logout();
        cancelScopeRequests();
        setConsoleRequestContext({});
        queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'auth' });
        queryClient.setQueryData<BootstrapResponse>(['auth', 'bootstrap'], {
          account: null,
        });
      },
    }),
    [account, bootstrapQuery, queryClient, status],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

import { createContext, useContext } from 'react';
import type { AuthAccount, BootstrapResponse } from './api';

export type AuthStatus = 'loading' | 'authenticated' | 'anonymous';

export type AuthContextValue = {
  account: AuthAccount | null;
  status: AuthStatus;
  csrfToken?: string;
  refresh: () => Promise<BootstrapResponse | undefined>;
  logout: () => Promise<void>;
};

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
}

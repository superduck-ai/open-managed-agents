import { createContext, useContext } from 'react';
import type { PlatformAdminClient } from '../../shared/api/platform-admin';
import type { PlatformBootstrap } from './types';

export const PlatformAdminContext = createContext<{ client: PlatformAdminClient; bootstrap: PlatformBootstrap } | null>(
  null,
);
export function usePlatformAdmin() {
  const value = useContext(PlatformAdminContext);
  if (!value) throw new Error('Platform administration context is unavailable');
  return value;
}

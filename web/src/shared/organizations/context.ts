import { createContext, useContext } from 'react';
import type { AuthMembership } from '../auth/api';

export type OrganizationContextValue = {
  memberships: AuthMembership[];
  orgUuid?: string;
  switching: boolean;
  error: unknown;
  switchOrganization: (orgUuid: string) => Promise<void>;
  retry: () => Promise<void>;
};

export const OrganizationContext = createContext<OrganizationContextValue | null>(null);
export const useOrganizations = () => useContext(OrganizationContext);

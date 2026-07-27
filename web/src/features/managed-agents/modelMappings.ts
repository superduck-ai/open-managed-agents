import { useQuery } from '@tanstack/react-query';
import { getEffectiveModelMappings } from './api';

export function useEffectiveModelMappings(orgUuid?: string) {
  const organizationID = orgUuid ?? '';
  return useQuery({
    queryKey: ['managed-agents', 'model-mappings', organizationID],
    queryFn: () => getEffectiveModelMappings(organizationID),
    enabled: Boolean(organizationID),
    retry: false,
  });
}

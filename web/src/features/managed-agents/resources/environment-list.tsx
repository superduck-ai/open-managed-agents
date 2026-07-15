import { useFormatters, useI18n } from '../../../shared/i18n';
import { type ReactNode } from 'react';
import { StatusPill } from '../components/common';
import { type ManagedEntityApiResponse, type ManagedEntitySection } from '../types';
import { cellsForEntity, entityDisplayName } from './model';
import { localizedRelativeTime } from './environment-model';

export function useManagedEntityCells(
  section: ManagedEntitySection,
  entity: ManagedEntityApiResponse,
): Record<string, ReactNode> {
  const { msg } = useI18n();
  const formatters = useFormatters();
  if (section !== 'environments') {
    return cellsForEntity(section, entity);
  }
  return {
    Name: entityDisplayName(section, entity),
    Status: (
      <StatusPill>
        {entity.archived_at ? msg('common.archived', 'Archived') : msg('common.active', 'Active')}
      </StatusPill>
    ),
    Type: msg('managedAgents.environments.cloud', 'Cloud'),
    'Updated at': localizedRelativeTime(entity.updated_at, formatters.relativeTime),
  };
}

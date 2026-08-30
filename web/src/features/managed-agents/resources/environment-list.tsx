import { useFormatters, useI18n } from '../../../shared/i18n';
import { type ReactNode } from 'react';
import { StatusPill } from '../components/common';
import { type ManagedEntityApiResponse, type ManagedEntitySection, type SessionApiResponse } from '../types';
import { objectRecord, optionalNumericValueFromKeys, sessionListCost } from '../utils';
import { cellsForEntity, entityDisplayName, statusPillTone } from './model';
import { localizedRelativeTime } from './environment-model';

export function useManagedEntityCells(
  section: ManagedEntitySection,
  entity: ManagedEntityApiResponse,
): Record<string, ReactNode> {
  const { msg } = useI18n();
  const formatters = useFormatters();
  if (section === 'sessions') {
    const session = entity as SessionApiResponse;
    const tokens = sessionListTokens(session);
    const cost = sessionListCost(session.usage) ?? sessionListCost(session.stats);
    return {
      ...cellsForEntity(section, entity, msg, formatters.relativeTime),
      'Tokens in / out': tokens ? (
        <span className="font-mono tabular-nums" title={`${tokens.input} / ${tokens.output}`}>
          {formatSessionListTokenCount(tokens.input, formatters)} /{' '}
          {formatSessionListTokenCount(tokens.output, formatters)}
        </span>
      ) : (
        '—'
      ),
      Cost: cost ? (
        <span className="font-mono tabular-nums">
          {formatters.currency(cost.amount, cost.currency, { maximumFractionDigits: 4 })}
        </span>
      ) : (
        '—'
      ),
    };
  }
  if (section !== 'environments') {
    return cellsForEntity(section, entity, msg, formatters.relativeTime);
  }
  return {
    Name: entityDisplayName(section, entity),
    Status: (
      <StatusPill tone={statusPillTone(section, entity)}>
        {entity.archived_at ? msg('common.archived', 'Archived') : msg('common.active', 'Active')}
      </StatusPill>
    ),
    Type: msg('managedAgents.environments.cloud', 'Cloud'),
    'Updated at': localizedRelativeTime(entity.updated_at, formatters.relativeTime),
  };
}

function sessionListTokens(session: SessionApiResponse) {
  const usage = objectRecord(session.usage);
  const stats = objectRecord(session.stats);
  const input =
    optionalNumericValueFromKeys(usage, ['input_tokens', 'inputTokens', 'tokens_in', 'tokensIn', 'input']) ??
    optionalNumericValueFromKeys(stats, ['input_tokens', 'inputTokens', 'tokens_in', 'tokensIn', 'input']) ??
    0;
  const output =
    optionalNumericValueFromKeys(usage, ['output_tokens', 'outputTokens', 'tokens_out', 'tokensOut', 'output']) ??
    optionalNumericValueFromKeys(stats, ['output_tokens', 'outputTokens', 'tokens_out', 'tokensOut', 'output']) ??
    0;
  const cacheRead =
    optionalNumericValueFromKeys(usage, ['cache_read_input_tokens', 'cacheReadInputTokens']) ??
    optionalNumericValueFromKeys(stats, ['cache_read_input_tokens', 'cacheReadInputTokens']) ??
    0;
  const cacheCreation =
    optionalNumericValueFromKeys(usage, ['cache_creation_input_tokens', 'cacheCreationInputTokens']) ??
    optionalNumericValueFromKeys(stats, ['cache_creation_input_tokens', 'cacheCreationInputTokens']) ??
    sessionCacheCreationTokens(usage);
  const totalInput = input + cacheRead + cacheCreation;
  return totalInput || output ? { input: totalInput, output } : null;
}

function sessionCacheCreationTokens(usage: Record<string, unknown>) {
  const cacheCreation = objectRecord(usage.cache_creation);
  return (
    (optionalNumericValueFromKeys(cacheCreation, ['ephemeral_5m_input_tokens']) ?? 0) +
    (optionalNumericValueFromKeys(cacheCreation, ['ephemeral_1h_input_tokens']) ?? 0)
  );
}

function formatSessionListTokenCount(value: number, formatters: ReturnType<typeof useFormatters>) {
  return value >= 1000 ? `${(value / 1000).toFixed(1)}k` : formatters.number(value);
}

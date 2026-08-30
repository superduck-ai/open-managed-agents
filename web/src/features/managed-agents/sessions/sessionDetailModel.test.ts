import { describe, expect, test } from 'bun:test';
import { type useFormatters } from '../../../shared/i18n';
import { type I18nMsg, type SessionApiResponse } from '../types';
import { Bot } from 'lucide-react';
import { buildSessionDetailSummary } from './sessionDetailModel';

const msg: I18nMsg = ((_key, fallback) => fallback) as I18nMsg;
const formatters = {
  bytes: (value: number) => `${value} B`,
  currency: (value: number) => `$${value}`,
  date: (value: string | number | Date) => String(value),
  number: (value: number) => String(value),
  relativeTime: (value: number, unit: Intl.RelativeTimeFormatUnit) => `${value} ${unit}`,
  time: (value: string | number | Date) => String(value),
} as ReturnType<typeof useFormatters>;

describe('Claude session header summary', () => {
  test('uses the Claude field order without resources or token totals', () => {
    const session: SessionApiResponse = {
      id: 'sesn_test',
      agent: { id: 'agent_test', name: 'Research agent', version: 3 },
      archived_at: null,
      created_at: '2026-08-29T00:00:00.000Z',
      environment_id: 'env_1234567890abcdefghijklmnop',
      resources: [{ id: 'file_test', type: 'file' }],
      status: 'idle',
      title: 'Session title',
      type: 'session',
      updated_at: '2026-08-29T00:01:00.000Z',
      usage: { input_tokens: 1_000, output_tokens: 200, list_cost: 0.42 },
      vault_ids: ['vault_1234567890abcdefghijklmnop'],
    };

    const summary = buildSessionDetailSummary(session, [], formatters, msg, Date.parse(session.updated_at));

    expect(summary.chips.map((chip) => chip.key)).toEqual([
      'agent',
      'environment',
      'vaults',
      'duration',
      'cost',
      'created',
    ]);
    expect(summary.chips[0]?.icon).toBe(Bot);
    expect(summary.chips.map((chip) => [chip.key, chip.tooltip])).toEqual([
      ['agent', undefined],
      ['environment', 'env_1234567890abcdefghijklmnop'],
      ['vaults', 'vault_1234567890abcdefghijklmnop'],
      ['duration', undefined],
      ['cost', undefined],
      ['created', session.created_at],
    ]);
  });

  test('formats structured list cost in the header', () => {
    const session: SessionApiResponse = {
      id: 'sesn_test',
      agent: { id: 'agent_test', name: 'Research agent', version: 3 },
      archived_at: null,
      created_at: '2026-08-29T00:00:00.000Z',
      environment_id: 'env_test',
      status: 'idle',
      title: 'Session title',
      type: 'session',
      updated_at: '2026-08-29T00:01:00.000Z',
      usage: { list_cost: { amount: '125', currency: 'USD' } },
    };

    const summary = buildSessionDetailSummary(session, [], formatters, msg, Date.parse(session.updated_at));

    expect(summary.chips.find((chip) => chip.key === 'cost')?.value).toBe('$1.25');
  });
});

import { describe, expect, test } from 'bun:test';
import {
  ALL_PROVIDERS_ID,
  addableModelId,
  catalogModels,
  filterCatalogModels,
  groupCatalogModels,
  modelsForProvider,
  providerHost,
  providerInput,
  readableError,
  resolvedProviderSelection,
} from './catalog';
import type { LLMProvider } from './api';

const dashScope = provider('llmprov_dash', 'DashScope', ['kimi-k2.5', 'glm-4.7']);
const moonshot = provider('llmprov_moon', 'Moonshot', ['moonshot-v1']);

describe('LLM model catalog helpers', () => {
  test('builds catalog rows from providers', () => {
    const fromProviders = catalogModels([dashScope, moonshot]);
    expect(fromProviders).toEqual([
      { id: 'kimi-k2.5', providerId: 'llmprov_dash', providerName: 'DashScope' },
      { id: 'glm-4.7', providerId: 'llmprov_dash', providerName: 'DashScope' },
      { id: 'moonshot-v1', providerId: 'llmprov_moon', providerName: 'Moonshot' },
    ]);
  });

  test('filters, scopes, and selects providers without changing model IDs', () => {
    const models = catalogModels([dashScope, moonshot]);
    expect(filterCatalogModels(models, ' KIMI ').map((model) => model.id)).toEqual(['kimi-k2.5']);
    expect(filterCatalogModels(models, 'moonshot').map((model) => model.id)).toEqual(['moonshot-v1']);
    expect(modelsForProvider(models, ALL_PROVIDERS_ID)).toEqual(models);
    expect(modelsForProvider(models, 'llmprov_dash').map((model) => model.id)).toEqual(['kimi-k2.5', 'glm-4.7']);
    expect(resolvedProviderSelection([dashScope], ALL_PROVIDERS_ID)).toBe(ALL_PROVIDERS_ID);
    expect(resolvedProviderSelection([dashScope], 'llmprov_dash')).toBe('llmprov_dash');
    expect(resolvedProviderSelection([dashScope], 'missing')).toBe('llmprov_dash');
    expect(resolvedProviderSelection([dashScope, moonshot], 'llmprov_moon')).toBe('llmprov_moon');
    expect(resolvedProviderSelection([dashScope, moonshot], 'missing')).toBe(ALL_PROVIDERS_ID);
  });

  test('groups catalog rows under providers', () => {
    const models = catalogModels([dashScope, moonshot]);
    expect(groupCatalogModels(models, [dashScope, moonshot], true)).toEqual([
      {
        providerId: 'llmprov_dash',
        providerName: 'DashScope',
        models: [
          { id: 'kimi-k2.5', providerId: 'llmprov_dash', providerName: 'DashScope' },
          { id: 'glm-4.7', providerId: 'llmprov_dash', providerName: 'DashScope' },
        ],
      },
      {
        providerId: 'llmprov_moon',
        providerName: 'Moonshot',
        models: [{ id: 'moonshot-v1', providerId: 'llmprov_moon', providerName: 'Moonshot' }],
      },
    ]);
    expect(groupCatalogModels(catalogModels([dashScope]), [dashScope, moonshot], true)).toEqual([
      {
        providerId: 'llmprov_dash',
        providerName: 'DashScope',
        models: [
          { id: 'kimi-k2.5', providerId: 'llmprov_dash', providerName: 'DashScope' },
          { id: 'glm-4.7', providerId: 'llmprov_dash', providerName: 'DashScope' },
        ],
      },
      {
        providerId: 'llmprov_moon',
        providerName: 'Moonshot',
        models: [],
      },
    ]);
    expect(groupCatalogModels(catalogModels([dashScope]), [dashScope, moonshot], false)).toHaveLength(1);
  });

  test('derives addable IDs, hosts, update payloads, and error text', () => {
    const models = catalogModels([dashScope]);
    expect(addableModelId('  qwen-max  ', models)).toBe('qwen-max');
    expect(addableModelId('kimi-k2.5', models)).toBe('');
    expect(addableModelId('   ', models)).toBe('');
    expect(providerHost('https://dashscope.aliyuncs.com/apps/anthropic')).toBe('dashscope.aliyuncs.com');
    expect(providerHost('not-a-url')).toBe('not-a-url');
    expect(providerInput(dashScope, ['kimi-k2.5'])).toEqual({
      name: 'DashScope',
      base_url: dashScope.base_url,
      model_ids: ['kimi-k2.5'],
    });
    expect(readableError({ code: 'llm_provider_name_conflict', message: 'wording changed' }, englishMessage)).toBe(
      'A provider with this name already exists.',
    );
    expect(
      readableError({ code: 'model_conflict', message: 'wording changed', modelId: 'glm-4.7' }, englishMessage),
    ).toBe('Model ID glm-4.7 is already configured by another provider.');
    expect(readableError({ message: 'upstream failed' }, englishMessage)).toBe('Request failed. Please try again.');
    expect(readableError('nope', englishMessage)).toBe('Request failed. Please try again.');
  });
});

function englishMessage(_id: string, defaultMessage: string, values?: Record<string, string>) {
  return Object.entries(values ?? {}).reduce(
    (message, [key, value]) => message.replace(`{${key}}`, value),
    defaultMessage,
  );
}

function provider(id: string, name: string, modelIds: string[]): LLMProvider {
  return {
    type: 'llm_provider',
    id,
    name,
    base_url: `https://${name.toLowerCase()}.example.com/anthropic`,
    has_api_key: true,
    api_key_last4: '1111',
    model_ids: modelIds,
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
  };
}

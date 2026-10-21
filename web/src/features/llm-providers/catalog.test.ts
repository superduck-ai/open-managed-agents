import { describe, expect, test } from 'bun:test';
import {
  ALL_PROVIDERS_ID,
  addableModelId,
  catalogModels,
  filterCatalogModels,
  groupCatalogModels,
  isValidSlug,
  modelsForProvider,
  providerHost,
  providerInput,
  readableError,
  resolvedProviderSelection,
  slugFromName,
} from './catalog';
import type { LLMProvider } from './api';

const dashScope = provider('llmprov_dash', 'DashScope', 'dashscope', ['kimi-k2.5', 'glm-4.7']);
const moonshot = provider('llmprov_moon', 'Moonshot', 'default', ['moonshot-v1']);
const kimiRow = {
  id: 'kimi-k2.5',
  qualifiedId: 'dashscope/kimi-k2.5',
  providerId: 'llmprov_dash',
  providerName: 'DashScope',
};
const glmRow = {
  id: 'glm-4.7',
  qualifiedId: 'dashscope/glm-4.7',
  providerId: 'llmprov_dash',
  providerName: 'DashScope',
};
const moonshotRow = {
  id: 'moonshot-v1',
  qualifiedId: 'default/moonshot-v1',
  providerId: 'llmprov_moon',
  providerName: 'Moonshot',
};

describe('LLM model catalog helpers', () => {
  test('keeps organization and workspace models with the same raw ID separately addressable', () => {
    const organization = provider('llmprov_org', 'Organization', 'organization', ['shared-model', 'org-only']);
    organization.scope = 'organization';
    const workspace = provider('llmprov_ws', 'Workspace', 'workspace', ['shared-model']);
    expect(catalogModels([organization, workspace])).toEqual([
      {
        id: 'shared-model',
        qualifiedId: 'organization/shared-model',
        providerId: 'llmprov_org',
        providerName: 'Organization',
      },
      { id: 'org-only', qualifiedId: 'organization/org-only', providerId: 'llmprov_org', providerName: 'Organization' },
      {
        id: 'shared-model',
        qualifiedId: 'workspace/shared-model',
        providerId: 'llmprov_ws',
        providerName: 'Workspace',
      },
    ]);
  });

  test('builds catalog rows from providers', () => {
    const fromProviders = catalogModels([dashScope, moonshot]);
    expect(fromProviders).toEqual([kimiRow, glmRow, moonshotRow]);
  });

  test('derives provider slugs from names', () => {
    expect(slugFromName('DashScope CN')).toBe('dashscope-cn');
    expect(slugFromName('  --Open AI.  ')).toBe('open-ai');
    expect(slugFromName('阿里云')).toBe('');
    expect(slugFromName('a'.repeat(40))).toBe('a'.repeat(32));
    expect(isValidSlug('dashscope-cn')).toBe(true);
    expect(isValidSlug('default-2')).toBe(true);
    expect(isValidSlug('Dash/Scope')).toBe(false);
    expect(isValidSlug('-dash')).toBe(false);
    expect(isValidSlug('')).toBe(false);
  });

  test('filters, scopes, and selects providers without changing model IDs', () => {
    const models = catalogModels([dashScope, moonshot]);
    expect(filterCatalogModels(models, ' KIMI ').map((model) => model.id)).toEqual(['kimi-k2.5']);
    expect(filterCatalogModels(models, 'moonshot').map((model) => model.id)).toEqual(['moonshot-v1']);
    expect(filterCatalogModels(models, 'dashscope/glm').map((model) => model.id)).toEqual(['glm-4.7']);
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
        models: [kimiRow, glmRow],
      },
      {
        providerId: 'llmprov_moon',
        providerName: 'Moonshot',
        models: [moonshotRow],
      },
    ]);
    expect(groupCatalogModels(catalogModels([dashScope]), [dashScope, moonshot], true)).toEqual([
      {
        providerId: 'llmprov_dash',
        providerName: 'DashScope',
        models: [kimiRow, glmRow],
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
    expect(addableModelId('  qwen-max  ', models)).toBe('  qwen-max  ');
    expect(addableModelId('kimi-k2.5', models)).toBe('');
    expect(addableModelId('   ', models)).toBe('   ');
    expect(addableModelId('', models)).toBe('');
    expect(providerHost('https://dashscope.aliyuncs.com/apps/anthropic')).toBe('dashscope.aliyuncs.com');
    expect(providerHost('not-a-url')).toBe('not-a-url');
    expect(providerInput(dashScope, ['kimi-k2.5'])).toEqual({
      name: 'DashScope',
      slug: 'dashscope',
      api_backend: dashScope.api_backend,
      base_url: dashScope.base_url,
      model_ids: ['kimi-k2.5'],
    });
    expect(readableError({ code: 'llm_provider_name_conflict', message: 'wording changed' }, englishMessage)).toBe(
      'A provider with this name already exists.',
    );
    expect(readableError({ code: 'llm_provider_slug_conflict', message: 'wording changed' }, englishMessage)).toBe(
      'A provider with this slug already exists.',
    );
    expect(readableError({ code: 'model_conflict', message: 'wording changed' }, englishMessage)).toBe(
      'Request failed. Please try again.',
    );
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

function provider(id: string, name: string, slug: string, modelIds: string[]): LLMProvider {
  return {
    type: 'llm_provider',
    id,
    name,
    slug,
    api_backend: 'messages',
    base_url: `https://${name.toLowerCase()}.example.com/anthropic`,
    has_api_key: true,
    api_key_last4: '1111',
    model_ids: modelIds,
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
  };
}

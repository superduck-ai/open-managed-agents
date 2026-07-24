import { describe, expect, test } from 'bun:test';
import { catalogModelIDs, isCatalogModelID, resolveCatalogDefaultModelID } from './model';

const models = [
  { model_name: 'gateway/alpha', display_name: 'Alpha' },
  { model_name: 'gateway/beta', display_name: 'Beta' },
];

describe('model catalog selection', () => {
  test('does not treat the first catalog entry as an implicit default', () => {
    expect(
      resolveCatalogDefaultModelID({
        models,
        default_prompt_settings: { model_name: 'gateway/alpha' },
        model_catalog: { default_available: false, stale: false },
      }),
    ).toBe('');
  });

  test('returns an explicitly configured default only while it is available', () => {
    expect(
      resolveCatalogDefaultModelID({
        models,
        default_prompt_settings: { model_name: 'gateway/beta' },
        model_catalog: { default_available: true, stale: false },
      }),
    ).toBe('gateway/beta');
    expect(
      resolveCatalogDefaultModelID({
        models,
        default_prompt_settings: { model_name: 'gateway/missing' },
        model_catalog: { default_available: true, stale: false },
      }),
    ).toBe('');
  });

  test('normalizes model ids without adding aliases or fallbacks', () => {
    expect(catalogModelIDs([...models, { model_name: 'gateway/alpha' }, { model_name: ' ' }])).toEqual([
      'gateway/alpha',
      'gateway/beta',
    ]);
    expect(isCatalogModelID('gateway/beta', models)).toBe(true);
    expect(isCatalogModelID('beta', models)).toBe(false);
  });
});

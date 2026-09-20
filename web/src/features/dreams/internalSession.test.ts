import { expect, test } from 'bun:test';

import { isDreamInternalSession } from './internalSession';

test('does not treat ordinary review Sessions as Dream internals', () => {
  expect(
    isDreamInternalSession({
      id: 'sesn_user',
      title: '记忆评测—08',
      metadata: { topic: 'taste' },
    }),
  ).toBe(false);
  expect(
    isDreamInternalSession({
      id: 'sesn_named',
      title: 'Dream weekend plan',
    }),
  ).toBe(false);
});

test('hides current and legacy Dream internal Sessions', () => {
  expect(
    isDreamInternalSession({
      id: 'sesn_meta',
      title: '记忆评测',
      metadata: { internal_kind: 'dream', dream_id: 'drm_test' },
    }),
  ).toBe(true);
  expect(
    isDreamInternalSession({
      id: 'sesn_current',
      title: 'Dream drm_02au9Lu4wC1UmbH1OiR9kgRG',
    }),
  ).toBe(true);
  expect(
    isDreamInternalSession({
      id: 'sesn_prep',
      title: 'Dream preparation drm_O1CEGbMz1xITrtqSt79YaQhv',
    }),
  ).toBe(true);
});

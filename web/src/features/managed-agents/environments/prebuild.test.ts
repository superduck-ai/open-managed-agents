import { expect, test } from 'bun:test';
import { prebuildIsActive, prebuildStepState, packagesKey, type EnvironmentPrebuild } from './prebuild';

test('package identity ignores manager grouping but preserves versions and installation order', () => {
  const packages = [
    { manager: 'pip', value: 'numpy' },
    { manager: 'npm', value: 'is-number@7.0.0' },
  ];
  expect(packagesKey(packages)).toBe(packagesKey([...packages].reverse()));
  expect(packagesKey(packages)).not.toBe(packagesKey([{ ...packages[1], value: 'is-number@6' }, packages[0]]));
  expect(
    packagesKey([
      { manager: 'pip', value: 'a' },
      { manager: 'pip', value: 'b' },
    ]),
  ).not.toBe(
    packagesKey([
      { manager: 'pip', value: 'b' },
      { manager: 'pip', value: 'a' },
    ]),
  );
  expect(packagesKey([{ manager: 'pip', value: '  ' }])).toBe(packagesKey([]));
});

test('completed stages remain complete while failures and uncertainty stop polling', () => {
  for (const [stage, state, image, template, active] of [
    ['image', 'failed', 'failed', 'idle', false],
    ['template', 'unknown', 'ready', 'unknown', false],
    ['image', 'cancelled', 'cancelled', 'idle', false],
    ['image', 'queued', 'queued', 'idle', true],
    ['template', 'running', 'ready', 'running', true],
    ['template', 'canceling', 'ready', 'canceling', true],
    ['template', 'ready', 'ready', 'ready', false],
  ] as const) {
    const prebuild = { stage, state } as EnvironmentPrebuild;
    expect(prebuildStepState(prebuild, 'image')).toBe(image);
    expect(prebuildStepState(prebuild, 'template')).toBe(template);
    expect(prebuildIsActive(state)).toBe(active);
  }
});

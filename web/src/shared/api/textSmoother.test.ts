import { describe, expect, test } from 'bun:test';
import { TextStreamSmoother } from './textSmoother';

describe('TextStreamSmoother', () => {
  test('reveals a large arrival over multiple updates', async () => {
    const clock = manualClock();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      now: clock.now,
      sleep: clock.sleep,
      initialCharsPerSecond: 90
    });
    const text = 'abcdefghijklmnopqrstuvwxyz';

    smoother.setTarget(text);

    expect(updates.length).toBeGreaterThan(0);
    expect(updates.at(-1)).not.toBe(text);
    const firstLength = updates.at(-1)?.length ?? 0;

    await clock.step(17);
    await clock.step(17);

    expect((updates.at(-1)?.length ?? 0)).toBeGreaterThan(firstLength);
    expect(updates.at(-1)).not.toBe(text);
    smoother.dispose();
  });

  test('finishes to the complete text after message stop', async () => {
    const clock = manualClock();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      now: clock.now,
      sleep: clock.sleep,
      initialCharsPerSecond: 60
    });
    const text = 'final smoothed workbench response';

    smoother.setTarget(text);
    const done = smoother.finish();
    for (let i = 0; i < 20; i += 1) {
      await clock.step(17);
    }
    await done;

    expect(updates.at(-1)).toBe(text);
    smoother.dispose();
  });

  test('flushes all received text when aborted', () => {
    const controller = new AbortController();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      signal: controller.signal
    });
    const text = 'abort should not hide arrived text';

    smoother.setTarget(text);
    controller.abort();

    expect(updates.at(-1)).toBe(text);
    smoother.dispose();
  });

  test('continues smoothing at the hidden-tab interval while the document is hidden', async () => {
    const clock = manualClock();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      now: clock.now,
      sleep: clock.sleep,
      isDocumentHidden: () => true
    });
    const text = 'hidden tab completion should still reveal progressively';

    smoother.setTarget(text);

    const firstLength = updates.at(-1)?.length ?? 0;
    expect(firstLength).toBeGreaterThan(0);
    expect(updates.at(-1)).not.toBe(text);
    expect(clock.sleepDurations.at(-1)).toBe(200);

    await clock.step(200);

    expect((updates.at(-1)?.length ?? 0)).toBeGreaterThan(firstLength);
    expect(updates.at(-1)).not.toBe(text);
    expect(clock.sleepDurations.at(-1)).toBe(200);
    smoother.dispose();
  });

  test('caps visible burst reveal size after a delayed frame', async () => {
    const clock = manualClock();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      now: clock.now,
      sleep: clock.sleep,
      initialCharsPerSecond: 5000,
      maxCharsPerSecond: 5000,
      maxVisibleCharsPerStep: 12
    });

    smoother.setTarget('x'.repeat(1000));
    const firstLength = updates.at(-1)?.length ?? 0;

    await clock.step(1000);

    expect((updates.at(-1)?.length ?? 0) - firstLength).toBeLessThanOrEqual(12);
    smoother.dispose();
  });

  test('caps hidden burst reveal size when browser timers are clamped', async () => {
    const clock = manualClock();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      now: clock.now,
      sleep: clock.sleep,
      isDocumentHidden: () => true,
      initialCharsPerSecond: 5000,
      maxCharsPerSecond: 5000,
      maxHiddenCharsPerStep: 60
    });

    smoother.setTarget('x'.repeat(1000));
    const firstLength = updates.at(-1)?.length ?? 0;

    await clock.step(1000);

    expect((updates.at(-1)?.length ?? 0) - firstLength).toBeLessThanOrEqual(60);
    expect(clock.sleepDurations.at(-1)).toBe(200);
    smoother.dispose();
  });

  test('uses the offscreen interval when output is not visible', () => {
    const clock = manualClock();
    const smoother = new TextStreamSmoother({
      onUpdate: () => undefined,
      now: clock.now,
      sleep: clock.sleep,
      isOutputVisible: () => false
    });

    smoother.setTarget('offscreen throttled text');

    expect(clock.sleepDurations.at(-1)).toBe(100);
    smoother.dispose();
  });

  test('ignores an older generation after reset', async () => {
    const clock = manualClock();
    const updates: string[] = [];
    const smoother = new TextStreamSmoother({
      onUpdate: (text) => updates.push(text),
      now: clock.now,
      sleep: clock.sleep,
      initialCharsPerSecond: 80
    });

    smoother.setTarget('old generation text');
    smoother.reset();
    smoother.setTarget('new');
    const afterReset = updates.at(-1);

    await clock.step(17);

    expect(updates.at(-1)).toBe(afterReset);
    expect(updates.some((text) => text.includes('old generation tex'))).toBe(false);
    smoother.dispose();
  });
});

function manualClock() {
  let current = 0;
  const pending: Array<() => void> = [];
  const sleepDurations: number[] = [];
  return {
    sleepDurations,
    now: () => current,
    sleep: (ms: number) => {
      sleepDurations.push(ms);
      return new Promise<void>((resolve) => {
        pending.push(resolve);
      });
    },
    step: async (ms: number) => {
      current += ms;
      pending.shift()?.();
      await Promise.resolve();
      await Promise.resolve();
    }
  };
}

import { afterEach, expect, spyOn, test } from 'bun:test';
import { act, cleanup, renderHook } from '@testing-library/react';
import { useBuilderExamples } from './useBuilderExamples';

afterEach(cleanup);

test('examples rotate only when empty and unfocused, freezing the visible example immediately on focus', () => {
  let tick = () => {};
  const timer = spyOn(globalThis, 'setInterval').mockImplementation((callback) => {
    tick = callback as () => void;
    return 1 as unknown as ReturnType<typeof setInterval>;
  });
  const clear = spyOn(globalThis, 'clearInterval').mockImplementation(() => {});
  try {
    const { result, rerender, unmount } = renderHook(({ enabled }) => useBuilderExamples(enabled, true), {
      initialProps: { enabled: true },
    });
    const first = result.current.example;
    act(() => tick());
    const second = result.current.example;
    expect(second).not.toBe(first);
    act(() => {
      result.current.onFocus();
      tick();
    });
    expect(result.current.example).toBe(second);
    expect(clear).toHaveBeenCalled();
    act(() => result.current.onBlur());
    act(() => tick());
    expect(result.current.example).not.toBe(second);
    const calls = timer.mock.calls.length;
    rerender({ enabled: false });
    expect(timer.mock.calls.length).toBe(calls);
    unmount();
  } finally {
    timer.mockRestore();
    clear.mockRestore();
  }
});

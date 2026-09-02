import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { act, cleanup, render } from '@testing-library/react';
import { ScrollArea } from './scroll-area';

afterEach(cleanup);

async function flushScrollAreaEffects() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

test('composes a Base UI scroll area with an overlay viewport', async () => {
  const result = render(
    <ScrollArea className="h-20 w-20">
      <div className="h-40">Scrollable content</div>
    </ScrollArea>,
  );
  await flushScrollAreaEffects();

  const root = result.container.querySelector('[data-slot="scroll-area"]');
  expect(root?.className).toContain('overflow-hidden');
  expect(root?.querySelector('[data-slot="scroll-area-viewport"]')).toBeTruthy();
  expect(root?.querySelector('[data-slot="scroll-area-content"]')).toBeTruthy();
});

test('supports several independently owned scroll viewports', async () => {
  const result = render(
    <div>
      {Array.from({ length: 5 }, (_, index) => (
        <ScrollArea key={index} className="h-20 w-20">
          <div className="h-40">Scrollable content {index}</div>
        </ScrollArea>
      ))}
    </div>,
  );
  await flushScrollAreaEffects();

  expect(result.container.querySelectorAll('[data-slot="scroll-area-viewport"]')).toHaveLength(5);
});

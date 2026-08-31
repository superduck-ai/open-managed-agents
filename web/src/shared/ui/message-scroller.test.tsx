import { afterEach, expect, mock, test } from 'bun:test';
import '../../test/setup';
import {
  MessageScroller,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from './message-scroller';

const { act, cleanup, fireEvent, render } = await import('@testing-library/react');

afterEach(cleanup);

test('rearms follow-output after a gesture that leaves the viewport at the live edge', async () => {
  const result = render(
    <MessageScrollerProvider autoScroll defaultScrollPosition="end">
      <MessageScroller>
        <MessageScrollerViewport>
          <MessageScrollerContent>
            <MessageScrollerItem messageId="message-1">Message</MessageScrollerItem>
          </MessageScrollerContent>
        </MessageScrollerViewport>
      </MessageScroller>
    </MessageScrollerProvider>,
  );
  await nextFrame();

  const viewport = result.container.querySelector<HTMLElement>('[data-slot="message-scroller-viewport"]')!;
  Object.defineProperties(viewport, {
    clientHeight: { configurable: true, value: 100 },
    scrollHeight: { configurable: true, value: 200 },
  });
  const scrollTo = mock(({ top }: ScrollToOptions) => {
    viewport.scrollTop = Number(top);
  });
  Object.defineProperty(viewport, 'scrollTo', { configurable: true, value: scrollTo });

  viewport.scrollTop = 99;
  fireEvent.wheel(viewport, { deltaY: 40 });
  await nextFrame();
  expect(scrollTo).toHaveBeenCalledWith({ behavior: 'auto', top: 100 });

  scrollTo.mockClear();
  viewport.scrollTop = 80;
  fireEvent.wheel(viewport, { deltaY: -40 });
  await nextFrame();
  expect(scrollTo).not.toHaveBeenCalled();
});

async function nextFrame() {
  await act(async () => {
    await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
  });
}

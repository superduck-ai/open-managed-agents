import { afterEach, describe, expect, test } from 'bun:test';
import { type ReactNode } from 'react';
import { resetTestDom } from '../../../test/setup';

const testingLibrary = await import('@testing-library/react');
const { act, cleanup, fireEvent, render, screen, waitFor } = testingLibrary;
const { SessionTraceWorkspaceLayout } = await import('./SessionTraceWorkspaceLayout');

afterEach(() => {
  cleanup();
});

describe('SessionTraceWorkspaceLayout', () => {
  test('keeps the primary DOM mounted while switching between split and overlay layouts', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    let containerWidth = 1200;
    const originalRect = HTMLElement.prototype.getBoundingClientRect;
    HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect() {
      if (
        this.getAttribute('data-testid') === 'session-trace-layout-host' ||
        this.getAttribute('data-slot') === 'resizable-panel-group'
      ) {
        return layoutRect(containerWidth, 800);
      }
      return originalRect.call(this);
    };

    try {
      const view = render(<LayoutHarness primary={<input aria-label="Draft" defaultValue="kept draft" />} />);

      const group = await waitFor(() => {
        const element = document.querySelector('[data-slot="resizable-panel-group"]');
        expect(element?.getAttribute('data-layout-mode')).toBe('split');
        return element;
      });
      const draft = screen.getByRole('textbox', { name: 'Draft' }) as HTMLInputElement;
      fireEvent.input(draft, { target: { value: 'edited draft' } });
      draft.focus();

      containerWidth = 900;
      await act(async () => window.dispatchEvent(new window.Event('resize')));
      await waitFor(() => expect(group.getAttribute('data-layout-mode')).toBe('overlay'));

      expect(view.container.querySelector('input[aria-label="Draft"]')).toBe(draft);
      expect(draft.value).toBe('edited draft');
      expect(document.querySelector('[data-session-trace-primary-viewport]')?.getAttribute('aria-hidden')).toBe('true');
      expect(document.querySelector('[data-session-trace-primary-viewport]')?.hasAttribute('inert')).toBe(true);
      expect(document.querySelector('[data-session-trace-inspector-viewport]')?.classList.contains('absolute')).toBe(
        true,
      );

      containerWidth = 1200;
      await act(async () => window.dispatchEvent(new window.Event('resize')));
      await waitFor(() => expect(group.getAttribute('data-layout-mode')).toBe('split'));

      expect(screen.getByRole('textbox', { name: 'Draft' })).toBe(draft);
      expect(draft.value).toBe('edited draft');
      expect(document.querySelector('[data-session-trace-primary-viewport]')?.hasAttribute('inert')).toBe(false);
    } finally {
      HTMLElement.prototype.getBoundingClientRect = originalRect;
    }
  });

  test('hides the resize handle and inspector without unmounting primary content when closed', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_test');
    const originalRect = HTMLElement.prototype.getBoundingClientRect;
    HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect() {
      if (this.getAttribute('data-testid') === 'session-trace-layout-host') {
        return layoutRect(1200, 800);
      }
      return originalRect.call(this);
    };

    try {
      const view = render(<LayoutHarness inspectorOpen />);
      const primary = screen.getByTestId('primary-content');
      expect(await screen.findByRole('separator', { name: 'Resize inspector' })).toBeTruthy();

      view.rerender(<LayoutHarness inspectorOpen={false} />);

      expect(screen.getByTestId('primary-content')).toBe(primary);
      expect(screen.queryByRole('separator', { name: 'Resize inspector' })).toBeNull();
      expect(document.querySelector('[data-session-trace-inspector-viewport]')?.classList.contains('hidden')).toBe(
        true,
      );

      view.rerender(<LayoutHarness inspectorOpen />);

      expect(screen.getByTestId('primary-content')).toBe(primary);
      expect(await screen.findByRole('separator', { name: 'Resize inspector' })).toBeTruthy();
      expect(document.querySelector('[data-session-trace-inspector-viewport]')?.classList.contains('hidden')).toBe(
        false,
      );
    } finally {
      HTMLElement.prototype.getBoundingClientRect = originalRect;
    }
  });
});

function LayoutHarness({
  inspectorOpen = true,
  primary = <div data-testid="primary-content">Primary</div>,
}: {
  inspectorOpen?: boolean;
  primary?: ReactNode;
}) {
  return (
    <div style={{ width: 1200, height: 800 }}>
      <SessionTraceWorkspaceLayout
        primary={primary}
        inspector={
          <>
            <button data-inspector-close="">Close inspector</button>
            <button>Inspector action</button>
          </>
        }
        inspectorOpen={inspectorOpen}
        resizeLabel="Resize inspector"
      />
    </div>
  );
}

function layoutRect(width: number, height: number): DOMRect {
  return new DOMRect(0, 0, width, height);
}

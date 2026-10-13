import { afterEach, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { ResourceIdButton } from './ResourceIdButton';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
const commandDescriptor = Object.getOwnPropertyDescriptor(document, 'execCommand');

afterEach(() => {
  cleanup();
  if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor);
  else Reflect.deleteProperty(navigator, 'clipboard');
  if (commandDescriptor) Object.defineProperty(document, 'execCommand', commandDescriptor);
  else Reflect.deleteProperty(document, 'execCommand');
});

function renderId(value: string, displayValue?: string) {
  return render(
    <I18nProvider initialLocale="en">
      <TooltipProvider>
        <ResourceIdButton value={value} displayValue={displayValue} />
      </TooltipProvider>
    </I18nProvider>,
  );
}

test('keeps the full ID in the DOM and copies the original value even when its label differs', async () => {
  const id = 'sesn_QNi1234567890abcdefghijklmnopqrstuvwxyzN6oqpu';
  const writeText = mock(async (_value: string) => undefined);
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  const view = renderId(id);
  const button = screen.getByRole('button', { name: `Copy ${id}` });
  expect(button.textContent).toBe(id);
  fireEvent.click(button);
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(id));
  await waitFor(() => expect(button.querySelector('.lucide-check')).not.toBeNull());
  view.unmount();
  renderId(id, 'sesn_QNi...N6oqpu');
  fireEvent.click(screen.getByRole('button', { name: `Copy ${id}` }));
  await waitFor(() => expect(writeText).toHaveBeenCalledTimes(2));
  expect(writeText.mock.calls[1]?.[0]).toBe(id);
});

test.each(['throw', 'false'])(
  'does not show a copied state when the clipboard fallback returns %s',
  async (failure) => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error('denied');
        },
      },
    });
    const execCommand = mock(() => {
      if (failure === 'throw') throw new Error('unsupported');
      return false;
    });
    Object.defineProperty(document, 'execCommand', {
      configurable: true,
      value: execCommand,
    });
    renderId('sesn_failed');
    const button = screen.getByRole('button', { name: 'Copy sesn_failed' });
    fireEvent.click(button);
    await waitFor(() => expect(execCommand).toHaveBeenCalled());
    expect(button.querySelector('.lucide-copy')).not.toBeNull();
    expect(button.querySelector('.lucide-check')).toBeNull();
    expect(document.querySelector('textarea')).toBeNull();
  },
);

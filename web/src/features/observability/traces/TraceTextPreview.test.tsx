import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';
import { TraceIOPanels, TraceTextPreview } from './TraceTextPreview';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

let copied: string[] = [];

beforeEach(() => {
  copied = [];
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: {
      writeText: (value: string) => {
        copied.push(value);
        return Promise.resolve();
      },
    },
  });
});

function renderWithI18n(node: React.ReactElement) {
  resetTestDom('https://oma.duck.ai/workspaces/default/observability');
  return render(<I18nProvider initialLocale="en">{node}</I18nProvider>);
}

function openPreview(trigger: HTMLElement) {
  fireEvent.pointerDown(trigger);
  fireEvent.mouseDown(trigger);
  fireEvent.pointerUp(trigger);
  fireEvent.mouseUp(trigger);
  fireEvent.click(trigger);
}

describe('TraceTextPreview', () => {
  test('clicking the preview opens the full text without copying', async () => {
    renderWithI18n(<TraceTextPreview value="line one\nline two" />);

    openPreview(screen.getByRole('button', { name: 'Show full text' }));

    expect(await screen.findByRole('button', { name: 'Copy' })).toBeTruthy();
    expect(copied).toEqual([]);
  });

  test('the explicit copy button copies the full text', async () => {
    renderWithI18n(<TraceTextPreview value="  full text  " />);

    openPreview(screen.getByRole('button', { name: 'Show full text' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Copy' }));

    expect(copied).toEqual(['full text']);
  });
});

describe('TraceIOPanels', () => {
  test('renders an empty placeholder without a copy button when a side has no text', () => {
    renderWithI18n(<TraceIOPanels input="" output="" />);

    expect(screen.getByText('Input')).toBeTruthy();
    expect(screen.getByText('Output')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Copy' })).toBeNull();
  });

  test('shows full input and output text with per-panel copy', () => {
    renderWithI18n(<TraceIOPanels input="the input prompt" output="the model output" />);

    expect(screen.getByText('the input prompt')).toBeTruthy();
    expect(screen.getByText('the model output')).toBeTruthy();

    const copyButtons = screen.getAllByRole('button', { name: 'Copy' });
    expect(copyButtons.length).toBe(2);
    fireEvent.click(copyButtons[1]);
    expect(copied).toEqual(['the model output']);
  });
});

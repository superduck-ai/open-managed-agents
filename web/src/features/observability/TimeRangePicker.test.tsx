import { afterEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { defaultObservabilityFilters } from './model';
import { TimeRangePicker } from './TimeRangePicker';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

function timeRangeTrigger() {
  return screen.getByRole('button', { name: /Time range/ });
}

describe('TimeRangePicker', () => {
  test('shows absolute timestamps only for custom ranges', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/observability');
    const filters = defaultObservabilityFilters(1);
    const view = render(
      <I18nProvider initialLocale="en">
        <TimeRangePicker filters={filters} onChange={() => undefined} />
      </I18nProvider>,
    );

    expect(timeRangeTrigger().textContent).not.toContain('~');

    view.rerender(
      <I18nProvider initialLocale="en">
        <TimeRangePicker filters={{ ...filters, preset: 'custom' }} onChange={() => undefined} />
      </I18nProvider>,
    );

    expect(timeRangeTrigger().textContent).toContain('Custom');
    expect(timeRangeTrigger().textContent).toContain('~');
  });

  test('expands custom text fields below presets and applies a preset', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/observability');
    let changed = defaultObservabilityFilters(1);

    render(
      <I18nProvider initialLocale="en">
        <TimeRangePicker
          filters={changed}
          onChange={(next) => {
            changed = next;
          }}
        />
      </I18nProvider>,
    );

    fireEvent.click(timeRangeTrigger());

    expect(await screen.findByText('Quick ranges')).toBeTruthy();
    expect(screen.queryByLabelText('Start')).toBeNull();

    const customTrigger = screen.getByRole('button', { name: 'Custom' });
    fireEvent.click(customTrigger);

    const start = screen.getByLabelText<HTMLInputElement>('Start');
    expect(start.type).toBe('text');
    expect(start.value).toMatch(/^\d{4}-\d{2}-\d{2} /);
    expect(start.value).not.toContain('T');
    expect(screen.getByLabelText<HTMLInputElement>('End').type).toBe('text');
    expect(screen.getByText(/Shown in local time/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Apply' }).hasAttribute('disabled')).toBe(false);
    expect(screen.getByText('Quick ranges')).toBeTruthy();

    fireEvent.click(customTrigger);
    await waitFor(() => expect(screen.queryByLabelText('Start')).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: 'Last hour' }));

    expect(changed.preset).toBe('1h');
    await waitFor(() => expect(screen.queryByText('Quick ranges')).toBeNull());
  });

  test('keeps Apply enabled and shows a field error for an inverted range', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/observability');
    let changed = defaultObservabilityFilters(1);

    render(
      <I18nProvider initialLocale="en">
        <TimeRangePicker
          filters={changed}
          onChange={(next) => {
            changed = next;
          }}
        />
      </I18nProvider>,
    );

    fireEvent.click(timeRangeTrigger());
    fireEvent.click(screen.getByRole('button', { name: 'Custom' }));

    fireEvent.change(screen.getByLabelText('Start'), { target: { value: '2026-08-14 10:00:00' } });
    fireEvent.change(screen.getByLabelText('End'), { target: { value: '2026-08-13 10:00:00' } });
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    expect(await screen.findByText('End must be after start, within 30 days.')).toBeTruthy();
    expect(changed.preset).toBe('24h');
    expect(screen.getByLabelText('End').getAttribute('aria-invalid')).toBe('true');
  });
});

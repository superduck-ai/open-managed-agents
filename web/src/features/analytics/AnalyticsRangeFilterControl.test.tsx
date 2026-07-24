import { afterEach, describe, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { resetTestDom } from '../../test/setup';
import { I18nContext, type I18nContextValue } from '../../shared/i18n/context';
import { AnalyticsRangeFilterControl, type AnalyticsRangeFilter } from './AnalyticsRangeFilterControl';

const testingLibrary = await import('@testing-library/react');
const { act, cleanup, fireEvent, render, screen } = testingLibrary;

afterEach(cleanup);

resetTestDom('https://oma.duck.ai/');

const presets = [
  { value: 'last-7-days', label: 'Last 7 days' },
  { value: 'last-30-days', label: 'Last 30 days' },
];

const i18nContext: I18nContextValue = {
  locale: 'en',
  setLocale: () => undefined,
  msg: (_id, defaultMessage) => defaultMessage,
};

function I18nHarness({ children }: { children: ReactNode }) {
  return <I18nContext.Provider value={i18nContext}>{children}</I18nContext.Provider>;
}

function renderControl(value: AnalyticsRangeFilter, onChange = mock(() => {})) {
  return render(
    <I18nHarness>
      <AnalyticsRangeFilterControl label="Range" presets={presets} value={value} onChange={onChange} />
    </I18nHarness>,
  );
}

function clickDay(date: Date) {
  const day = date.toLocaleDateString();
  const button = document.querySelector(`[data-day="${day}"]`) as HTMLElement | null;
  if (!button) {
    throw new Error(`Day button not found for "${day}"`);
  }
  act(() => {
    fireEvent.click(button);
  });
}

function clickButton(name: RegExp) {
  act(() => {
    fireEvent.click(screen.getByRole('button', { name }));
  });
}

describe('AnalyticsRangeFilterControl', () => {
  test('closes popover and updates value when a preset is clicked', () => {
    const onChange = mock(() => {});
    renderControl({ kind: 'preset', value: 'last-7-days' }, onChange);

    clickButton(/Range/);

    const preset = screen.getByRole('radio', { name: 'Last 30 days' });
    act(() => {
      fireEvent.click(preset);
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ kind: 'preset', value: 'last-30-days' });
    expect(screen.queryByRole('radio')).toBeNull();
  });

  test('lets the user pick a custom range and apply it', () => {
    const onChange = mock(() => {});
    renderControl({ kind: 'preset', value: 'last-7-days' }, onChange);

    // Open the popover.
    clickButton(/Range/);

    // Expand the Custom range calendar.
    clickButton(/Custom range/);

    // Select July 10 as the start and July 15 as the end of the range.
    clickDay(new Date(2026, 6, 10));
    clickDay(new Date(2026, 6, 15));

    // Apply the custom range.
    clickButton(/Apply/);

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({
      kind: 'custom',
      from: '2026-07-10',
      to: '2026-07-15',
    });
    expect(screen.queryByRole('radio')).toBeNull();
  });

  test('disables Apply when no dates are selected and enables it when range is complete', () => {
    renderControl({ kind: 'preset', value: 'last-7-days' });

    // Open and expand Custom range.
    clickButton(/Range/);
    clickButton(/Custom range/);

    // Before any selection Apply is disabled.
    expect(screen.getByRole('button', { name: /Apply/ }).disabled).toBe(true);

    // After picking a range Apply becomes enabled.
    // react-day-picker v10 sets from==to on the first click and then expands on the second.
    clickDay(new Date(2026, 6, 10));
    clickDay(new Date(2026, 6, 15));
    expect(screen.getByRole('button', { name: /Apply/ }).disabled).toBe(false);

    // After clearing the draft Apply is disabled again.
    clickButton(/Clear/);
    expect(screen.getByRole('button', { name: /Apply/ }).disabled).toBe(true);
  });

  test('resets draft when popover is closed and reopened', () => {
    renderControl({ kind: 'preset', value: 'last-7-days' });

    // First open: expand Custom and pick one date.
    clickButton(/Range/);
    clickButton(/Custom range/);
    clickDay(new Date(2026, 6, 10));

    // Close the popover by clicking the trigger again.
    clickButton(/Range/);
    expect(screen.queryByRole('radio')).toBeNull();

    // Reopen — draft must be reset to the committed value.
    clickButton(/Range/);

    // Custom range is collapsed (value is a preset).
    expect(screen.queryByRole('button', { name: /Apply/ })).toBeNull();

    // Expand it again; the calendar must be blank (no prior selection).
    clickButton(/Custom range/);
    // When the draft is undefined the Apply button should be disabled.
    expect(screen.getByRole('button', { name: /Apply/ }).disabled).toBe(true);
  });
});

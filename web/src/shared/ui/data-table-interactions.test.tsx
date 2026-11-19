import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, mock, test } from 'bun:test';

import { I18nProvider } from '@/shared/i18n';
import { CopyIdCell } from './data-table-interactions';
import { ResourceStatusBadge } from './resource-status-badge';
import { formatRelativeDateTime, RelativeTimeText } from './resource-time-cell';

afterEach(() => {
  cleanup();
});

describe('shared data table primitives', () => {
  test('CopyIdCell announces copied status for screen readers after copying', async () => {
    const writeText = mock(() => Promise.resolve());
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });

    render(
      <I18nProvider initialLocale="en">
        <CopyIdCell value="wh_0123456789abcdef" ariaLabel="Copy wh_0123456789abcdef" />
      </I18nProvider>,
    );

    const status = screen.getByRole('status');
    expect(status.textContent).toBe('');

    fireEvent.click(screen.getByRole('button', { name: 'Copy wh_0123456789abcdef' }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('wh_0123456789abcdef'));
    await waitFor(() => expect(status.textContent).toBe('Copied'));
  });

  test('ResourceStatusBadge composes the shared Badge primitive', () => {
    render(<ResourceStatusBadge tone="success">Active</ResourceStatusBadge>);
    const badge = screen.getByText('Active');
    expect(badge.getAttribute('data-slot')).toBe('badge');
    expect(badge.className).toContain('status-success');
  });

  test('RelativeTimeText renders relative time with full datetime title', () => {
    const timestamp = '2026-11-19T07:00:00Z';
    expect(formatRelativeDateTime(timestamp, 'en', 'just now', Date.parse('2026-11-19T07:05:00Z'))).toBe(
      '5 minutes ago',
    );
    render(
      <I18nProvider initialLocale="en">
        <RelativeTimeText value={timestamp} />
      </I18nProvider>,
    );
    const timeEl = document.querySelector('time[datetime="2026-11-19T07:00:00.000Z"]');
    expect(timeEl).toBeTruthy();
    expect(timeEl?.getAttribute('title')).toContain('2026');
  });
});

import { afterEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { ObservabilityStatus } from './ObservabilityStatus';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

describe('ObservabilityStatus', () => {
  test('empty state has no retry action', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/observability');
    render(<ObservabilityStatus title="No data" />);

    expect(screen.getByRole('status').textContent).toContain('No data');
    expect(screen.queryByRole('button')).toBeNull();
  });

  test('error state exposes retry', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/observability');
    let retried = false;
    render(
      <ObservabilityStatus
        tone="error"
        title="Couldn’t load this panel"
        actionLabel="Retry"
        onAction={() => {
          retried = true;
        }}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(retried).toBe(true);
    expect(screen.getByRole('alert').textContent).toContain('Couldn’t load this panel');
  });
});

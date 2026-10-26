import { afterEach, expect, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { resetTestDom } from '../../../test/setup';
import { SpanDetailPanel } from './SpanDetailPanel';
import { buildTraceTree, flattenTraceTree } from './traceTree';

const { cleanup, fireEvent, render, screen, within } = await import('@testing-library/react');

afterEach(cleanup);

test('preserves attribute values and exposes the selected format when switching views', () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/observability');
  const span = flattenTraceTree(
    buildTraceTree([
      {
        span_id: 'span_test',
        parent_span_id: '',
        kind: 'llm',
        name: 'model.request',
        start_time: '2026-10-25T03:00:00Z',
        end_time: '2026-10-25T03:00:01Z',
        duration_ms: 1000,
        status: 'ok',
        attributes: { model: 'example-model' },
      },
    ]),
  )[0]!;
  render(
    <I18nProvider initialLocale="en">
      <SpanDetailPanel span={span} color="var(--chart-1)" agentName="Test agent" onClose={() => undefined} />
    </I18nProvider>,
  );
  const json = screen.getByRole('button', { name: 'JSON', exact: true });
  const table = screen.getByRole('button', { name: 'Table', exact: true });
  expect(json.getAttribute('aria-pressed')).toBe('true');
  expect(table.getAttribute('aria-pressed')).toBe('false');
  fireEvent.click(table);
  expect(table.getAttribute('aria-pressed')).toBe('true');
  expect(within(screen.getByRole('tabpanel', { name: 'Attributes' })).getByText('example-model')).toBeTruthy();
  fireEvent.click(screen.getByRole('tab', { name: 'Metadata' }));
  expect(within(screen.getByRole('tabpanel', { name: 'Metadata' })).getByText('span_test')).toBeTruthy();
  fireEvent.click(json);
  expect(json.getAttribute('aria-pressed')).toBe('true');
  expect(screen.getByRole('tabpanel', { name: 'Metadata' }).textContent).toContain('span_test');
});

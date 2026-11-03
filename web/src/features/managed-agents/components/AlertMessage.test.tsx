import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ManagedErrorAlert, ManagedWarningAlert } from './common';
import { analyzeAlertText } from './alertText';

const htmlError =
  '404 <!DOCTYPE html><html lang="en"><head><title>404 - Not Found</title><style>body { margin: 0; }</style></head><body>404 Not Found</body></html>';

afterEach(cleanup);

function longMessage() {
  return ['Request failed:', ...Array.from({ length: 50 }, () => 'detail')].join(' ');
}

function renderAlert(message: string, locale: 'en' | 'zh-CN' = 'en') {
  return render(
    <I18nProvider initialLocale={locale}>
      <ManagedWarningAlert>{message}</ManagedWarningAlert>
    </I18nProvider>,
  );
}

describe('alert text analysis', () => {
  test('recognizes an HTML error page and keeps its raw text as detail', () => {
    expect(analyzeAlertText(htmlError)).toEqual({
      detail: htmlError,
      html: true,
      status: 404,
      summary: '',
    });
  });

  test('keeps short plain messages intact without details', () => {
    expect(analyzeAlertText('  Request failed.  ')).toEqual({
      detail: null,
      html: false,
      status: null,
      summary: 'Request failed.',
    });
  });

  test('truncates long plain messages and keeps the full text as detail', () => {
    const message = longMessage();
    const analysis = analyzeAlertText(message);
    expect(analysis?.detail).toBe(message);
    expect(analysis?.summary.endsWith('…')).toBe(true);
    expect(analysis?.summary.length).toBeLessThan(message.length);
  });

  test('keeps a plain message that mentions an HTML closing tag intact', () => {
    expect(analyzeAlertText('Unexpected token </body> in template.')).toEqual({
      detail: null,
      html: false,
      status: null,
      summary: 'Unexpected token </body> in template.',
    });
  });

  test('returns null for blank text', () => {
    expect(analyzeAlertText('   ')).toBeNull();
  });
});

describe('alert message presentation', () => {
  test('shows a concise HTTP summary and reveals the raw error page on demand', () => {
    renderAlert(htmlError);
    expect(screen.getByText('Request failed: the server returned an HTTP 404 page.')).toBeTruthy();
    expect(screen.queryByText(/<!DOCTYPE html>/)).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Show raw error' }));
    expect(screen.getByText(htmlError)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Hide raw error' }));
    expect(screen.queryByText(/<!DOCTYPE html>/)).toBeNull();
  });

  test('localizes the HTTP summary and toggle labels', () => {
    renderAlert(htmlError, 'zh-CN');
    expect(screen.getByText('请求失败：服务返回了 HTTP 404 页面。')).toBeTruthy();
    expect(screen.getByRole('button', { name: '查看原始错误' })).toBeTruthy();
  });

  test('summarizes an HTML error page without an HTTP status', () => {
    renderAlert('<!DOCTYPE html><html><body>Service unavailable</body></html>');
    expect(screen.getByText('Request failed: the server returned an unrecognized response.')).toBeTruthy();
  });

  test('renders a short plain message without a details toggle', () => {
    renderAlert('Request failed.');
    expect(screen.getByText('Request failed.')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });

  test('expands long plain messages to the full text', () => {
    const message = longMessage();
    renderAlert(message);
    expect(screen.queryByText(message)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Show details' }));
    expect(screen.getByText(message)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Hide details' }));
    expect(screen.queryByText(message)).toBeNull();
  });

  test('renders non-string children unchanged', () => {
    render(
      <I18nProvider initialLocale="en">
        <ManagedErrorAlert>
          <strong>Nested failure</strong>
        </ManagedErrorAlert>
      </I18nProvider>,
    );
    expect(screen.getByText('Nested failure').tagName).toBe('STRONG');
  });
});

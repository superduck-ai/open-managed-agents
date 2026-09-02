import { afterEach, describe, expect, test } from 'bun:test';
import { useState } from 'react';

import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';

const testingLibrary = await import('@testing-library/react');
const { CredentialMcpServerField } = await import('./credential-mcp-server-field');

const { cleanup, fireEvent, render, screen, within } = testingLibrary;

const directoryOptions = [
  {
    id: 'https://api.githubcopilot.com/mcp/',
    label: 'GitHub',
    secondary: 'https://api.githubcopilot.com/mcp/',
  },
];

function Harness({ initialValue = '' }: { initialValue?: string }) {
  const [value, setValue] = useState(initialValue);
  return (
    <I18nProvider>
      <div data-testid="mcp-value">{value}</div>
      <CredentialMcpServerField value={value} directoryOptions={directoryOptions} onChange={setValue} />
    </I18nProvider>
  );
}

afterEach(() => {
  cleanup();
});

describe('CredentialMcpServerField', () => {
  test('locks a directory selection until cleared', async () => {
    resetTestDom('https://oma.duck.ai/resources');
    render(<Harness />);

    fireEvent.click(screen.getByRole('combobox', { name: 'MCP server' }));
    const listbox = await screen.findByRole('listbox');
    fireEvent.click(within(listbox).getByText('GitHub'));

    expect(screen.getByTestId('mcp-value').textContent).toBe('https://api.githubcopilot.com/mcp/');
    expect(screen.queryByRole('combobox', { name: 'MCP server' })).toBeNull();
    expect(screen.getByDisplayValue(/GitHub/)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Clear MCP server' }));
    expect(screen.getByTestId('mcp-value').textContent).toBe('');
    expect(screen.getByRole('combobox', { name: 'MCP server' })).toBeTruthy();
  });

  test('custom server requires confirm before locking', async () => {
    resetTestDom('https://oma.duck.ai/resources');
    render(<Harness />);

    fireEvent.click(screen.getByRole('combobox', { name: 'MCP server' }));
    const listbox = await screen.findByRole('listbox');
    fireEvent.click(within(listbox).getByText('Custom server'));

    const input = screen.getByRole('textbox', { name: 'MCP server' });
    fireEvent.change(input, { target: { value: 'https://mcp.example.com/mcp' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(screen.getByTestId('mcp-value').textContent).toBe('https://mcp.example.com/mcp');
    expect(screen.getByDisplayValue('https://mcp.example.com/mcp')).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: 'MCP server' })).toBeNull();
  });
});

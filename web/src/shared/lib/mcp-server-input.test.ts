import { describe, expect, test } from 'bun:test';

import { validateMCPServerInput } from './mcp-server-input';

describe('validateMCPServerInput', () => {
  test('rejects missing and malformed values', () => {
    expect(validateMCPServerInput(' ', 'ftp://example.com/mcp')).toEqual({
      name: 'required',
      url: 'invalid',
    });
    expect(validateMCPServerInput('unsafe name', 'https://user:secret@example.com/mcp')).toEqual({
      name: 'invalid',
      url: 'invalid',
    });
    expect(validateMCPServerInput('unsafe__name', 'https://example.com/mcp#tools')).toEqual({
      name: 'ambiguous',
      url: 'invalid',
    });
  });

  test('measures the endpoint limit in bytes', () => {
    expect(validateMCPServerInput('server', `https://example.com/${'界'.repeat(680)}`)).toEqual({ url: 'too_long' });
  });

  test('accepts a valid reusable MCP server', () => {
    expect(validateMCPServerInput('internal-docs', 'https://docs.example.com/mcp')).toEqual({});
  });
});

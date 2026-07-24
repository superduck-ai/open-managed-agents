import { describe, expect, test } from 'bun:test';
import { codeForRevision, createDefaultRevision } from './model';

describe('Workbench generated API code', () => {
  const revision = {
    ...createDefaultRevision('provider/model'),
    messages: [{ role: 'human' as const, content: [{ type: 'text' as const, text: 'Hello' }] }],
  };

  test('routes cURL through the Open Managed Agents API', () => {
    const code = codeForRevision('curl', revision);

    expect(code).toContain('$OMA_BASE_URL/v1/messages');
    expect(code).toContain('$OMA_API_KEY');
    expect(code).not.toContain('api.anthropic.com');
    expect(code).not.toContain('ANTHROPIC_API_KEY');
  });

  test('configures Anthropic SDKs to call Open Managed Agents', () => {
    const python = codeForRevision('python', revision);
    const typescript = codeForRevision('typescript', revision);

    expect(python).toContain('base_url=os.environ["OMA_BASE_URL"]');
    expect(python).toContain('api_key=os.environ["OMA_API_KEY"]');
    expect(typescript).toContain('baseURL: process.env.OMA_BASE_URL');
    expect(typescript).toContain('apiKey: process.env.OMA_API_KEY');
  });
});

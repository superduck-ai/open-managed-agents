import { anthropicBaseURL } from '../../../../shared/api/anthropic';
import { quickstartCurl } from '../RequestPreview';
import type { BuilderState } from './model';

export type IntegrationLanguage = 'bash' | 'javascript' | 'python';
export type IntegrationRequest = { path: string; body?: unknown; stream?: boolean };

export function builderIntegrationRequests(state: BuilderState, sessionId: string): IntegrationRequest[] {
  const id = encodeURIComponent(sessionId || 'SESSION_ID');
  return [
    state.deployment
      ? { path: `/v1/deployment_runs?deployment_id=${encodeURIComponent(state.deployment.id)}` }
      : {
          path: '/v1/sessions',
          body: {
            agent: { type: 'agent', id: state.agent?.id, version: state.agent?.version },
            environment_id: state.environment?.id,
            vault_ids: state.vaults.map((vault) => vault.id),
            resources: [],
            metadata: {},
          },
        },
    { path: `/v1/sessions/${id}/events/stream`, stream: true },
    {
      path: `/v1/sessions/${id}/events`,
      body: {
        events: [
          { type: 'user.message', content: [{ type: 'text', text: state.firstMessage || state.suggestedMessage }] },
        ],
      },
    },
  ];
}

export function integrationExample(request: IntegrationRequest, workspaceId: string, language: IntegrationLanguage) {
  if (language === 'bash') return quickstartCurl(request.path, workspaceId, request.body, request.stream);
  const url = new URL(`${anthropicBaseURL()}${request.path}`);
  url.searchParams.set('beta', 'true');
  const headers = {
    'x-api-key': 'YOUR_API_KEY',
    'anthropic-version': '2023-06-01',
    'x-workspace-id': workspaceId,
    ...(request.stream ? { Accept: 'text/event-stream' } : {}),
  };
  const body = request.body === undefined ? undefined : JSON.stringify(request.body);
  if (language === 'python') {
    return [
      'import json',
      'import requests',
      '',
      `response = requests.${body ? 'post' : 'get'}(`,
      `    ${JSON.stringify(url.href)},`,
      `    headers=${JSON.stringify(headers, null, 4)},`,
      ...(body ? [`    json=json.loads(${JSON.stringify(body)}),`] : []),
      ...(request.stream ? ['    stream=True,'] : []),
      ')',
      'response.raise_for_status()',
      request.stream ? 'for line in response.iter_lines():\n    print(line.decode())' : 'print(response.json())',
    ].join('\n');
  }
  return [
    `const response = await fetch(${JSON.stringify(url.href)}, {`,
    `  method: ${JSON.stringify(body ? 'POST' : 'GET')},`,
    `  headers: ${JSON.stringify({ ...headers, ...(body ? { 'Content-Type': 'application/json' } : {}) }, null, 2)},`,
    ...(body ? [`  body: JSON.stringify(${body}),`] : []),
    '});',
    'if (!response.ok) throw new Error(`HTTP ${response.status}`);',
    request.stream
      ? 'const reader = response.body.getReader();\nconst decoder = new TextDecoder();\nwhile (true) {\n  const { done, value } = await reader.read();\n  if (done) break;\n  console.log(decoder.decode(value, { stream: true }));\n}'
      : 'console.log(await response.json());',
  ].join('\n');
}

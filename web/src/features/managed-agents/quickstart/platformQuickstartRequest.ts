import { platformQuickstartOfficialRequest } from './platformQuickstartOfficialRequest.generated';

export type QuickstartStep = 'agent' | 'environment' | 'session' | 'integrate';

export type QuickstartMessage = {
  role: 'user' | 'assistant';
  content: string | Array<Record<string, unknown>>;
};

export type QuickstartRequestInput = {
  step: QuickstartStep;
  deploymentSchedulePlanned: boolean;
  agentConfig: Record<string, unknown>;
  agentDescription?: string | null;
  messages?: QuickstartMessage[];
};

export type PlatformQuickstartRequest = {
  messages: QuickstartMessage[];
  system: Array<Record<string, unknown>>;
  model: string;
  max_tokens: number;
  tools: Array<Record<string, unknown>>;
  tool_choice: Record<string, unknown>;
  stream: boolean;
};

export const platformQuickstartModel = platformQuickstartOfficialRequest.model;
export const platformQuickstartMaxTokens = platformQuickstartOfficialRequest.max_tokens;
export const platformQuickstartSystem = platformQuickstartOfficialRequest.system;
export const platformQuickstartTools = platformQuickstartOfficialRequest.tools;
export const platformQuickstartToolChoice = platformQuickstartOfficialRequest.tool_choice;

export const platformQuickstartToolNames = platformQuickstartTools.map((tool) =>
  typeof tool.name === 'string' ? tool.name : String(tool.type)
);

export function buildPlatformQuickstartRequest(input: QuickstartRequestInput): PlatformQuickstartRequest {
  return {
    messages: input.messages?.length ? input.messages : [buildInitialQuickstartMessage(input)],
    system: platformQuickstartSystem,
    model: platformQuickstartModel,
    max_tokens: platformQuickstartMaxTokens,
    tools: platformQuickstartTools,
    tool_choice: platformQuickstartToolChoice,
    stream: true
  };
}

export function buildInitialQuickstartMessage(input: QuickstartRequestInput): QuickstartMessage {
  return {
    role: 'user',
    content: buildQuickstartTurnContextText(input)
  };
}

export function buildQuickstartTurnContextText(input: QuickstartRequestInput): string {
  const trimmedDescription = input.agentDescription?.trim();
  const agentIntro = trimmedDescription
    ? [
        "I'm building an agent. Here's my description:",
        `"${trimmedDescription}"`
      ]
    : ["I'm building a new agent."];

  return [
      `[Current quickstart step: "${input.step}". Follow this step's instructions from the system prompt.]`,
      '',
      `[Deployment schedule planned: ${input.deploymentSchedulePlanned ? 'yes' : 'no'}.]`,
      '',
      ...agentIntro,
      '',
      "Here's the current config:",
      JSON.stringify(input.agentConfig, null, 2),
      '',
      'Start from the current quickstart step (see turn context).'
    ].join('\n');
}

export function stableStringify(value: unknown) {
  return JSON.stringify(sortObjectKeys(value), null, 2);
}

function sortObjectKeys(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sortObjectKeys);
  }
  if (!isPlainObject(value)) {
    return value;
  }
  return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortObjectKeys(value[key])]));
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

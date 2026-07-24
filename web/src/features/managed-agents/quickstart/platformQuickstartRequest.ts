import { type Locale } from '../../../shared/i18n';
import { platformQuickstartOfficialRequest } from './platformQuickstartOfficialRequest.generated';
import { resolveQuickstartSystem } from './quickstartPromptText';

export type QuickstartStep = 'agent' | 'environment' | 'session' | 'integrate';

export type QuickstartMessage = {
  role: 'user' | 'assistant';
  content: string | Array<Record<string, unknown>>;
};

export type QuickstartTurnContextInput = {
  step: QuickstartStep;
  deploymentSchedulePlanned: boolean;
  agentConfig: Record<string, unknown>;
  agentDescription?: string | null;
  messages?: QuickstartMessage[];
  locale?: Locale;
};

export type QuickstartRequestInput = QuickstartTurnContextInput & {
  modelID: string;
  availableModelIDs: string[];
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

export const platformQuickstartMaxTokens = platformQuickstartOfficialRequest.max_tokens;
export const platformQuickstartSystem = platformQuickstartOfficialRequest.system;
export const platformQuickstartToolChoice = platformQuickstartOfficialRequest.tool_choice;

const platformQuickstartToolsTemplate = platformQuickstartOfficialRequest.tools;

export const platformQuickstartToolNames = platformQuickstartToolsTemplate.map((tool) =>
  typeof tool.name === 'string' ? tool.name : String(tool.type),
);

export function buildPlatformQuickstartRequest(input: QuickstartRequestInput): PlatformQuickstartRequest {
  const availableModelIDs = uniqueModelIDs(input.availableModelIDs);
  const modelID = input.modelID.trim();
  if (!modelID || !availableModelIDs.includes(modelID)) {
    throw new Error('Quickstart requires a model from the current model catalog.');
  }
  return {
    messages: input.messages?.length ? input.messages : [buildInitialQuickstartMessage(input)],
    system: resolveQuickstartSystem(input.locale ?? 'en'),
    model: modelID,
    max_tokens: platformQuickstartMaxTokens,
    tools: quickstartToolsForModels(availableModelIDs),
    tool_choice: platformQuickstartToolChoice,
    stream: true,
  };
}

export function quickstartToolsForModels(availableModelIDs: string[]) {
  const modelIDs = uniqueModelIDs(availableModelIDs);
  if (!modelIDs.length) {
    throw new Error('Quickstart model schema requires at least one catalog model.');
  }
  const tools = JSON.parse(JSON.stringify(platformQuickstartToolsTemplate)) as Array<Record<string, unknown>>;
  const buildAgentConfig = tools.find((tool) => tool.name === 'build_agent_config');
  const inputSchema = objectRecord(buildAgentConfig?.input_schema);
  const properties = objectRecord(inputSchema.properties);
  const modelSchema = objectRecord(properties.model);
  const alternatives = Array.isArray(modelSchema.anyOf) ? modelSchema.anyOf : [];
  const stringModel = objectRecord(alternatives[0]);
  const objectModel = objectRecord(alternatives[1]);
  const objectModelProperties = objectRecord(objectModel.properties);
  const objectModelID = objectRecord(objectModelProperties.id);
  if (!buildAgentConfig || alternatives.length < 2 || !Object.keys(objectModelID).length) {
    throw new Error('Captured quickstart build_agent_config model schema is invalid.');
  }
  alternatives[0] = { ...stringModel, enum: modelIDs };
  alternatives[1] = {
    ...objectModel,
    properties: {
      ...objectModelProperties,
      id: { ...objectModelID, enum: modelIDs },
    },
  };
  modelSchema.anyOf = alternatives;
  properties.model = modelSchema;
  inputSchema.properties = properties;
  buildAgentConfig.input_schema = inputSchema;
  return tools;
}

function uniqueModelIDs(modelIDs: string[]) {
  return Array.from(new Set(modelIDs.map((modelID) => modelID.trim()).filter(Boolean)));
}

function objectRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

export function buildInitialQuickstartMessage(input: QuickstartTurnContextInput): QuickstartMessage {
  return {
    role: 'user',
    content: buildQuickstartTurnContextText(input),
  };
}

export function buildQuickstartTurnContextText(input: QuickstartTurnContextInput): string {
  const trimmedDescription = input.agentDescription?.trim();

  // Bracketed state lines are machine state the model reads to align with the system
  // prompt's step keys; keep them in English for both locales. Only the natural-language
  // sentences are localized.
  const stateLines = [
    `[Current quickstart step: "${input.step}". Follow this step's instructions from the system prompt.]`,
    '',
    `[Deployment schedule planned: ${input.deploymentSchedulePlanned ? 'yes' : 'no'}.]`,
    '',
  ];

  if ((input.locale ?? 'en') === 'zh-CN') {
    const agentIntro = trimmedDescription
      ? ['我正在构建一个 agent。这是我的描述：', `"${trimmedDescription}"`]
      : ['我正在构建一个新的 agent。'];
    return [
      ...stateLines,
      ...agentIntro,
      '',
      '这是当前的配置：',
      JSON.stringify(input.agentConfig, null, 2),
      '',
      '请从当前 quickstart 步骤开始（见 turn context）。',
    ].join('\n');
  }

  const agentIntro = trimmedDescription
    ? ["I'm building an agent. Here's my description:", `"${trimmedDescription}"`]
    : ["I'm building a new agent."];

  return [
    ...stateLines,
    ...agentIntro,
    '',
    "Here's the current config:",
    JSON.stringify(input.agentConfig, null, 2),
    '',
    'Start from the current quickstart step (see turn context).',
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
  return Object.fromEntries(
    Object.keys(value)
      .sort()
      .map((key) => [key, sortObjectKeys(value[key])]),
  );
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

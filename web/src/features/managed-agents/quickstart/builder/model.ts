import { buildQuickstartToolResultMessage, toolResultBlock } from '../chatModel';
import { z } from 'zod';
import { createAgentDraftSchema, normalizeCreateAgentDraft } from '../../agents/create-dialog-model';
import type { CreateAgentInput } from '../../types';
import { stableStringify } from '../platformQuickstartRequest';

export const builderStepSchema = z.enum(['agent', 'environment', 'session', 'deploy', 'integrate']);
export const builderToolSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  input: z.record(z.string(), z.unknown()),
});
export const builderMessageSchema = z.object({
  role: z.enum(['user', 'assistant']),
  reasoning: z.string().optional(),
  content: z.union([z.string(), z.array(z.record(z.string(), z.unknown()))]),
});
const referenceSchema = z.object({ id: z.string(), name: z.string() });
export const builderStateSchema = z.object({
  version: z.literal(1),
  step: builderStepSchema,
  messages: z.array(builderMessageSchema),
  queue: z.array(builderToolSchema),
  config: createAgentDraftSchema,
  savedConfig: createAgentDraftSchema.nullable(),
  agent: referenceSchema.extend({ version: z.number() }).nullable(),
  environment: referenceSchema.nullable(),
  vaults: z.array(referenceSchema),
  skippedServers: z.array(z.string()),
  deployment: referenceSchema.nullable(),
  wantsSchedule: z.boolean(),
  gate: builderToolSchema.nullable(),
  readError: z.string().default(''),
  operation: z
    .object({
      id: z.string(),
      kind: z.enum(['agent', 'environment', 'vault', 'deployment', 'session']),
      toolId: z.string(),
    })
    .nullable(),
  sessionID: z.string(),
  sessionBinding: z.string(),
  unconfirmedMessage: z.string(),
  testClosed: z.boolean(),
  suggestedMessage: z.string(),
  firstMessage: z.string(),
  testSummary: z.string().default(''),
  initialized: z.boolean(),
});
export type BuilderState = z.infer<typeof builderStateSchema>;
export type BuilderTool = z.infer<typeof builderToolSchema>;
export type BuilderMessage = z.infer<typeof builderMessageSchema>;
export type BuilderStep = z.infer<typeof builderStepSchema>;

export function newBuilderState(config: CreateAgentInput): BuilderState {
  return {
    version: 1,
    step: 'agent',
    messages: [],
    queue: [],
    config: createAgentDraftSchema.parse(normalizeCreateAgentDraft(config)),
    savedConfig: null,
    agent: null,
    environment: null,
    vaults: [],
    skippedServers: [],
    deployment: null,
    wantsSchedule: false,
    gate: null,
    readError: '',
    operation: null,
    sessionID: '',
    sessionBinding: '',
    unconfirmedMessage: '',
    testClosed: false,
    suggestedMessage: '',
    firstMessage: '',
    testSummary: '',
    initialized: false,
  };
}
export function builderStorageKey(account: string, workspace: string) {
  return `oma-builder-v1:${account}:${workspace}`;
}
export function loadBuilderState(key: string): BuilderState | null {
  try {
    const result = builderStateSchema.safeParse(JSON.parse(window.sessionStorage.getItem(key) ?? 'null'));
    return result.success ? result.data : null;
  } catch {
    return null;
  }
}
export function saveBuilderState(key: string, state: BuilderState) {
  try {
    window.sessionStorage.setItem(key, JSON.stringify(state));
    return true;
  } catch {
    return false;
  }
}
export function toolResult(state: BuilderState, tool: BuilderTool, result: unknown, isError = false): BuilderState {
  if (!state.queue.some((item) => item.id === tool.id)) return state;
  let resultIndex = state.messages.length;
  while (
    resultIndex > 0 &&
    state.messages[resultIndex - 1].role === 'user' &&
    typeof state.messages[resultIndex - 1].content === 'string'
  )
    resultIndex--;
  return {
    ...state,
    gate: null,
    readError: '',
    queue: state.queue.filter((item) => item.id !== tool.id),
    messages: [
      ...state.messages.slice(0, resultIndex),
      buildQuickstartToolResultMessage([
        toolResultBlock(tool.id, {
          content: typeof result === 'string' ? result : JSON.stringify(result),
          isError,
        }),
      ]),
      ...state.messages.slice(resultIndex),
    ],
  };
}
export function builderTestBinding(state: BuilderState) {
  return state.agent && state.environment ? `${state.agent.id}:${state.agent.version}:${state.environment.id}` : '';
}
export function invalidateBuilderTest(state: BuilderState, preserveClosedTest = false): BuilderState {
  return {
    ...state,
    sessionID: '',
    sessionBinding: '',
    unconfirmedMessage: '',
    firstMessage: '',
    deployment: null,
    step: ['deploy', 'integrate'].includes(state.step)
      ? state.wantsSchedule && preserveClosedTest && state.testClosed
        ? 'deploy'
        : 'session'
      : state.step,
    testClosed: preserveClosedTest && state.testClosed,
    testSummary: preserveClosedTest ? state.testSummary : '',
  };
}
export function builderNextStep(state: BuilderState): BuilderStep {
  if (state.step === 'agent') return 'environment';
  if (state.step === 'environment') return 'session';
  if (state.step === 'session' && state.wantsSchedule) return 'deploy';
  return 'integrate';
}

export function builderHistoryText(message: BuilderMessage, zh: boolean, pendingToolId?: string) {
  if (typeof message.content === 'string') return message.content;
  return message.content
    .flatMap((block) => {
      if (block.type === 'text') return [String(block.text ?? '')];
      if (block.type === 'tool_use' && block.name === 'ask_user_questions' && block.id !== pendingToolId) {
        const input = z.object({ questions: z.array(z.object({ question: z.string() })) }).safeParse(block.input);
        return input.success ? input.data.questions.map((question) => question.question) : [];
      }
      if (block.type !== 'tool_result') return [];
      return builderResultText(block.content, zh);
    })
    .join('\n');
}

function builderResultText(content: unknown, zh: boolean) {
  try {
    const result = JSON.parse(String(content));
    const answers = z.array(z.object({ answers: z.array(z.string()) })).safeParse(result);
    if (answers.success) return answers.data.map((answer) => answer.answers.join(', ') || (zh ? '继续' : 'Continue'));
    if (result.user_choice) return [String(result.user_choice)];
    if (result.id && result.name) return [zh ? `${result.name}：已选择` : `${result.name}: selected`];
    if (result.saved || result.recovered) return [zh ? '已保存' : 'Saved'];
    if (result.skipped)
      return [zh ? `${result.service ?? ''}：已跳过授权` : `${result.service ?? ''}: authorization skipped`];
    if (result.authorized)
      return [zh ? `${result.service ?? ''}：凭据已添加` : `${result.service ?? ''}: credential added`];
    if (result.selected_vaults) return [zh ? '已选择保险库' : 'Vaults selected'];
    return [];
  } catch {
    return [];
  }
}

export function builderExportConfig(state: BuilderState) {
  const pending = Boolean(state.savedConfig && stableStringify(state.config) !== stableStringify(state.savedConfig));
  return {
    configuration_status: state.savedConfig ? (pending ? 'unsaved_changes' : 'saved') : 'draft',
    agent: state.savedConfig ?? state.config,
    ...(pending ? { draft: state.config } : {}),
    session: {
      ...(state.agent && state.savedConfig
        ? { agent: { type: 'agent', id: state.agent.id, version: state.agent.version } }
        : {}),
      ...(state.environment ? { environment_id: state.environment.id } : {}),
      vault_ids: state.vaults.map((vault) => vault.id),
    },
  };
}

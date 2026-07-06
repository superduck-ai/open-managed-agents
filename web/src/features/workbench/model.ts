import { AuthAccount } from '../../shared/auth/api';
import { TextStreamSmoother } from '../../shared/api/textSmoother';
import {
  WorkbenchContentBlock,
  WorkbenchMessage,
  WorkbenchModel,
  WorkbenchPromptDetail,
  WorkbenchPromptSummary,
  WorkbenchRevision,
  WorkbenchStreamEvent,
  WorkbenchThinking,
  WorkbenchTool,
  WorkbenchUploadedFile
} from './api';

export type DrawerName = 'model' | 'variables' | 'tools' | 'examples' | 'history';

export type ResponseTab = 'preview' | 'api';

export type WorkbenchMode = 'prompt' | 'evaluate';

export type GeneratePromptStep = 'generate' | 'output';

export type CodeLanguage =
  | 'python'
  | 'typescript'
  | 'curl'
  | 'bedrock-python'
  | 'bedrock-typescript'
  | 'vertex-python'
  | 'vertex-typescript';

export type ToolForm = 'custom' | 'web_search' | null;

export type WebSearchRestriction = 'none' | 'allowed_domains' | 'blocked_domains';

export type WebSearchToolForm = {
  maxUsesEnabled: boolean;
  maxUses: number;
  localize: boolean;
  searchRestriction: WebSearchRestriction;
  domains: string;
};

export const codeLanguageOptions: Array<{ value: CodeLanguage; label: string }> = [
  { value: 'python', label: 'Python' },
  { value: 'typescript', label: 'TypeScript' },
  { value: 'curl', label: 'cURL' },
  { value: 'bedrock-python', label: 'AWS Bedrock Python' },
  { value: 'bedrock-typescript', label: 'AWS Bedrock TypeScript' },
  { value: 'vertex-python', label: 'Vertex AI Python' },
  { value: 'vertex-typescript', label: 'Vertex AI TypeScript' }
];

export type WorkbenchExample = {
  id: string;
  values: Record<string, string>;
  idealOutput: string;
  additionalContext: string;
};

export type EvaluateComparison = {
  id: string;
  revisionId: string;
  label: string;
  revision: WorkbenchRevision;
};

export type EvaluateComparisonOutput = {
  modelOutput: string;
  rating: string;
  runError: string | null;
  isRunning: boolean;
};

export type EvaluateTestCase = {
  id: string;
  evaluationId: string;
  testCaseId: string;
  values: Record<string, string>;
  idealOutput: string;
  modelOutput: string;
  rating: string;
  runError: string | null;
  isRunning: boolean;
  comparisonOutputs: Record<string, EvaluateComparisonOutput>;
};

export const WORKBENCH_MAX_TOKENS = 128000;

export const DEFAULT_THINKING_BUDGET_TOKENS = 16000;

export const MIN_THINKING_BUDGET_TOKENS = 1024;

export const MAX_THINKING_BUDGET_TOKENS = WORKBENCH_MAX_TOKENS - 1;

export const DEFAULT_THINKING: WorkbenchThinking = {
  type: 'enabled',
  effort: 'high',
  budget_tokens: DEFAULT_THINKING_BUDGET_TOKENS
};

export const thinkingEffortOptions = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'extra_high', label: 'Extra high' },
  { value: 'max', label: 'Max' }
] as const;

export const fallbackModels: WorkbenchModel[] = [
  { model_name: 'claude-opus-4-8', display_name: 'Claude Opus Active', supports_thinking: true, supports_tool_use: true },
  { model_name: 'claude-sonnet-4-6', display_name: 'Claude Sonnet Active', supports_thinking: true, supports_tool_use: true },
  { model_name: 'claude-haiku-4-5-20251001', display_name: 'Claude Haiku 4.5', supports_thinking: false },
  { model_name: 'claude-fable-5', display_name: 'Claude Fable 5', supports_thinking: true, supports_tool_use: true }
];

export const defaultSchema = `{
  "type": "object",
  "properties": {
    "location": {
      "type": "string",
      "description": "The city and state, e.g. San Francisco, CA"
    }
  },
  "required": ["location"]
}`;

export function defaultWebSearchToolForm(): WebSearchToolForm {
  return {
    maxUsesEnabled: false,
    maxUses: 5,
    localize: false,
    searchRestriction: 'none',
    domains: ''
  };
}

export function generatedValueString(value: unknown) {
  if (value === null || value === undefined) {
    return '';
  }
  if (typeof value === 'string') {
    return value;
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  return '';
}

export function hasOwn(value: object, key: string) {
  return Object.prototype.hasOwnProperty.call(value, key);
}

export function createDefaultRevision(): WorkbenchRevision {
  return {
    id: '04977f32-f204-443c-8d3e-ed5aac2673aa',
    created_at: '2026-06-12T02:10:24.382428Z',
    is_latest: true,
    model_name: 'claude-opus-4-8',
    system_prompt: '',
    messages: [{ role: 'human', content: [{ type: 'text', text: '' }] }],
    variables: [],
    tools: [],
    max_tokens_to_sample: 20000,
    temperature: 1,
    thinking: { ...DEFAULT_THINKING },
    show_raw_thinking: false,
    skip_system_modification: false
  };
}

export function normalizeRevision(value?: Partial<WorkbenchRevision> | null, fallbackModel = 'claude-opus-4-8'): WorkbenchRevision {
  const base = createDefaultRevision();
  const messages = normalizeMessages(value?.messages);
  return {
    ...base,
    ...value,
    id: String(value?.id || base.id),
    created_at: String(value?.created_at || base.created_at),
    model_name: String(value?.model_name || fallbackModel || base.model_name),
    system_prompt: String(value?.system_prompt ?? ''),
    messages,
    variables: normalizeVariables(value?.variables),
    tools: normalizeTools(value?.tools),
    max_tokens_to_sample: numberOr(value?.max_tokens_to_sample, base.max_tokens_to_sample),
    temperature: numberOr(value?.temperature, base.temperature ?? 1),
    thinking: normalizeThinking(value?.thinking),
    show_raw_thinking: Boolean(value?.show_raw_thinking),
    skip_system_modification: Boolean(value?.skip_system_modification),
    is_latest: value?.is_latest !== false
  };
}

export function normalizeNewPromptRevision(value?: Partial<WorkbenchRevision> | null, fallbackModel = 'claude-opus-4-8'): WorkbenchRevision {
  return normalizeRevision(
    {
      ...value,
      model_name: value?.model_name || fallbackModel,
      system_prompt: '',
      messages: createDefaultRevision().messages,
      variables: [],
      tools: [],
      show_raw_thinking: false,
      skip_system_modification: false
    },
    fallbackModel
  );
}

export function normalizeMessages(messages?: WorkbenchMessage[]) {
  if (!Array.isArray(messages) || messages.length === 0) {
    return createDefaultRevision().messages;
  }
  const normalized = messages
    .map((message): WorkbenchMessage => ({
      role: message.role === 'assistant' ? 'assistant' : 'human',
      content: normalizeContentBlocks(message.content)
    }))
    .filter((message, index) => index === 0 || messageHasContent(message));
  return normalized.length ? normalized : createDefaultRevision().messages;
}

export function normalizeContentBlocks(content: WorkbenchMessage['content']): WorkbenchContentBlock[] {
  if (typeof content === 'string') {
    return [{ type: 'text', text: content }];
  }
  if (!Array.isArray(content)) {
    return [{ type: 'text', text: '' }];
  }
  const blocks = content
    .filter((block): block is WorkbenchContentBlock => Boolean(block && typeof block === 'object'))
    .map((block) => ({ ...block, type: String(block.type || 'text') }));
  return blocks.length ? blocks : [{ type: 'text', text: '' }];
}

export function cleanMessageContent(content: WorkbenchMessage['content'], includeEmptyText: boolean): WorkbenchContentBlock[] {
  const blocks = normalizeContentBlocks(content).flatMap((block) => {
    if (block.type !== 'text') {
      return [{ ...block }];
    }
    const text = typeof block.text === 'string' ? block.text : '';
    if (!includeEmptyText && !text.trim()) {
      return [];
    }
    return [{ ...block, type: 'text', text }];
  });
  return blocks.length || !includeEmptyText ? blocks : [{ type: 'text', text: '' }];
}

export function replaceMessageText(content: WorkbenchMessage['content'], text: string): WorkbenchContentBlock[] {
  let replaced = false;
  const blocks = normalizeContentBlocks(content).map((block) => {
    if (!replaced && block.type === 'text') {
      replaced = true;
      return { ...block, type: 'text', text };
    }
    return block;
  });
  return replaced ? blocks : [{ type: 'text', text }, ...blocks];
}

export function appendFileBlockToMessageContent(
  content: WorkbenchMessage['content'],
  uploaded: WorkbenchUploadedFile
): WorkbenchContentBlock[] {
  return [...normalizeContentBlocks(content), workbenchFileContentBlock(uploaded)];
}

export function appendUrlBlockToMessageContent(
  content: WorkbenchMessage['content'],
  kind: WorkbenchAttachmentKind,
  url: string
): WorkbenchContentBlock[] {
  return [...normalizeContentBlocks(content), workbenchUrlContentBlock(kind, url)];
}

export function replaceFileBlockInMessageContent(
  content: WorkbenchMessage['content'],
  blockIndex: number,
  uploaded: WorkbenchUploadedFile
): WorkbenchContentBlock[] {
  return normalizeContentBlocks(content).map((block, index) =>
    index === blockIndex ? workbenchFileContentBlock(uploaded) : block
  );
}

export function removeContentBlockFromMessageContent(content: WorkbenchMessage['content'], blockIndex: number): WorkbenchContentBlock[] {
  const blocks = normalizeContentBlocks(content).filter((_, index) => index !== blockIndex);
  return blocks.length ? blocks : [{ type: 'text', text: '' }];
}

export function workbenchFileContentBlock(uploaded: WorkbenchUploadedFile): WorkbenchContentBlock {
  const source = { type: 'file', file_id: uploaded.id };
  if (uploaded.mime_type?.startsWith('image/')) {
    return { type: 'image', source, filename: uploaded.filename || uploaded.id };
  }
  return { type: 'document', source, title: uploaded.filename || uploaded.id };
}

export function workbenchUrlContentBlock(kind: WorkbenchAttachmentKind, url: string): WorkbenchContentBlock {
  return {
    type: kind === 'image' ? 'image' : 'document',
    source: { type: 'url', url }
  };
}

export function messageHasContent(message: WorkbenchMessage) {
  return cleanMessageContent(message.content, false).some((block) => block.type !== 'text' || Boolean(block.text?.trim()));
}

export function revisionHasImageContent(revision: WorkbenchRevision) {
  return revision.messages.some((message) =>
    normalizeContentBlocks(message.content).some((block) => String(block.type).toLowerCase() === 'image')
  );
}

export function hasMultipleHumanMessages(revision: WorkbenchRevision) {
  return revision.messages.filter((message) => message.role !== 'assistant').length > 1;
}

export function messageAttachments(message: WorkbenchMessage) {
  return normalizeContentBlocks(message.content)
    .map((block, blockIndex) => ({ block, blockIndex }))
    .filter(({ block }) => block.type !== 'text')
    .map(({ block, blockIndex }) => {
      const source = block.source && typeof block.source === 'object' ? (block.source as Record<string, unknown>) : {};
      const urlLabel =
        stringValue(source.type) === 'url'
          ? String(block.type).toLowerCase() === 'image'
            ? 'Image'
            : 'PDF'
          : '';
      const label =
        stringValue(block.title) ||
        stringValue(block.filename) ||
        stringValue(source.file_id) ||
        urlLabel ||
        stringValue(source.url) ||
        stringValue(block.name) ||
        block.type;
      return {
        id: `${blockIndex}-${block.type}-${label}`,
        blockIndex,
        kind: String(block.type).toLowerCase(),
        label
      };
    });
}

export function normalizeVariables(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((item) => String(item).trim()).filter(Boolean);
}

export function normalizeTools(value: unknown): WorkbenchTool[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .filter((item): item is WorkbenchTool => Boolean(item && typeof item === 'object'))
    .map((item) => ({ ...item, name: String(item.name || 'tool') }));
}

export function normalizeThinking(value: unknown): WorkbenchThinking {
  if (!value || typeof value !== 'object') {
    return { ...DEFAULT_THINKING };
  }
  const thinking = value as WorkbenchThinking;
  return cleanThinking({ ...thinking, type: thinking.type || DEFAULT_THINKING.type, effort: thinking.effort ?? DEFAULT_THINKING.effort });
}

export function parseDraftRevision(value?: string) {
  if (!value?.trim()) {
    return null;
  }
  try {
    return JSON.parse(value) as WorkbenchRevision;
  } catch {
    return null;
  }
}

export const workbenchExampleMetaKeys = new Set([
  'id',
  'variable_values',
  'variableValues',
  'values',
  'variables',
  'inputs',
  'input',
  'ideal_output',
  'idealOutput',
  'golden_answer',
  'goldenAnswer',
  'expected_output',
  'expectedOutput',
  'output',
  'answer',
  'additional_context',
  'additionalContext',
  'example_description',
  'exampleDescription',
  'description',
  'context'
]);

export function workbenchExamplesFromPromptDetail(detail: WorkbenchPromptDetail, variables: string[]) {
  const promptRecord = detail as WorkbenchPromptDetail & Record<string, unknown>;
  const revisionRecord = detail.latest_revision as (WorkbenchRevision & Record<string, unknown>) | undefined;
  const candidates = [
    promptRecord.examples,
    revisionRecord?.examples,
    detail.kv_store?.examples,
    detail.kv_store?.workbench_examples,
    detail.kv_store?.multishot_examples,
    detail.kv_store?.multi_shot_examples
  ];
  for (const candidate of candidates) {
    const parsed = normalizeWorkbenchExamples(candidate, variables);
    if (parsed.length) {
      return parsed;
    }
  }
  return [];
}

export function normalizeWorkbenchExamples(source: unknown, variables: string[]) {
  return workbenchExampleArray(source)
    .map((item, index) => normalizeWorkbenchExample(item, variables, index))
    .filter((example): example is WorkbenchExample => Boolean(example));
}

export function workbenchExampleArray(source: unknown): unknown[] {
  if (typeof source === 'string') {
    if (!source.trim()) {
      return [];
    }
    try {
      return workbenchExampleArray(JSON.parse(source));
    } catch {
      return [];
    }
  }
  if (Array.isArray(source)) {
    return source;
  }
  if (!source || typeof source !== 'object') {
    return [];
  }
  const record = source as Record<string, unknown>;
  for (const key of ['examples', 'items', 'data', 'value']) {
    const nested = workbenchExampleArray(record[key]);
    if (nested.length) {
      return nested;
    }
  }
  return [];
}

export function normalizeWorkbenchExample(source: unknown, variables: string[], index: number): WorkbenchExample | null {
  if (!source || typeof source !== 'object' || Array.isArray(source)) {
    return null;
  }
  const record = source as Record<string, unknown>;
  const rawValues = firstRecordValue(record, ['variable_values', 'variableValues', 'values', 'variables', 'inputs', 'input']);
  const valuesSource = rawValues ?? record;
  const valueNames = variables.length
    ? variables
    : Object.keys(valuesSource).filter((key) => !workbenchExampleMetaKeys.has(key));
  const values = Object.fromEntries(
    valueNames.map((name) => [name, generatedValueString(recordValueForName(valuesSource, name)).trim()])
  );
  const idealOutput = firstStringValue(record, [
    'ideal_output',
    'idealOutput',
    'golden_answer',
    'goldenAnswer',
    'expected_output',
    'expectedOutput',
    'output',
    'answer'
  ]);
  const additionalContext = firstStringValue(record, [
    'additional_context',
    'additionalContext',
    'example_description',
    'exampleDescription',
    'description',
    'context'
  ]);
  if (!Object.values(values).some(Boolean) && !idealOutput && !additionalContext) {
    return null;
  }
  return {
    id: stringValue(record.id) || `example_${index + 1}`,
    values,
    idealOutput,
    additionalContext
  };
}

export function firstRecordValue(record: Record<string, unknown>, keys: string[]): Record<string, unknown> | null {
  for (const key of keys) {
    const value = record[key];
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return value as Record<string, unknown>;
    }
  }
  return null;
}

export function firstStringValue(record: Record<string, unknown>, keys: string[]) {
  for (const key of keys) {
    const value = generatedValueString(record[key]).trim();
    if (value) {
      return value;
    }
  }
  return '';
}

export function recordValueForName(record: Record<string, unknown>, name: string) {
  if (hasOwn(record, name)) {
    return record[name];
  }
  const matchedKey = Object.keys(record).find((key) => key.toLowerCase() === name.toLowerCase());
  return matchedKey ? record[matchedKey] : undefined;
}

export function buildRevisionPayload(
  draft: WorkbenchRevision,
  options: { includeEmptyMessages?: boolean; newRevisionId?: boolean } = {}
): WorkbenchRevision {
  const includeEmptyMessages = Boolean(options.includeEmptyMessages);
  const messages = draft.messages
    .map((message): WorkbenchMessage => ({
      role: message.role === 'assistant' ? 'assistant' : 'human',
      content: cleanMessageContent(message.content, includeEmptyMessages)
    }))
    .filter((message) => includeEmptyMessages || messageHasContent(message));
  const revision: WorkbenchRevision = {
    ...draft,
    id: options.newRevisionId ? `workbench-revision-${workbenchId('')}` : draft.id || `workbench-revision-${workbenchId('')}`,
    created_at: draft.created_at || new Date().toISOString(),
    is_latest: true,
    messages,
    variables: extractVariables({ ...draft, messages }),
    tools: draft.tools.map(cleanTool),
    thinking: cleanThinking(draft.thinking),
    max_tokens_to_sample: Math.max(1, Number(draft.max_tokens_to_sample) || 20000),
    show_raw_thinking: Boolean(draft.show_raw_thinking),
    skip_system_modification: Boolean(draft.skip_system_modification)
  };
  return revision;
}

export function buildRunRevisionPayload(
  draft: WorkbenchRevision,
  variableValues: Record<string, string>,
  examples: WorkbenchExample[] = []
): WorkbenchRevision {
  const revision = buildRevisionPayload(draft, { includeEmptyMessages: false });
  const hydratedRevision = revision.variables.length
    ? {
        ...revision,
        messages: revision.messages.map((message) => ({
          ...message,
          content: Array.isArray(message.content)
            ? message.content.flatMap((block) => replaceVariablesInBlock(block, revision.variables, variableValues))
            : replaceVariablesInText(message.content, revision.variables, variableValues)
        }))
      }
    : revision;
  return prependRunExamples(hydratedRevision, examples);
}

export function prependRunExamples(revision: WorkbenchRevision, examples: WorkbenchExample[]): WorkbenchRevision {
  if (!examples.length) {
    return revision;
  }
  const examplesBlock = buildRunExamplesBlock(examples, revision.variables);
  if (!examplesBlock) {
    return revision;
  }
  let inserted = false;
  const messages = revision.messages.map((message) => {
    if (inserted || message.role !== 'human') {
      return message;
    }
    inserted = true;
    return {
      ...message,
      content: prependTextBlock(message.content, examplesBlock)
    };
  });
  if (inserted) {
    return { ...revision, messages };
  }
  return {
    ...revision,
    messages: [{ role: 'human', content: [{ type: 'text', text: examplesBlock }] }, ...messages]
  };
}

export function prependTextBlock(content: WorkbenchMessage['content'], text: string): WorkbenchContentBlock[] {
  if (Array.isArray(content)) {
    return [{ type: 'text', text }, ...content];
  }
  return [{ type: 'text', text }, { type: 'text', text: content }];
}

export function buildRunExamplesBlock(examples: WorkbenchExample[], variables: string[]) {
  const usableExamples = examples.filter((example) => example.idealOutput.trim());
  if (!usableExamples.length) {
    return '';
  }
  const lines = ['<examples>'];
  for (const example of usableExamples) {
    lines.push('<example>');
    if (example.additionalContext.trim()) {
      lines.push('<example_description>', example.additionalContext, '</example_description>');
    }
    for (const name of variables) {
      lines.push(`<${name}>`, example.values[name] ?? '', `</${name}>`);
    }
    lines.push('<ideal_output>', example.idealOutput, '</ideal_output>', '</example>');
  }
  lines.push('</examples>', '', '');
  return lines.join('\n');
}

export function replaceVariablesInBlock(
  block: WorkbenchContentBlock,
  variables: string[],
  variableValues: Record<string, string>
): WorkbenchContentBlock[] {
  if (block.type !== 'text' || typeof block.text !== 'string') {
    return [block];
  }
  return replaceVariablesInText(block.text, variables, variableValues);
}

export function replaceVariablesInText(
  text: string,
  variables: string[],
  variableValues: Record<string, string>
): WorkbenchContentBlock[] {
  const matcher = variableMatcher(variables);
  if (!matcher) {
    return [{ type: 'text', text }];
  }
  const blocks: WorkbenchContentBlock[] = [];
  let cursor = 0;
  let match = matcher.exec(text);
  while (match) {
    const before = text.slice(cursor, match.index);
    if (before) {
      blocks.push({ type: 'text', text: before, cache_control: { type: 'ephemeral' } });
    }
    const name = match[1];
    blocks.push({ type: 'text', text: variableValues[name] ?? '' });
    cursor = match.index + match[0].length;
    match = matcher.exec(text);
  }
  const tail = text.slice(cursor);
  if (tail) {
    blocks.push({ type: 'text', text: tail });
  }
  return blocks.length ? blocks : [{ type: 'text', text }];
}

export function variableMatcher(variables: string[]) {
  if (!variables.length) {
    return null;
  }
  const alternatives = variables.map(escapeRegExp).join('|');
  return new RegExp(`{{\\s*(${alternatives})\\s*}}`, 'g');
}

export function titleMessageContent(draft: WorkbenchRevision) {
  return titleMessageBlocksText(draft.messages[0]).trim();
}

export function titleMessageBlocksText(message?: WorkbenchMessage) {
  if (!message) {
    return '';
  }
  if (typeof message.content === 'string') {
    return message.content;
  }
  return message.content.map((block) => (block.type === 'text' && typeof block.text === 'string' ? block.text : '')).join('');
}

export function truncateTitleMessageContent(text: string) {
  if (text.length <= 500) {
    return text;
  }
  return `${text.slice(0, 250)}\n\n  [...]\n\n  ${text.slice(-250)}`;
}

export function defaultGeneratedPromptTitle(date = new Date()) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const time = new Intl.DateTimeFormat('en-US', {
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    hour12: true
  }).format(date);
  return `Untitled - ${year}-${month}-${day} ${time}`;
}

export function displayPromptTitle(promptName: string, draft: WorkbenchRevision) {
  const trimmedName = promptName.trim();
  if (trimmedName && !isUntitledName(trimmedName)) {
    return trimmedName;
  }
  return fallbackPromptTitle(draft) || 'Untitled';
}

export function promptSummaryDisplayTitle(prompt: WorkbenchPromptSummary, selectedPromptId: string | undefined, selectedPromptTitle: string) {
  const trimmedName = prompt.name?.trim();
  if (trimmedName) {
    return trimmedName;
  }
  if (prompt.id === selectedPromptId && selectedPromptTitle.trim()) {
    return selectedPromptTitle;
  }
  return 'Untitled';
}

export function fallbackPromptTitle(draft: WorkbenchRevision) {
  return titleMessageContent(draft).replace(/[.!?。！？]+$/g, '').trim();
}

export function isUntitledName(name: string) {
  return !name.trim() || name.trim() === 'Untitled';
}

export function cleanTool(tool: WorkbenchTool): WorkbenchTool {
  const { id, ...rest } = tool;
  return rest;
}

export function webSearchToolFromForm(form: WebSearchToolForm, id: string): WorkbenchTool {
  const domains = parseDomainList(form.domains);
  return {
    id,
    type: 'web_search_v0',
    name: 'web_search',
    ...(form.maxUsesEnabled ? { max_uses: form.maxUses } : {}),
    ...(form.localize ? { user_location: { type: 'approximate' } } : {}),
    ...(form.searchRestriction === 'allowed_domains' && domains.length ? { allowed_domains: domains } : {}),
    ...(form.searchRestriction === 'blocked_domains' && domains.length ? { blocked_domains: domains } : {})
  };
}

export function webSearchToolFormFromTool(tool: WorkbenchTool): WebSearchToolForm {
  const allowedDomains = stringList(tool.allowed_domains);
  const blockedDomains = stringList(tool.blocked_domains);
  const maxUses = typeof tool.max_uses === 'number' && Number.isFinite(tool.max_uses) ? Math.max(1, Math.round(tool.max_uses)) : 5;
  const searchRestriction = allowedDomains.length ? 'allowed_domains' : blockedDomains.length ? 'blocked_domains' : 'none';
  return {
    maxUsesEnabled: typeof tool.max_uses === 'number' && Number.isFinite(tool.max_uses),
    maxUses,
    localize: Boolean(tool.user_location),
    searchRestriction,
    domains: (allowedDomains.length ? allowedDomains : blockedDomains).join(', ')
  };
}

export function webSearchToolSummary(tool: WorkbenchTool) {
  const allowedDomains = stringList(tool.allowed_domains);
  if (allowedDomains.length) {
    return `${allowedDomains.length === 1 ? 'Allowed domain' : 'Allowed domains'}: ${allowedDomains.join(', ')}`;
  }
  const blockedDomains = stringList(tool.blocked_domains);
  if (blockedDomains.length) {
    return `${blockedDomains.length === 1 ? 'Blocked domain' : 'Blocked domains'}: ${blockedDomains.join(', ')}`;
  }
  return 'No search restrictions';
}

export function stringList(value: unknown) {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((item) => String(item).trim()).filter(Boolean);
}

export function parseDomainList(value: string) {
  return value
    .split(/[,\s]+/)
    .map((domain) => domain.trim().replace(/^https?:\/\//i, '').replace(/\/.*$/, '').toLowerCase())
    .filter(Boolean)
    .filter((domain, index, domains) => domains.indexOf(domain) === index);
}

export function webSearchRestrictionLabel(value: WebSearchRestriction) {
  if (value === 'allowed_domains') {
    return 'Allow domains';
  }
  if (value === 'blocked_domains') {
    return 'Blocked domains';
  }
  return 'None';
}

export function clampThinkingBudgetTokens(value: unknown) {
  const numeric = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(numeric)) {
    return DEFAULT_THINKING_BUDGET_TOKENS;
  }
  return Math.min(MAX_THINKING_BUDGET_TOKENS, Math.max(MIN_THINKING_BUDGET_TOKENS, Math.round(numeric)));
}

export function nextThinkingForMode(
  current: WorkbenchThinking,
  mode: 'disabled' | 'enabled' | 'adaptive'
): WorkbenchThinking {
  if (mode === 'disabled') {
    return { type: 'disabled' };
  }
  if (mode === 'enabled') {
    return {
      ...current,
      type: 'enabled',
      effort: current.effort ?? 'high',
      budget_tokens: clampThinkingBudgetTokens(current.budget_tokens)
    };
  }
  const { budget_tokens: _budgetTokens, ...rest } = current;
  return { ...rest, type: 'adaptive', effort: rest.effort ?? 'high' };
}

export function cleanThinking(thinking: WorkbenchThinking): WorkbenchThinking {
  const mode = thinkingMode(thinking);
  if (mode === 'disabled') {
    return { type: 'disabled' };
  }
  if (mode === 'enabled') {
    return {
      ...thinking,
      type: 'enabled',
      effort: thinking.effort ?? 'high',
      budget_tokens: clampThinkingBudgetTokens(thinking.budget_tokens)
    };
  }
  const { budget_tokens: _budgetTokens, ...rest } = thinking;
  return { ...rest, type: 'adaptive', effort: rest.effort ?? 'high' };
}

export function messageText(message?: WorkbenchMessage) {
  if (!message) {
    return '';
  }
  if (typeof message.content === 'string') {
    return message.content;
  }
  return message.content
    .map((block) => (block.type === 'text' && typeof block.text === 'string' ? block.text : ''))
    .filter(Boolean)
    .join('\n');
}

export type VariableTextPart =
  | { type: 'text'; text: string }
  | { type: 'variable'; name: string };

export function splitVariableText(text: string): VariableTextPart[] {
  const parts: VariableTextPart[] = [];
  const matcher = /{{\s*([a-zA-Z_][\w.-]*)\s*}}/g;
  let lastIndex = 0;
  let match = matcher.exec(text);
  while (match) {
    if (match.index > lastIndex) {
      parts.push({ type: 'text', text: text.slice(lastIndex, match.index) });
    }
    parts.push({ type: 'text', text: '{{' });
    parts.push({ type: 'variable', name: match[1] });
    parts.push({ type: 'text', text: '}}' });
    lastIndex = match.index + match[0].length;
    match = matcher.exec(text);
  }
  if (lastIndex < text.length) {
    parts.push({ type: 'text', text: text.slice(lastIndex) });
  }
  return parts.length ? parts : [{ type: 'text', text }];
}

export function extractVariables(revision: WorkbenchRevision) {
  const names = new Set<string>();
  collectVariablesFromText(revision.system_prompt, names);
  revision.messages.forEach((message) => collectVariablesFromText(messageText(message), names));
  return Array.from(names);
}

export function extractVariablesFromText(text: string) {
  const names = new Set<string>();
  collectVariablesFromText(text, names);
  return Array.from(names);
}

export function collectVariablesFromText(text: string, names: Set<string>) {
  const matcher = /{{\s*([a-zA-Z_][\w.-]*)\s*}}/g;
  let match = matcher.exec(text);
  while (match) {
    names.add(match[1]);
    match = matcher.exec(text);
  }
}

export function canEvaluateRevision(revision: WorkbenchRevision) {
  return extractVariables(revision).length > 0 && revision.tools.length === 0;
}

export function parseTaggedVariables(text: string, variables: string[]) {
  const values: Record<string, string> = {};
  variables.forEach((name) => {
    const value = parseTaggedValue(text, name);
    if (value) {
      values[name] = value;
    }
  });
  return values;
}

export function parseTaggedValue(text: string, tagName: string) {
  const match = new RegExp(`<${escapeRegExp(tagName)}>([\\s\\S]*?)</${escapeRegExp(tagName)}>`, 'i').exec(text);
  return match?.[1]?.trim() ?? '';
}

export function stripTaggedVariables(text: string, variables: string[]) {
  let stripped = text;
  variables.forEach((name) => {
    stripped = stripped.replace(new RegExp(`<${escapeRegExp(name)}>[\\s\\S]*?</${escapeRegExp(name)}>`, 'gi'), '');
  });
  return stripped;
}

export function hasRunnableMessage(revision: WorkbenchRevision) {
  return revision.messages.some((message) => message.role !== 'assistant' && messageHasContent(message));
}

export function isBlankWorkbenchDraft(revision: WorkbenchRevision) {
  const payload = buildRevisionPayload(revision, { includeEmptyMessages: false });
  return !payload.system_prompt?.trim() && payload.messages.length === 0;
}

export function extractGeneratedPromptInstructions(text: string) {
  const section = extractStreamingXmlSection(text, 'Instructions');
  return section ? cleanGeneratedPromptInstructions(section) : '';
}

export function extractStreamingXmlSection(text: string, tagName: string) {
  const openTag = `<${tagName}>`;
  const closeTag = `</${tagName}>`;
  const openIndex = text.indexOf(openTag);
  if (openIndex === -1) {
    return '';
  }
  const contentStart = openIndex + openTag.length;
  const closeIndex = text.indexOf(closeTag, contentStart);
  if (closeIndex !== -1) {
    return text.slice(contentStart, closeIndex).trim();
  }
  const partialCloseIndex = text.indexOf('<', text.length - closeTag.length);
  return partialCloseIndex !== -1 && closeTag.startsWith(text.slice(partialCloseIndex))
    ? text.slice(contentStart, partialCloseIndex).trim()
    : text.slice(contentStart).trim();
}

export function cleanGeneratedPromptInstructions(text: string) {
  return removeTrailingGeneratedPromptCourtesy(
    text
      .replace(/\{\$([a-zA-Z_][\w.-]*)\}/g, '{{$1}}')
      .replace(/<\/?Instructions>/g, '')
      .trim()
  );
}

export function removeTrailingGeneratedPromptCourtesy(text: string) {
  const lines = text.trimEnd().split(/\r?\n/);
  while (lines.length > 0 && /^\s*(?:please\s+)?let me know\b/i.test(lines[lines.length - 1])) {
    lines.pop();
  }
  return lines.join('\n').trim();
}

export async function streamSmoothedWorkbenchText({
  signal,
  stream,
  onEvent,
  onRawText,
  onDisplayText,
  displayTextFromRaw = (text) => text,
  smoothingEnabled = true
}: {
  signal: AbortSignal;
  stream: (onEvent: (event: WorkbenchStreamEvent) => void) => Promise<void>;
  onEvent?: (event: WorkbenchStreamEvent) => void;
  onRawText?: (rawText: string, deltaText: string, event: WorkbenchStreamEvent) => void;
  onDisplayText: (text: string) => void;
  displayTextFromRaw?: (rawText: string) => string;
  smoothingEnabled?: boolean;
}) {
  let rawText = '';
  const smoother = new TextStreamSmoother({
    signal,
    smoothingEnabled,
    onUpdate: onDisplayText
  });

  try {
    await stream((event) => {
      onEvent?.(event);
      const deltaText = textDeltaFromEvent(event);
      if (!deltaText) {
        return;
      }
      rawText += deltaText;
      onRawText?.(rawText, deltaText, event);
      smoother.setTarget(displayTextFromRaw(rawText));
    });
    await smoother.finish();
    return rawText;
  } catch (error) {
    smoother.flush();
    throw error;
  } finally {
    smoother.dispose();
  }
}

export function textDeltaFromEvent(event: WorkbenchStreamEvent) {
  const type = String(event.data.type ?? event.event ?? '');
  if (type !== 'content_block_delta') {
    return '';
  }
  const delta = event.data.delta;
  if (!delta || typeof delta !== 'object') {
    return '';
  }
  const text = (delta as Record<string, unknown>).text;
  return typeof text === 'string' ? text : '';
}

export function modelDisplayName(model?: WorkbenchModel) {
  return String(model?.display_name || model?.name || model?.model_name || 'Claude');
}

export function thinkingMode(thinking?: WorkbenchThinking) {
  const mode = String(thinking?.type || DEFAULT_THINKING.type);
  return mode === 'enabled' || mode === 'disabled' || mode === 'adaptive' ? mode : DEFAULT_THINKING.type;
}

export function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export function drawerTitle(drawer: DrawerName) {
  switch (drawer) {
    case 'model':
      return 'Model';
    case 'variables':
      return 'Test Case';
    case 'tools':
      return 'Tools';
    case 'examples':
      return 'Examples';
    case 'history':
      return 'Version history';
  }
}

export function currentRouteTab(): WorkbenchMode {
  if (typeof window === 'undefined') {
    return 'prompt';
  }
  return new URLSearchParams(window.location.search).get('tab') === 'evaluate' ? 'evaluate' : 'prompt';
}

export function currentRouteIsWorkbenchIndex() {
  if (typeof window === 'undefined') {
    return false;
  }
  return /^\/workbench\/?$/.test(window.location.pathname);
}

export function currentRoutePromptId() {
  if (typeof window === 'undefined') {
    return undefined;
  }
  const match = /\/workbench\/([^/?#]+)/.exec(window.location.pathname);
  const value = match?.[1] ? decodeURIComponent(match[1]) : undefined;
  return value && value !== 'new' ? value : undefined;
}

export function currentRouteRequestsNewPrompt() {
  if (typeof window === 'undefined') {
    return false;
  }
  return /^\/workbench\/new\/?$/.test(window.location.pathname);
}

export function syncWorkbenchPromptUrl(promptId: string, mode: 'replace' | 'push', options: { resetTab?: boolean } = {}) {
  if (typeof window === 'undefined') {
    return;
  }
  const trimmed = promptId.trim();
  if (!trimmed) {
    return;
  }
  const nextUrl = new URL(window.location.href);
  nextUrl.pathname = `/workbench/${encodeURIComponent(trimmed)}`;
  if (options.resetTab) {
    nextUrl.searchParams.delete('tab');
  }
  const nextPath = `${nextUrl.pathname}${nextUrl.search}${nextUrl.hash}`;
  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (currentPath === nextPath) {
    return;
  }
  if (mode === 'push') {
    updateWorkbenchHistory('push', nextPath);
  } else {
    updateWorkbenchHistory('replace', nextPath);
  }
}

export function syncWorkbenchIndexUrl(mode: 'replace' | 'push') {
  if (typeof window === 'undefined' || window.location.pathname === '/workbench') {
    return;
  }
  if (mode === 'push') {
    updateWorkbenchHistory('push', '/workbench');
  } else {
    updateWorkbenchHistory('replace', '/workbench');
  }
}

export function syncWorkbenchNewUrl(mode: 'replace' | 'push') {
  if (typeof window === 'undefined') {
    return;
  }
  const nextUrl = new URL(window.location.href);
  nextUrl.pathname = '/workbench/new';
  nextUrl.searchParams.delete('tab');
  const nextPath = `${nextUrl.pathname}${nextUrl.search}${nextUrl.hash}`;
  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (currentPath === nextPath) {
    return;
  }
  if (mode === 'push') {
    updateWorkbenchHistory('push', nextPath);
  } else {
    updateWorkbenchHistory('replace', nextPath);
  }
}

export function syncWorkbenchTabUrl(tab: WorkbenchMode, mode: 'replace' | 'push') {
  if (typeof window === 'undefined') {
    return;
  }
  const nextUrl = new URL(window.location.href);
  if (tab === 'evaluate') {
    nextUrl.searchParams.set('tab', 'evaluate');
  } else {
    nextUrl.searchParams.delete('tab');
  }
  const nextPath = `${nextUrl.pathname}${nextUrl.search}${nextUrl.hash}`;
  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (nextPath === currentPath) {
    return;
  }
  if (mode === 'push') {
    updateWorkbenchHistory('push', nextPath);
  } else {
    updateWorkbenchHistory('replace', nextPath);
  }
}

export function updateWorkbenchHistory(mode: 'replace' | 'push', nextPath: string) {
  if (mode === 'push') {
    window.history.pushState(window.history.state, '', nextPath);
  } else {
    window.history.replaceState(window.history.state, '', nextPath);
  }
  const event =
    typeof window.PopStateEvent === 'function'
      ? new window.PopStateEvent('popstate', { state: window.history.state })
      : new window.Event('popstate');
  window.dispatchEvent(event);
}

export function workbenchDraftAutosaveKey(promptId: string, draft: WorkbenchRevision) {
  return `${promptId}\u0000${JSON.stringify(buildRevisionPayload(draft, { includeEmptyMessages: true }))}`;
}

export function mergePromptSummaries(current: WorkbenchPromptSummary[], detail: WorkbenchPromptSummary) {
  const next = current.filter((item) => item.id !== detail.id);
  return [detail, ...next];
}

export function numberOr(value: unknown, fallback: number) {
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : fallback;
}

export function shortTime() {
  return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(new Date());
}

export function savedPromptMeta(prompt: WorkbenchPromptDetail | null, draft: WorkbenchRevision) {
  const formatted = formatDate(prompt?.updated_at || draft.created_at || prompt?.created_at);
  return formatted ? `Last saved ${formatted}` : 'Last saved';
}

export function historyDayLabel(value?: string) {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const today = new Date();
  if (
    date.getFullYear() === today.getFullYear() &&
    date.getMonth() === today.getMonth() &&
    date.getDate() === today.getDate()
  ) {
    return 'Today';
  }
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', year: 'numeric' }).format(date);
}

export function historyRevisionName(revision: WorkbenchRevision) {
  return stringValue(revision.name) || stringValue(revision.title) || 'Untitled version';
}

export function historyRevisionPreview(revision: WorkbenchRevision):
  | { kind: 'variables'; values: string[] }
  | { kind: 'message'; value: string } {
  const variables = revision.variables?.length ? revision.variables : extractVariables(revision);
  if (variables.length) {
    return { kind: 'variables', values: variables };
  }
  return { kind: 'message', value: titleMessageContent(revision) || 'Empty prompt' };
}

export function formatHistoryTimestamp(value?: string) {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit'
  }).format(date);
}

export function formatPromptSummaryDate(prompt: WorkbenchPromptSummary, account?: AuthAccount | null) {
  const value = prompt.created_at || prompt.updated_at;
  const creatorLabel = isPromptCreator(prompt, account ?? null) ? 'you' : promptCreatorLabel(prompt);
  if (!value) {
    return `by ${creatorLabel}`;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return `by ${creatorLabel}`;
  }
  return `${new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', year: 'numeric' }).format(date)} by ${creatorLabel}`;
}

export function formatShareCreatedDate(value?: string) {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', year: 'numeric' }).format(date);
}

export function promptCreatorLabel(prompt: WorkbenchPromptSummary) {
  const creator = prompt.creator;
  return creator?.tagged_id?.trim() || creator?.full_name?.trim() || creator?.email_address?.trim() || 'you';
}

export function isPromptCreator(prompt: WorkbenchPromptSummary, account: AuthAccount | null) {
  const creator = prompt.creator;
  if (!creator) {
    return true;
  }
  const creatorIds = [
    creator.tagged_id,
    creator.uuid,
    creator.email_address,
    creator.full_name
  ].map((value) => value?.trim()).filter(Boolean);
  if (!creatorIds.length) {
    return true;
  }
  const accountIds = [
    account?.tagged_id,
    account?.uuid,
    account?.email_address,
    account?.full_name,
    account?.display_name
  ].map((value) => value?.trim()).filter(Boolean);
  return accountIds.some((value) => creatorIds.includes(value));
}

export function mostRecentPromptForWorkbenchEntry(
  prompts: WorkbenchPromptSummary[],
  workspaceId: string,
  account: AuthAccount | null
) {
  return [...prompts]
    .filter((prompt) => prompt.workspace_id === workspaceId)
    .filter((prompt) => isStrictCurrentAccountCreator(prompt, account))
    .sort((left, right) => promptRecencyTime(right) - promptRecencyTime(left))[0];
}

export function isStrictCurrentAccountCreator(prompt: WorkbenchPromptSummary, account: AuthAccount | null) {
  const creatorTaggedId = prompt.creator?.tagged_id?.trim();
  const accountTaggedId = account?.tagged_id?.trim();
  return Boolean(creatorTaggedId && accountTaggedId && creatorTaggedId === accountTaggedId);
}

export function promptRecencyTime(prompt: WorkbenchPromptSummary) {
  const timestamp = Date.parse(prompt.updated_at || prompt.created_at || '');
  return Number.isFinite(timestamp) ? timestamp : 0;
}

export type WorkbenchAccessState = { hasAccess: true } | { hasAccess: false; productName: string };

export type WorkbenchPromptGeneratorWarning = { title: string };

export type WorkbenchAttachmentKind = 'image' | 'pdf';

export const promptGeneratorWarningTitle = 'Get More credits to use the prompt generator';

export const generatePromptExamples = [
  {
    id: 'summarize',
    title: 'Summarize a document',
    task: 'Summarize documents into 10 bullet points max'
  },
  {
    id: 'email',
    title: 'Write me an email',
    task: 'Draft an email responding to a customer complaint email and offer a resolution'
  },
  {
    id: 'translate-code',
    title: 'Translate code',
    task: 'Translate code to Python'
  },
  {
    id: 'content-moderation',
    title: 'Content moderation',
    task: 'Classify chat transcripts into categories using our content moderation policy'
  },
  {
    id: 'recommend-product',
    title: 'Recommend a product',
    task: 'Recommend a product based on a customer’s previous transactions'
  }
] as const;

export function workbenchAccessState(account: AuthAccount | null, orgUuid?: string | null): WorkbenchAccessState {
  const membership = currentOrganizationMembership(account, orgUuid);
  const organization = membership?.organization as Record<string, unknown> | undefined;
  const settings = organization?.settings as Record<string, unknown> | undefined;
  const productName =
    stringValue(readSetting(settings, ['product_name'])) ||
    stringValue(readSetting(settings, ['productName'])) ||
    stringValue(organization?.name) ||
    'Open Managed Agents';

  if (hasExplicitDisabledWorkbenchProduct(settings) || hasExplicitDisabledWorkbenchRole(settings)) {
    return { hasAccess: false, productName };
  }
  return { hasAccess: true };
}

export function workbenchPromptGeneratorWarning(
  account: AuthAccount | null,
  orgUuid?: string | null,
  prepaidCreditAmount?: number | null
): WorkbenchPromptGeneratorWarning | null {
  if (organizationApiDisabledReason(account, orgUuid)) {
    return { title: promptGeneratorWarningTitle };
  }
  if (prepaidCreditAmount !== null && prepaidCreditAmount !== undefined && prepaidCreditAmount <= 0) {
    return { title: promptGeneratorWarningTitle };
  }
  return null;
}

export function organizationApiDisabledReason(account: AuthAccount | null, orgUuid?: string | null) {
  const organization = currentOrganizationMembership(account, orgUuid)?.organization as Record<string, unknown> | undefined;
  return stringValue(organization?.api_disabled_reason);
}

export function currentOrganizationMembership(account: AuthAccount | null, orgUuid?: string | null) {
  const memberships = account?.memberships ?? [];
  return memberships.find((membership) => membership.organization?.uuid === orgUuid) ?? memberships[0];
}

export function hasExplicitDisabledWorkbenchProduct(settings?: Record<string, unknown>) {
  return hasExplicitFalseSetting(settings, [
    ['workbench', 'enabled'],
    ['workbench', 'access_enabled'],
    ['workbench_access', 'enabled'],
    ['enable_workbench'],
    ['workbench_enabled'],
    ['workbench_access_enabled']
  ]);
}

export function hasExplicitDisabledWorkbenchRole(settings?: Record<string, unknown>) {
  return hasExplicitFalseSetting(settings, [
    ['workbench', 'role_access_enabled'],
    ['workbench', 'user_role_access_enabled'],
    ['workbench_role_access_enabled'],
    ['workbench_user_role_access_enabled'],
    ['default_workspace_settings', 'enable_workbench']
  ]);
}

export function hasExplicitFalseSetting(settings: Record<string, unknown> | undefined, paths: string[][]) {
  return paths.some((path) => {
    const value = readSetting(settings, path);
    return value === false || value === 'false' || value === 'disabled';
  });
}

export function readSetting(settings: Record<string, unknown> | undefined, path: string[]) {
  let value: unknown = settings;
  for (const key of path) {
    if (!value || typeof value !== 'object') {
      return undefined;
    }
    value = (value as Record<string, unknown>)[key];
  }
  return value;
}

export function formatDate(value?: string) {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }).format(date);
}

export function errorMessage(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  if (error && typeof error === 'object') {
    const message = (error as Record<string, unknown>).message;
    if (typeof message === 'string' && message.trim()) {
      return message;
    }
  }
  return String(error);
}

export function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : '';
}

export function numberValue(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

export function trackWorkbenchEvent(name: string, properties: Record<string, unknown> = {}) {
  if (typeof window === 'undefined') {
    return;
  }
  window.dispatchEvent(new window.CustomEvent('workbench:analytics', { detail: { name, properties } }));
  const analytics = (window as Window & { analytics?: { track?: (eventName: string, eventProperties: Record<string, unknown>) => void } })
    .analytics;
  analytics?.track?.(name, properties);
}

export function workbenchId(prefix: string) {
  const id = typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
  return prefix ? `${prefix}_${id}` : id;
}

export function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

export function codeForRevision(language: CodeLanguage, revision: WorkbenchRevision) {
  const request = anthropicCodeRequest(revision);
  const providerRequest = { ...request, model: '' };
  const variableNames = extractVariables(revision);
  const variableNote = variableNames.length
    ? `\n# Replace placeholders like ${variableNames.map((name) => `{{${name}}}`).join(', ')} with real values,\n# because the SDK does not support variables.\n`
    : '\n';
  const tsVariableNote = variableNames.length
    ? `\n// Replace placeholders like ${variableNames.map((name) => `{{${name}}}`).join(', ')} with real values,\n// because the SDK does not support variables.\n`
    : '\n';
  switch (language) {
    case 'typescript':
      return `import Anthropic from "@anthropic-ai/sdk";

const anthropic = new Anthropic();
${tsVariableNote}
const message = await anthropic.messages.create(${JSON.stringify(request, null, 2)});

console.log(message.content);`;
    case 'curl':
      return `curl https://api.anthropic.com/v1/messages \\
  -H "x-api-key: $ANTHROPIC_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '${JSON.stringify(request, null, 2)}'`;
    case 'bedrock-python':
      return `from anthropic import AnthropicBedrock

# See https://docs.claude.com/en/api/claude-on-amazon-bedrock
# for authentication options
client = AnthropicBedrock()
${variableNote}
message = client.messages.create(${pythonKwargs(providerRequest)}

print(message.content)`;
    case 'bedrock-typescript':
      return `import AnthropicBedrock from "@anthropic-ai/bedrock-sdk";

// See https://docs.claude.com/en/api/claude-on-amazon-bedrock
// for authentication options
const client = new AnthropicBedrock();
${tsVariableNote}
const message = await client.messages.create(${JSON.stringify(providerRequest, null, 2)});

console.log(message.content);`;
    case 'vertex-python':
      return `from anthropic import AnthropicVertex

# See https://docs.claude.com/en/api/claude-on-vertex-ai
# for authentication options
client = AnthropicVertex()
${variableNote}
message = client.messages.create(${pythonKwargs(providerRequest)}

print(message.content)`;
    case 'vertex-typescript':
      return `import AnthropicVertex from "@anthropic-ai/vertex-sdk";

// See https://docs.claude.com/en/api/claude-on-vertex-ai
// for authentication options
const client = new AnthropicVertex();
${tsVariableNote}
const message = await client.messages.create(${JSON.stringify(providerRequest, null, 2)});

console.log(message.content);`;
    case 'python':
    default:
      return `import anthropic

client = anthropic.Anthropic(
    # defaults to os.environ.get("ANTHROPIC_API_KEY")
    api_key="my_api_key",
)
${variableNote}
message = client.messages.create(${pythonKwargs(request)}

print(message.content)`;
  }
}

export function anthropicCodeRequest(revision: WorkbenchRevision) {
  const request: Record<string, unknown> = {
    model: revision.model_name,
    max_tokens: revision.max_tokens_to_sample,
    messages: revision.messages.map((message) => {
      const content = cleanMessageContent(message.content, false).map(cleanCodeContentBlock);
      return {
        role: message.role === 'assistant' ? 'assistant' : 'user',
        content: content.length ? content : [cleanCodeContentBlock({ type: 'text', text: '' })]
      };
    })
  };
  if (revision.system_prompt.trim()) {
    request.system = revision.system_prompt;
  }
  if (thinkingMode(revision.thinking) !== 'disabled') {
    request.thinking = cleanCodeThinking(revision.thinking);
  } else if (typeof revision.temperature === 'number') {
    request.temperature = revision.temperature;
  }
  if (revision.tools.length) {
    request.tools = revision.tools.map(cleanTool);
  }
  return request;
}

export function cleanCodeContentBlock(block: WorkbenchContentBlock): WorkbenchContentBlock {
  const blockType = String(block.type || 'text');
  const {
    type: _type,
    text,
    source,
    title,
    cache_control,
    ...rest
  } = block;
  const ordered: WorkbenchContentBlock = { type: blockType };
  if (blockType === 'text') {
    ordered.text = typeof text === 'string' ? text : '';
  } else {
    if (source !== undefined) {
      ordered.source = source;
    }
    if (title !== undefined) {
      ordered.title = title;
    }
    if (text !== undefined) {
      ordered.text = text;
    }
  }
  if (cache_control !== undefined) {
    ordered.cache_control = cache_control;
  }
  Object.entries(rest).forEach(([key, value]) => {
    if (value !== undefined) {
      ordered[key] = value;
    }
  });
  return ordered;
}

export function cleanCodeThinking(thinking: WorkbenchThinking): WorkbenchThinking {
  const mode = thinkingMode(thinking);
  if (mode === 'disabled') {
    return { type: 'disabled' };
  }
  if (mode === 'adaptive') {
    return { type: 'adaptive', effort: thinking.effort ?? 'high' };
  }
  return {
    ...thinking,
    type: 'enabled',
    effort: thinking.effort ?? 'high',
    budget_tokens: clampThinkingBudgetTokens(thinking.budget_tokens)
  };
}

export function pythonKwargs(request: Record<string, unknown>) {
  return Object.entries(request)
    .map(([key, value]) => `\n    ${key}=${JSON.stringify(value, null, 4).replace(/\n/g, '\n    ')}`)
    .join(',') + '\n)';
}

import { type Dispatch, type SetStateAction } from 'react';
import { type Locale } from '../../../shared/i18n';
import { type QuickstartChatItem, type QuickstartToolCall, type QuickstartToolExecutionResult } from '../types';
import { parseQuestionInput } from './questionModel';

export function cleanQuickstartAssistantText(content: string) {
  return stripQuickstartInternalNarration(stripQuickstartThinking(content))
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

export function stripQuickstartThinking(content: string) {
  return content
    .replace(/<think\b[^>]*>[\s\S]*?<\/think>/gi, '')
    .replace(/<think\b[^>]*>[\s\S]*$/gi, '')
    .replace(/<\/think>/gi, '');
}

export function stripQuickstartInternalNarration(content: string) {
  const trimmed = content.trim();
  if (!trimmed) {
    return '';
  }
  if (/^(?:Search results for query:\s*)+/i.test(trimmed)) {
    return '';
  }
  return stripQuickstartInternalSentences(trimmed);
}

export function stripQuickstartInternalSentences(content: string) {
  const trimmed = content.trim();
  if (!trimmed) {
    return '';
  }
  return trimmed
    .replace(
      /(?:^|[.!?]\s+|\n+)(?:Thinking\b|The user\b|User (?:wants|chose|selected|asked|said|is|has)\b|I see\b|I (?:need|should|will|can)\b|I'll\b|Let me\b|Creating\b|Now\b)[\s\S]*$/i,
      '',
    )
    .trim();
}

export function quickstartItemId(prefix: string) {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}

export function appendQuickstartStatus(
  setChatItems: Dispatch<SetStateAction<QuickstartChatItem[]>>,
  content: string,
  tone: 'muted' | 'success' | 'error' = 'muted',
) {
  setChatItems((current) => [...current, { id: quickstartItemId('status'), type: 'status', content, tone }]);
}

export function updateQuickstartMessage(
  items: QuickstartChatItem[],
  itemId: string,
  content: string,
): QuickstartChatItem[] {
  return items.map((item) => (item.id === itemId && item.type === 'message' ? { ...item, content } : item));
}

export function updateQuickstartTool(
  setChatItems: Dispatch<SetStateAction<QuickstartChatItem[]>>,
  toolUseId: string,
  patch: Partial<QuickstartToolCall>,
) {
  setChatItems((current) =>
    current.map((item) =>
      item.type === 'tool' && item.call.id === toolUseId ? { ...item, call: { ...item.call, ...patch } } : item,
    ),
  );
}

export function awaitingQuickstartToolCalls(items: QuickstartChatItem[]) {
  return items
    .filter(
      (item): item is Extract<QuickstartChatItem, { type: 'tool' }> =>
        item.type === 'tool' && item.call.status === 'awaiting_user',
    )
    .map((item) => item.call);
}

export function hasAwaitingQuickstartQuestionSet(items: QuickstartChatItem[]) {
  return items.some(
    (item) =>
      item.type === 'tool' &&
      item.call.name === 'ask_user_questions' &&
      item.call.status === 'awaiting_user' &&
      parseQuestionInput(item.call.input).length > 0,
  );
}

export function quickstartChatReplyToolResult(call: QuickstartToolCall, reply: string, locale: Locale = 'en') {
  const zh = locale === 'zh-CN';
  if (call.name === 'build_agent_config') {
    return zh ? `用户改为发送了消息："${reply}"` : `User sent a message instead: "${reply}"`;
  }
  return zh ? `用户在聊天中回复：${reply}` : `User replied in chat: ${reply}`;
}

export function toolResultBlock(toolUseId: string, result: QuickstartToolExecutionResult) {
  return {
    type: 'tool_result',
    tool_use_id: toolUseId,
    content: result.content,
    ...(result.isError ? { is_error: true } : {}),
  };
}

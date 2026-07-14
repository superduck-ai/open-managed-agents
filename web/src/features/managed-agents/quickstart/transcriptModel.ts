import { type QuickstartChatItem } from '../types';
import { cleanQuickstartAssistantText } from './chatModel';
import { quickstartToolPresentation } from './toolPresentation';

export type QuickstartSpeaker = 'assistant' | 'user';

export type QuickstartTranscriptEntry = {
  item: QuickstartChatItem;
  speaker?: QuickstartSpeaker;
  continued: boolean;
};

export type QuickstartTranscriptPresentation = {
  entries: QuickstartTranscriptEntry[];
  lastSpeaker?: QuickstartSpeaker;
};

function visibleQuickstartItem(item: QuickstartChatItem): QuickstartChatItem | null {
  if (item.type !== 'message') {
    return item;
  }
  const content = item.role === 'assistant' ? cleanQuickstartAssistantText(item.content) : item.content;
  if (!content.trim()) {
    return null;
  }
  return content === item.content ? item : { ...item, content };
}

function quickstartItemSpeaker(item: QuickstartChatItem): QuickstartSpeaker | undefined {
  if (item.type === 'message') {
    return item.role;
  }
  if (item.type === 'create_agent_result') {
    return 'assistant';
  }
  if (item.type === 'tool' && quickstartToolPresentation(item.call.name).occupiesSpeaker) {
    return 'assistant';
  }
  return undefined;
}

export function presentQuickstartTranscript(items: QuickstartChatItem[]): QuickstartTranscriptPresentation {
  const entries: QuickstartTranscriptEntry[] = [];
  let lastSpeaker: QuickstartSpeaker | undefined;

  for (const rawItem of items) {
    const item = visibleQuickstartItem(rawItem);
    if (!item) {
      continue;
    }
    const speaker = quickstartItemSpeaker(item);
    entries.push({ item, speaker, continued: speaker !== undefined && speaker === lastSpeaker });
    if (speaker) {
      lastSpeaker = speaker;
    }
  }

  return { entries, lastSpeaker };
}

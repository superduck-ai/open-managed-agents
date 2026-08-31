import { type DisplayEventEntry, type SessionEventListEntry } from '../types';
import { sessionEventThreadId } from './sessionDetailModel';
import { sessionSubagentName } from './sessionTraceModel';

export type SessionTranscriptIteration = {
  id: string;
  bracketId: string;
  entries: SessionEventListEntry[];
};

export type SessionTranscriptBlock =
  | {
      id: string;
      kind: 'user';
      entry: DisplayEventEntry;
    }
  | {
      id: string;
      kind: 'agent';
      speakerKey: string;
      speakerLabel: string;
      headerEntry: SessionEventListEntry;
      iterations: SessionTranscriptIteration[];
    }
  | {
      id: string;
      kind: 'standalone';
      entry: SessionEventListEntry;
    };

export function buildSessionTranscriptBlocks(entries: SessionEventListEntry[]): SessionTranscriptBlock[] {
  const blocks: SessionTranscriptBlock[] = [];
  const bracketIds = entries.map((_, index) => sessionTranscriptEffectiveBracketId(entries, index));
  let agentBlock: Extract<SessionTranscriptBlock, { kind: 'agent' }> | null = null;

  const flushAgentBlock = () => {
    if (!agentBlock) {
      return;
    }
    blocks.push(agentBlock);
    agentBlock = null;
  };

  entries.forEach((entry, index) => {
    if (sessionTranscriptEntryIsBoundary(entry)) {
      flushAgentBlock();
      blocks.push(sessionTranscriptBoundaryBlock(entry));
      return;
    }

    const bracketId = bracketIds[index];
    if (!bracketId || !sessionTranscriptEntryBelongsToAgent(entry)) {
      flushAgentBlock();
      blocks.push(sessionTranscriptStandaloneBlock(entry));
      return;
    }

    const identity = sessionTranscriptEntryIdentity(entry);
    if (
      agentBlock &&
      (sessionTranscriptSpeakerChanged(agentBlock, identity) ||
        sessionTranscriptTurnChanged(agentBlock.headerEntry, entry))
    ) {
      flushAgentBlock();
    }
    if (!agentBlock) {
      agentBlock = {
        id: `agent-turn-${entry.id}`,
        kind: 'agent',
        speakerKey: identity.laneId,
        speakerLabel: identity.label || 'Agent',
        headerEntry: entry,
        iterations: [],
      };
    } else if (identity.label && agentBlock.speakerLabel === 'Agent') {
      agentBlock.speakerLabel = identity.label;
    }
    if (identity.laneId && !agentBlock.speakerKey) {
      agentBlock.speakerKey = identity.laneId;
    }

    const latestIteration = agentBlock.iterations.at(-1);
    if (latestIteration?.bracketId === bracketId) {
      latestIteration.entries.push(entry);
      return;
    }
    agentBlock.iterations.push({
      id: `iteration-${bracketId}-${entry.id}`,
      bracketId,
      entries: [entry],
    });
  });

  flushAgentBlock();
  return blocks;
}

function sessionTranscriptTurnChanged(current: SessionEventListEntry, next: SessionEventListEntry) {
  return (
    'turnId' in current && 'turnId' in next && Boolean(current.turnId && next.turnId && current.turnId !== next.turnId)
  );
}

export function filterSessionTranscriptBlocks(
  blocks: SessionTranscriptBlock[],
  isVisible: (entry: SessionEventListEntry) => boolean,
): SessionTranscriptBlock[] {
  const visibleBlocks: SessionTranscriptBlock[] = [];
  blocks.forEach((block) => {
    if (block.kind !== 'agent') {
      if (isVisible(block.entry)) {
        visibleBlocks.push(block);
      }
      return;
    }
    const iterations = block.iterations.flatMap((iteration) => {
      const entries = iteration.entries.filter(isVisible);
      return entries.length ? [{ ...iteration, entries }] : [];
    });
    if (iterations.length) {
      visibleBlocks.push({ ...block, iterations });
    }
  });
  return visibleBlocks;
}

function sessionTranscriptEffectiveBracketId(entries: SessionEventListEntry[], index: number) {
  const direct = sessionTranscriptEntryBracketId(entries[index]);
  if (direct || !sessionTranscriptEntryIsThinking(entries[index])) {
    return direct;
  }
  for (let nextIndex = index + 1; nextIndex < entries.length; nextIndex += 1) {
    const next = entries[nextIndex];
    if (sessionTranscriptEntryIsBoundary(next)) {
      return '';
    }
    const nextBracket = sessionTranscriptEntryBracketId(next);
    if (nextBracket) {
      return nextBracket;
    }
    if (!sessionTranscriptEntryIsThinking(next)) {
      return '';
    }
  }
  return '';
}

export function sessionTranscriptEntryBracketId(entry: SessionEventListEntry) {
  if (entry.kind === 'tool_call') {
    return entry.bracketId;
  }
  if (entry.kind === 'tool_batch') {
    const brackets = new Set(entry.calls.map((call) => call.bracketId).filter(Boolean));
    return brackets.size === 1 && entry.calls.every((call) => call.bracketId) ? [...brackets][0] : '';
  }
  if ('bracketId' in entry) {
    return entry.bracketId ?? '';
  }
  return '';
}

export function sessionTranscriptEntryIsBoundary(entry: SessionEventListEntry) {
  if (
    entry.kind === 'idle_gap' ||
    entry.kind === 'queued_boundary' ||
    entry.kind === 'outcome' ||
    entry.kind === 'status'
  ) {
    return true;
  }
  if (!('displayEvent' in entry)) {
    return false;
  }
  return entry.displayEvent.type === 'user' || entry.displayEvent.type.startsWith('status_');
}

function sessionTranscriptEntryBelongsToAgent(entry: SessionEventListEntry) {
  if (entry.kind === 'tool_call' || entry.kind === 'tool_batch') {
    return true;
  }
  return (
    'displayEvent' in entry &&
    (entry.displayEvent.type === 'agent' ||
      entry.displayEvent.type === 'thinking' ||
      entry.displayEvent.type === 'tool_use')
  );
}

function sessionTranscriptEntryIsThinking(entry: SessionEventListEntry) {
  return 'displayEvent' in entry && entry.displayEvent.type === 'thinking';
}

function sessionTranscriptBoundaryBlock(entry: SessionEventListEntry): SessionTranscriptBlock {
  if ('displayEvent' in entry && entry.displayEvent.type === 'user') {
    return { id: `user-turn-${entry.id}`, kind: 'user', entry: entry as DisplayEventEntry };
  }
  return sessionTranscriptStandaloneBlock(entry);
}

function sessionTranscriptStandaloneBlock(entry: SessionEventListEntry): SessionTranscriptBlock {
  return { id: `standalone-${entry.id}`, kind: 'standalone', entry };
}

function sessionTranscriptSpeakerChanged(
  block: Extract<SessionTranscriptBlock, { kind: 'agent' }>,
  identity: { laneId: string; label: string },
) {
  if (block.speakerKey && identity.laneId && block.speakerKey !== identity.laneId) {
    return true;
  }
  return Boolean(identity.label && block.speakerLabel !== 'Agent' && block.speakerLabel !== identity.label);
}

function sessionTranscriptEntryIdentity(entry: SessionEventListEntry) {
  if (!('event' in entry)) {
    return { laneId: '', label: '' };
  }
  const laneId = sessionEventThreadId(entry.event);
  const label =
    entry.displayEvent.type === 'agent'
      ? entry.displayEvent.label || sessionSubagentName(entry.event)
      : sessionSubagentName(entry.event);
  return { laneId, label };
}

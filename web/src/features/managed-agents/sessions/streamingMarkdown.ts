export type StreamingMarkdownSnapshot = {
  markdown: string;
  pending: string;
};

const PARTIAL_FENCE = /^( {0,3})(`{1,2}|~{1,2})[ \t]*$/;
const FENCE_MARKER = /^( {0,3})(`{3,}|~{3,})(.*)$/;
const FENCE_CLOSER = /^( {0,3})(`{3,}|~{3,})[ \t]*$/;
const LIST_MARKER_ONLY = /^[ \t]{0,3}(?:[-+*]|\d{1,9}[.)])[ \t]*$/;
const PARTIAL_THEMATIC_BREAK = /^[ \t]{0,3}(?:[-*_]){1,2}[ \t]*$/;

export function streamingMarkdownSnapshot(value: string): StreamingMarkdownSnapshot {
  const openMarker = unclosedFenceMarker(value);
  if (openMarker) {
    const suffix = value.endsWith('\n') ? openMarker : `\n${openMarker}`;
    return { markdown: `${value}${suffix}`, pending: '' };
  }
  return splitTrailingUnsafe(value);
}

function unclosedFenceMarker(value: string) {
  const { lines } = documentLines(value);
  let openMarker: string | null = null;
  for (const line of lines) {
    openMarker = nextFenceMarker(openMarker, line);
  }
  return openMarker;
}

function nextFenceMarker(openMarker: string | null, line: string) {
  if (openMarker) {
    return isClosingFence(line, openMarker) ? null : openMarker;
  }
  return openingFenceMarker(line);
}

function openingFenceMarker(line: string) {
  const match = FENCE_MARKER.exec(line);
  if (!match) {
    return null;
  }
  const marker = match[2] ?? '';
  const info = match[3] ?? '';
  if (marker.startsWith('`') && info.includes('`')) {
    return null;
  }
  return marker;
}

function isClosingFence(line: string, openMarker: string) {
  const match = FENCE_CLOSER.exec(line);
  if (!match) {
    return false;
  }
  const marker = match[2] ?? '';
  return marker.startsWith(openMarker[0] ?? '') && marker.length >= openMarker.length;
}

function splitTrailingUnsafe(value: string): StreamingMarkdownSnapshot {
  const { lines, endsWithNewline } = documentLines(value);
  const last = lines[lines.length - 1];
  if (last !== undefined && !endsWithNewline && isUnsafeOpenLine(last)) {
    return holdFrom(lines, lines.length - 1);
  }
  return holdIncompleteTable(lines, endsWithNewline) ?? { markdown: value, pending: '' };
}

function isUnsafeOpenLine(line: string) {
  return isPartialFence(line) || isListMarkerOnly(line) || isPartialThematicBreak(line) || isUnbalancedInlineCode(line);
}

function isPartialFence(line: string) {
  return PARTIAL_FENCE.test(line);
}

function isListMarkerOnly(line: string) {
  return LIST_MARKER_ONLY.test(line);
}

function isPartialThematicBreak(line: string) {
  return PARTIAL_THEMATIC_BREAK.test(line);
}

function isUnbalancedInlineCode(line: string) {
  return !FENCE_CLOSER.test(line) && oddBacktickCount(line);
}

function oddBacktickCount(line: string) {
  let count = 0;
  for (let index = 0; index < line.length; index += 1) {
    if (line[index] === '\\') {
      index += 1;
      continue;
    }
    if (line[index] === '`') {
      count += 1;
    }
  }
  return count % 2 === 1;
}

function holdIncompleteTable(lines: string[], endsWithNewline: boolean): StreamingMarkdownSnapshot | null {
  const start = trailingTableStart(lines);
  if (start === null) {
    return null;
  }
  const block = lines.slice(start);
  if (!block.some(isTableDelimiter)) {
    return holdFrom(lines, start);
  }
  if (endsWithNewline) {
    return null;
  }
  const last = lines[lines.length - 1] ?? '';
  if (isTableDelimiter(last) || isCompleteTableRow(last)) {
    return null;
  }
  return holdFrom(lines, lines.length - 1);
}

function trailingTableStart(lines: string[]) {
  let start: number | null = null;
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    if (!isTableish(lines[index] ?? '')) {
      break;
    }
    start = index;
  }
  return start;
}

function isTableish(line: string) {
  return isTableDelimiter(line) || line.trim().startsWith('|');
}

function isTableDelimiter(line: string) {
  const trimmed = line.trim();
  if (!trimmed.includes('|') || !trimmed.includes('-')) {
    return false;
  }
  const cells = trimmed.replace(/^\|/, '').replace(/\|$/, '').split('|');
  return cells.every((cell) => /^:?-+:?$/.test(cell.trim()));
}

function isCompleteTableRow(line: string) {
  const trimmed = line.trim();
  const pipes = trimmed.match(/\|/g);
  return trimmed.startsWith('|') && trimmed.endsWith('|') && (pipes?.length ?? 0) >= 2 && !isTableDelimiter(line);
}

function holdFrom(lines: string[], index: number): StreamingMarkdownSnapshot {
  return {
    markdown: lines.slice(0, index).join('\n'),
    pending: lines.slice(index).join('\n'),
  };
}

function documentLines(value: string) {
  const endsWithNewline = value.endsWith('\n');
  const lines = value.split('\n');
  if (endsWithNewline) {
    lines.pop();
  }
  return { lines, endsWithNewline };
}

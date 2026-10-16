import type { FileMetadataApiResponse, SessionEventListEntry, SessionResourceApiResponse } from '../types';

export type SessionOutputFile = FileMetadataApiResponse & { path?: string };

function sessionOutputPath(value: string) {
  return value.replace(/^sandbox:/, '').replace(/^\/mnt\/user-data\/outputs\//, '/outputs/');
}

function sessionOutputLinkPath(value: string) {
  try {
    return sessionOutputPath(decodeURIComponent(value));
  } catch {
    return '';
  }
}

export function sessionOutputFilePaths(files: FileMetadataApiResponse[], resources: SessionResourceApiResponse[]) {
  const paths = new Map(
    resources
      .filter((resource) => resource.file_id && resource.mount_path)
      .map((resource) => [resource.file_id, resource.mount_path]),
  );
  return files.map((file) => ({ ...file, path: paths.get(file.id) }));
}

export function sessionOutputFileForPath(files: SessionOutputFile[], href: string) {
  const path = sessionOutputLinkPath(href);
  return files.find((file) => file.path && sessionOutputPath(file.path) === path);
}

export function sessionMessageOutputFiles(
  entries: SessionEventListEntry[],
  files: SessionOutputFile[],
  running: boolean,
) {
  const turns: SessionEventListEntry[][] = [];
  for (const entry of entries) {
    if ('displayEvent' in entry && entry.displayEvent.type === 'user') turns.push([]);
    if (!turns.length) turns.push([]);
    turns.at(-1)!.push(entry);
  }
  const result = new Map<string, SessionOutputFile[]>();
  turns.forEach((turn, index) => {
    const messages = turn.filter((entry) => entry.kind === 'message' && entry.displayEvent.type === 'agent');
    const final = messages.at(-1);
    if (!final || final.kind !== 'message') return;
    if (turn.some(sessionOutputEntryIsOpen) || (running && index === turns.length - 1)) return;
    const start = turn[0].createdAtMs;
    const next = turns[index + 1]?.[0].createdAtMs ?? Infinity;
    const text = messages.map((entry) => ('displayEvent' in entry ? entry.displayEvent.content : '')).join('\n');
    const referencedPaths = [...text.matchAll(/\]\(([^)\n]+)\)|`([^`\n]+)`/g)].map((match) =>
      match[1] ? sessionOutputLinkPath(match[1]) : sessionOutputPath(match[2]),
    );
    const outputs = files.filter((file) => {
      const created = Date.parse(file.created_at);
      const mentioned = file.path && referencedPaths.includes(sessionOutputPath(file.path));
      return mentioned || (created >= start && created < next);
    });
    if (outputs.length) result.set(final.id, outputs);
  });
  return result;
}

function sessionOutputEntryIsOpen(entry: SessionEventListEntry): boolean {
  if (entry.kind === 'tool_batch') return entry.calls.some(sessionOutputEntryIsOpen);
  if (entry.kind === 'tool_call') return entry.lifecycle === 'running' || entry.lifecycle === 'awaiting_approval';
  return 'displayEvent' in entry && Boolean(entry.inProgress || entry.displayEvent.isStreaming || entry.bracketOpen);
}

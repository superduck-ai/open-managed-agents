import { type SessionEventListEntry } from '../types';
import { sessionEventEntryMatchesSelectedId, sessionEventEntrySelectionId } from './sessionDetailModel';

export function sessionTraceKeyboardTarget(
  entries: SessionEventListEntry[],
  selectedEntryId: string | null,
  key: string,
) {
  const selectableEntries = entries.filter((entry) => sessionEventEntrySelectionId(entry) !== null);
  if (!selectableEntries.length) {
    return null;
  }
  const currentIndex = selectableEntries.findIndex((entry) =>
    sessionEventEntryMatchesSelectedId(entry, selectedEntryId),
  );
  let nextIndex: number;
  switch (key) {
    case 'ArrowDown':
    case 'j':
      nextIndex = currentIndex < 0 ? 0 : Math.min(currentIndex + 1, selectableEntries.length - 1);
      break;
    case 'ArrowUp':
    case 'k':
      nextIndex = currentIndex < 0 ? selectableEntries.length - 1 : Math.max(currentIndex - 1, 0);
      break;
    case 'Home':
      nextIndex = 0;
      break;
    case 'End':
      nextIndex = selectableEntries.length - 1;
      break;
    default:
      return null;
  }
  const entry = selectableEntries[nextIndex];
  const selectionId = sessionEventEntrySelectionId(entry);
  return selectionId ? { entry, selectionId } : null;
}

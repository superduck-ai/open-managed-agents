import type { SessionApiResponse } from '../types';
import { SessionListEmpty } from './SessionListEmpty';
import { SessionListTable, type SessionListActions } from './SessionListTable';

export function SessionListContent({
  filtered,
  error,
  onCreate,
  onReset,
  onRetry,
  ...tableProps
}: {
  sessions: SessionApiResponse[];
  agentNames?: ReadonlyMap<string, string>;
  workspaceId: string;
  loading: boolean;
  busyAction: string | null;
  actions: SessionListActions;
  filtered: boolean;
  error: string | null;
  onCreate: () => void;
  onReset: () => void;
  onRetry: () => void;
}) {
  return (
    <>
      <SessionListTable {...tableProps} />
      {!tableProps.loading && !tableProps.sessions.length ? (
        <SessionListEmpty filtered={filtered} error={error} onCreate={onCreate} onReset={onReset} onRetry={onRetry} />
      ) : null}
    </>
  );
}

import { useEffect, useRef } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { Button } from '../../../shared/ui/button';
import { environmentQueryKey } from './data';
import type { PrebuildStage } from './prebuild';

type LogResponse = { text: string; next_cursor: string; complete: boolean };

const ansiColors = new RegExp(`${String.fromCharCode(27)}\\[[\\d;:]*m`, 'g');

export function EnvironmentPrebuildLogs({
  environmentId,
  workspaceId,
  jobId,
  stage,
}: {
  environmentId: string;
  workspaceId: string;
  jobId: string;
  stage: PrebuildStage;
}) {
  const { msg } = useI18n();
  const logRef = useRef<HTMLPreElement>(null);
  const followTail = useRef(true);
  const query = useInfiniteQuery({
    queryKey: [...environmentQueryKey(workspaceId), 'prebuild-logs', environmentId, jobId, stage],
    initialPageParam: '',
    queryFn: async ({ pageParam, signal }) => {
      const chunk = await anthropicBetaApi.environments.prebuild.logs<LogResponse>(
        environmentId,
        workspaceId,
        { stage, job_id: jobId, cursor: pageParam },
        signal,
      );
      return { text: chunk.text, nextCursor: chunk.next_cursor, complete: chunk.complete };
    },
    getNextPageParam: (last) => (last.complete ? undefined : last.nextCursor),
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const last = query.data?.pages.at(-1);
  const cursor = query.data?.pageParams.at(-1);
  const { hasNextPage, isFetching, isError, fetchNextPage } = query;
  useEffect(() => {
    if (!hasNextPage || isFetching || isError) return;
    // Drain available pages immediately; wait only when caught up with a live step.
    const delay = last?.text || last?.nextCursor !== cursor ? 0 : 3000;
    const timer = setTimeout(() => void fetchNextPage(), delay);
    return () => clearTimeout(timer);
  }, [last, cursor, hasNextPage, isFetching, isError, fetchNextPage]);
  const text = (query.data?.pages.map((page) => page.text).join('') || '').replace(ansiColors, '');
  useEffect(() => {
    if (followTail.current && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [text]);
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/30">
      <pre
        ref={logRef}
        onScroll={({ currentTarget }) => {
          followTail.current = currentTarget.scrollHeight - currentTarget.scrollTop - currentTarget.clientHeight < 32;
        }}
        className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-relaxed"
      >
        {text ||
          (query.isPending ? msg('common.loading', 'Loading...') : msg('environmentPrebuild.noLogs', 'No logs yet.'))}
      </pre>
      {query.isError ? (
        <p role="alert" className="px-4 py-2 text-xs text-destructive">
          {msg('environmentPrebuild.logsFailed', 'Could not load logs.')}
        </p>
      ) : null}
      {query.isError ? (
        <div className="flex shrink-0 justify-end border-t border-border px-3 py-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void (query.hasNextPage ? query.fetchNextPage() : query.refetch())}
          >
            {query.isFetching ? msg('common.loading', 'Loading...') : msg('common.retry', 'Retry')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

import { ArrowLeft, RefreshCw } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { type ObservabilityScope } from './api';
import { useTrajectoryBrowser } from './useTrajectoryBrowser';
import { NativeTrajectory } from './trajectory/NativeTrajectory';

export function ObservabilityPage({ scope }: { scope: ObservabilityScope }) {
  const { locale, msg } = useI18n();
  const { queryScope, scopeKey, current, select, query, next, previous, hasPrevious, ready } =
    useTrajectoryBrowser(scope);
  if (!ready)
    return (
      <p role="status" className="p-3 text-sm text-muted-foreground">
        {msg('trajectory.workspaceRequired', 'Select a workspace to view trajectories.')}
      </p>
    );
  return (
    <section className="flex h-full min-h-[500px] min-w-0 flex-col gap-3 py-3">
      <div className="flex items-center gap-2 px-3">
        {current && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => select(null)}
            aria-label={msg('trajectory.back', 'Back')}
          >
            <ArrowLeft />
          </Button>
        )}
        <h2 className="min-w-0 flex-1 truncate text-sm font-medium">
          {msg('trajectory.title', 'Trajectory')}
          {current ? ` · ${current}` : ''}
        </h2>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => void query.refetch()}
          aria-label={msg('trajectory.refresh', 'Refresh')}
        >
          <RefreshCw />
        </Button>
      </div>
      {query.isError && (
        <div role="alert" className="px-3 text-sm text-destructive">
          {msg('trajectory.loadError', 'Could not load trajectories. Please try again later.')}
        </div>
      )}
      {current ? (
        <NativeTrajectory key={`${scopeKey}:${current}`} scope={queryScope} id={current} />
      ) : (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{msg('trajectory.title', 'Trajectory')}</TableHead>
                <TableHead>{msg('trajectory.session', 'Session')}</TableHead>
                <TableHead>{msg('trajectory.lastEvent', 'Last event')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data?.items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <Button variant="link" onClick={() => select(item.id)}>
                      {item.id}
                    </Button>
                  </TableCell>
                  <TableCell className="max-w-48 truncate">{item.session_id}</TableCell>
                  <TableCell>{new Date(item.last_event_at).toLocaleString(locale)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!query.isPending && !query.isError && !query.data?.items.length && (
            <p className="px-3 text-sm text-muted-foreground">
              {msg('trajectory.empty', 'No DSH trajectory events in the last month.')}
            </p>
          )}
          <div className="flex gap-2 px-3">
            <Button variant="outline" size="sm" disabled={!hasPrevious || query.isFetching} onClick={previous}>
              {msg('trajectory.previous', 'Previous')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={query.data?.next_offset == null || query.isFetching}
              onClick={next}
            >
              {msg('trajectory.next', 'Next')}
            </Button>
          </div>
        </>
      )}
    </section>
  );
}

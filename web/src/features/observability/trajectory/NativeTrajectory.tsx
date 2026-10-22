import { useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import type { QueryScope } from '../api';
import { pollTrajectory } from './poll';
import { createProjection, type Projection } from './projection';
import type { NativeStore } from './native-types';
import './native-theme.css';

const sessionState = { openState: 'open', hasMore: false, loadingOlder: false };

function NativeView({ projection, snapshot }: { projection: Projection; snapshot: unknown }) {
  const injected = useMemo(() => projection.view.inject('trajectory'), [projection]);
  const Component = projection.view.Component;
  function useDuration<T>(selector: (value: boolean) => T) {
    const source: NativeStore<boolean> = injected.hooks.duration;
    return selector(useSyncExternalStore(source.subscribe, source.getSnapshot, source.getSnapshot));
  }
  return (
    <Component
      useTrajectory={(selector: (value: unknown) => unknown) => selector(snapshot)}
      useSession={(selector: (value: typeof sessionState) => unknown) => selector(sessionState)}
      useDuration={useDuration}
      loadOlder={async () => false}
      loadImage={async () => {
        throw new Error('Attachment preview is unavailable');
      }}
      setActualDuration={injected.setActualDuration}
      jsonStringWrapping={injected.jsonStringWrapping}
      viewRequest={null}
      completeViewRequest={() => {}}
      renderSlot={() => null}
      t={projection.translate}
    />
  );
}

export function NativeTrajectory({ scope, id }: { scope: QueryScope; id: string }) {
  const { locale, msg } = useI18n();
  const [state, setState] = useState<{ projection?: Projection; snapshot?: unknown; error?: 'retrying' | 'stopped' }>(
    {},
  );
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    let stopPolling: (() => void) | undefined;
    setState({});
    const run = async () => {
      try {
        const [{ default: native }, { default: trajectory }] = await Promise.all([
          import('harness:@deepseek-ai/dsh-client-ui-conversation'),
          import('harness:@deepseek-ai/dsh-client-ui-trajectory'),
        ]);
        if (abort.signal.aborted) return;
        const projection = createProjection(native, trajectory, locale);
        stopPolling = pollTrajectory(
          scope,
          id,
          abort.signal,
          (page) => {
            const snapshot = projection.append(page.events);
            setState({ projection, snapshot });
          },
          (stopped) => setState((current) => ({ ...current, error: stopped ? 'stopped' : 'retrying' })),
        );
      } catch {
        if (!abort.signal.aborted) setState({ error: 'stopped' });
      }
    };
    void run();
    return () => {
      abort.abort();
      stopPolling?.();
    };
  }, [scope, id, locale, revision]);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {state.error && (
        <div role="alert" className="flex items-center gap-2 p-3 text-sm text-destructive">
          {state.error === 'stopped'
            ? msg(
                'trajectory.pollingStopped',
                'Could not load trajectory. Automatic updates are paused. Retry to resume.',
              )
            : msg('trajectory.retrying', 'Could not update trajectory. Retrying shortly.')}
          <Button variant="outline" size="sm" onClick={() => setRevision((value) => value + 1)}>
            {msg('trajectory.retry', 'Retry')}
          </Button>
        </div>
      )}
      {state.projection ? (
        <div className="harness-trajectory min-h-[400px] flex-1">
          <NativeView projection={state.projection} snapshot={state.snapshot} />
        </div>
      ) : (
        !state.error && (
          <div className="p-3 text-sm text-muted-foreground">{msg('trajectory.loading', 'Loading trajectory…')}</div>
        )
      )}
    </div>
  );
}

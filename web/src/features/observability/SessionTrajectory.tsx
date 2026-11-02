import { useMemo } from 'react';
import { useI18n } from '../../shared/i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { useWorkspace } from '../../shared/workspaces/context';
import type { TrajectorySegment } from './api';
import { NativeTrajectory } from './trajectory/NativeTrajectory';
import { useSessionTrajectoryLane } from './useSessionTrajectoryLane';

export function SessionTrajectory({
  sessionId,
  threadId,
  laneLabel,
}: {
  sessionId: string;
  threadId?: string;
  laneLabel?: string;
}) {
  const { locale, msg } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const { viewKey, target, onPage, select, segments, selectedIndex } = useSessionTrajectoryLane(sessionId, threadId);
  const scope = useMemo(
    () => ({ orgUuid: orgUuid ?? '', workspaceId: activeWorkspaceId }),
    [orgUuid, activeWorkspaceId],
  );
  if (!orgUuid || !activeWorkspaceId)
    return (
      <p role="status" className="p-3 text-sm text-muted-foreground">
        {msg('trajectory.workspaceRequired', 'Select a workspace to view trajectories.')}
      </p>
    );
  const selected = segments[selectedIndex];
  const segmentLabel = (segment: TrajectorySegment, index: number) =>
    msg('trajectory.segmentLabel', 'Run {index} · {time}', {
      index: index + 1,
      time: new Date(segment.started_at).toLocaleString(locale),
    });
  return (
    <section className="flex h-full min-h-[500px] min-w-0 flex-col gap-3 py-3">
      <div className="flex items-center gap-2 px-3">
        <h2 className="min-w-0 flex-1 truncate text-sm font-medium">
          {msg('trajectory.title', 'Trajectory')}
          {laneLabel ? ` · ${laneLabel}` : ''}
        </h2>
        {segments.length > 1 && selected && (
          <Select<string> value={selected.id} onValueChange={(value) => value && select(value)}>
            <SelectTrigger size="sm" aria-label={msg('trajectory.segment', 'Run')} className="max-w-56 text-xs">
              <SelectValue>{segmentLabel(selected, selectedIndex)}</SelectValue>
            </SelectTrigger>
            <SelectContent align="end" alignItemWithTrigger={false}>
              {segments.map((segment, index) => (
                <SelectItem key={segment.id} value={segment.id} label={segmentLabel(segment, index)}>
                  {segmentLabel(segment, index)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      <NativeTrajectory key={viewKey} scope={scope} target={target} onPage={onPage} />
    </section>
  );
}

import { trajectoryPage, type QueryScope, type TrajectoryPage, type TrajectoryTarget } from '../api';

export function pollTrajectory(
  scope: QueryScope,
  target: TrajectoryTarget,
  signal: AbortSignal,
  onPage: (page: TrajectoryPage) => void,
  onFailure: (stopped: boolean) => void,
) {
  let after = -1;
  let failures = 0;
  let segment = target.segment;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const poll = async () => {
    if (signal.aborted) return;
    try {
      let more = true;
      while (more && !signal.aborted) {
        const page = await trajectoryPage(scope, { ...target, segment }, after, signal);
        if (signal.aborted) return;
        segment ||= page.segment || undefined;
        onPage(page);
        after = page.next_seq;
        more = page.has_more;
        failures = 0;
      }
    } catch {
      if (signal.aborted) return;
      failures++;
      onFailure(failures >= 3);
    }
    if (!signal.aborted && failures < 3) {
      timer = setTimeout(() => void poll(), failures ? 1000 * 2 ** failures : 1000);
    }
  };
  void poll();
  return () => clearTimeout(timer);
}

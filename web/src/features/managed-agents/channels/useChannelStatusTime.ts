import { useEffect, useState } from 'react';
import type { Channel } from './api';
import { channelConnectionDeadline } from './status';

export function useChannelStatusTime(channels: Channel | readonly Channel[] | null | undefined, dataUpdatedAt: number) {
  const [timerTime, setTimerTime] = useState(Date.now);
  const observedAt = Math.max(dataUpdatedAt, timerTime);

  useEffect(() => {
    if (!channels) return;
    let nextDeadline = Infinity;
    for (const channel of 'id' in channels ? [channels] : channels) {
      const deadline = channelConnectionDeadline(channel);
      if (deadline !== undefined && deadline > observedAt) nextDeadline = Math.min(nextDeadline, deadline);
    }
    if (!Number.isFinite(nextDeadline)) return;
    const timer = window.setTimeout(() => setTimerTime(Date.now()), Math.max(1, nextDeadline - Date.now()));
    return () => window.clearTimeout(timer);
  }, [channels, observedAt]);

  return observedAt;
}

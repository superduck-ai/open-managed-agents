import { useLayoutEffect, useRef } from 'react';
import { useMessageScroller } from '../../../shared/ui/message-scroller';

export function FollowSentSessionMessage({ version }: { version: number }) {
  const { scrollToEnd } = useMessageScroller();
  const previousVersion = useRef(version);
  useLayoutEffect(() => {
    if (version !== previousVersion.current) {
      previousVersion.current = version;
      scrollToEnd({ behavior: 'auto' });
    }
  }, [scrollToEnd, version]);
  return null;
}

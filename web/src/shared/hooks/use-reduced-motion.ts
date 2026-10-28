import { useSyncExternalStore } from 'react';

const reducedMotionQuery = '(prefers-reduced-motion: reduce)';

function subscribeToReducedMotion(onChange: () => void) {
  const media = typeof window === 'undefined' ? undefined : window.matchMedia?.(reducedMotionQuery);
  media?.addEventListener?.('change', onChange);
  return () => media?.removeEventListener?.('change', onChange);
}

function getReducedMotion() {
  return typeof window !== 'undefined' && Boolean(window.matchMedia?.(reducedMotionQuery).matches);
}

export function useReducedMotion() {
  return useSyncExternalStore(subscribeToReducedMotion, getReducedMotion, () => false);
}

import { useEffect } from 'react';

const guards = new Set<() => boolean>();

export function useScopeUnsavedChanges(...dirtyFlags: boolean[]) {
  const dirty = dirtyFlags.some(Boolean);
  useEffect(() => {
    const guard = () => dirty;
    guards.add(guard);
    return () => {
      guards.delete(guard);
    };
  }, [dirty]);
}

export function hasScopeUnsavedChanges() {
  return [...guards].some((guard) => guard());
}

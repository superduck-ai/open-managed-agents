import { useEffect, useRef } from 'react';

export interface ShortcutHandlers {
  shiftBack: () => void;
  shiftForward: () => void;
  zoomOut: () => void;
  refresh: () => void;
  viewHovered: () => void;
  exitView: () => void;
}

const SEQUENCES: Record<string, keyof ShortcutHandlers> = {
  tArrowLeft: 'shiftBack',
  tArrowRight: 'shiftForward',
  tz: 'zoomOut',
  dr: 'refresh',
};

function ignored(event: KeyboardEvent) {
  const target = event.target instanceof Element ? event.target : null;
  if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return true;
  if (target?.closest('input, textarea, select, [contenteditable="true"]')) return true;
  if (event.key === 'Escape') return false;
  return !!target?.closest('[role="dialog"], [role="menu"]') || !!document.querySelector('[role="dialog"]');
}

export function useDashboardShortcuts(handlers: ShortcutHandlers) {
  const latest = useRef(handlers);
  useEffect(() => {
    latest.current = handlers;
  });
  useEffect(() => {
    let prefix = '';
    let timer = 0;
    const onKey = (event: KeyboardEvent) => {
      if (ignored(event)) return;
      const action = SEQUENCES[`${prefix}${event.key}`];
      window.clearTimeout(timer);
      prefix = '';
      if (action) {
        event.preventDefault();
        latest.current[action]();
      } else if (event.key === 't' || event.key === 'd') {
        prefix = event.key;
        timer = window.setTimeout(() => (prefix = ''), 1_000);
      } else if (event.key === 'v') latest.current.viewHovered();
      else if (event.key === 'Escape') latest.current.exitView();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.clearTimeout(timer);
    };
  }, []);
}

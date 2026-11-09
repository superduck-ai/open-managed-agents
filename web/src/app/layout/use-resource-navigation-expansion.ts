import { useEffect, useState } from 'react';

function resourceNavigationMedia() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return [];
  return ['(max-width: 1100px)', '(max-height: 780px)'].map((query) => window.matchMedia(query));
}

export function useResourceNavigationExpansion(currentPath: string, active: boolean) {
  const [expanded, setExpanded] = useState(() => active || !resourceNavigationMedia().some((media) => media.matches));

  useEffect(() => {
    const media = resourceNavigationMedia();
    const update = () => setExpanded(active || !media.some((item) => item.matches));
    update();
    for (const item of media) item.addEventListener?.('change', update);
    return () => {
      for (const item of media) item.removeEventListener?.('change', update);
    };
  }, [currentPath, active]);

  return { expanded, setExpanded };
}

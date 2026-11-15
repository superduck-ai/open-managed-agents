import { useEffect, useState } from 'react';

const compactQuery = '(max-width: 1040px)';

function getExpanded() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return true;
  }
  return !window.matchMedia(compactQuery).matches;
}

export function useResponsiveSidebar() {
  const [expanded, setExpanded] = useState(getExpanded);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return;
    }
    const media = window.matchMedia(compactQuery);
    const update = () => setExpanded(!media.matches);
    update();
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);

  return expanded;
}

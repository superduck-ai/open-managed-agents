import { useNavigate, useLocation } from '@tanstack/react-router';
import { useEffect } from 'react';

export function LegacySettingsRedirect({ to }: { to: '/settings/llm' | '/settings/telemetry' | '/settings/members' }) {
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    void navigate({
      href: to + location.searchStr + (location.hash ? `#${location.hash}` : ''),
      replace: true,
    });
  }, [navigate, to, location.searchStr, location.hash]);
  return null;
}

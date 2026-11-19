import { PanelRight } from 'lucide-react';

import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';

export function QuickViewButton({
  name,
  className,
  onClick,
}: {
  name: string;
  className?: string;
  onClick: () => void;
}) {
  const { msg } = useI18n();
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      className={className}
      onClick={onClick}
      aria-label={msg('managedAgents.common.quickViewResource', 'Quick view {name}', { name })}
      aria-haspopup="dialog"
    >
      <span className="hidden sm:inline">{msg('managedAgents.common.quickView', 'Quick view')}</span>
      <PanelRight aria-hidden />
    </Button>
  );
}

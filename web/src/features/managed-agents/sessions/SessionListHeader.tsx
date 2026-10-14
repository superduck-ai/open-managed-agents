import { Plus, RotateCcw } from 'lucide-react';
import type { ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';

export function SessionListHeader({
  title,
  description,
  filters,
  loading,
  onCreate,
  onRefresh,
}: {
  title: string;
  description: string;
  filters: ReactNode;
  loading: boolean;
  onCreate: () => void;
  onRefresh: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="mb-4 space-y-5" data-testid="session-list-header">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="font-serif text-[28px] font-medium leading-tight tracking-tight">{title}</h1>
          <p className="mt-1.5 text-sm text-muted-foreground">{description}</p>
        </div>
        <Button size="sm" onClick={onCreate}>
          <Plus className="size-4" aria-hidden />
          {msg('managedAgents.sessions.createLabel', 'Create session')}
        </Button>
      </header>
      <div className="flex flex-wrap items-center gap-2">
        {filters}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="ml-auto"
          disabled={loading}
          aria-label={msg('common.refresh', 'Refresh')}
          onClick={onRefresh}
        >
          <RotateCcw className="size-4" aria-hidden />
        </Button>
      </div>
    </div>
  );
}

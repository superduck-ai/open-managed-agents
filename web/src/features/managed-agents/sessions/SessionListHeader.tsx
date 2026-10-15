import { Plus, RotateCcw } from 'lucide-react';
import type { ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { ResourcePageHeader } from '../../../shared/ui/resource-page-header';

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
    <div className="mb-7" data-testid="session-list-header">
      <ResourcePageHeader
        title={title}
        description={description}
        actions={
          <Button size="lg" onClick={onCreate}>
            <Plus className="size-4" aria-hidden />
            {msg('managedAgents.sessions.createLabel', 'Create session')}
          </Button>
        }
      />
      <div className="flex flex-wrap items-center gap-2">
        {filters}
        <Button
          type="button"
          variant="ghost"
          size="icon-lg"
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

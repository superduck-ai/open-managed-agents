import { BookOpen, Plus } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button, ButtonLink } from '@/shared/ui/button';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';

export function AgentListHeader({
  title,
  description,
  createLabel,
  compact,
  onCreate,
}: {
  title: string;
  description: string;
  createLabel?: string;
  compact: boolean;
  onCreate: () => void;
}) {
  const { msg } = useI18n();
  return (
    <ResourcePageHeader
      title={title}
      description={description}
      compact={compact}
      actions={
        createLabel ? (
          <>
            <Button type="button" onClick={onCreate}>
              <Plus className="size-4" aria-hidden />
              {createLabel}
            </Button>
            {compact ? (
              <ButtonLink
                href="https://oma.mintlifysite.com/"
                target="_blank"
                rel="noreferrer"
                variant="secondary"
                size="icon"
                aria-label={msg('nav.documentation', 'Documentation')}
              >
                <BookOpen aria-hidden />
              </ButtonLink>
            ) : null}
          </>
        ) : null
      }
    />
  );
}

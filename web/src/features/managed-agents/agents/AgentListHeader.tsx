import { BookOpen, Plus } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button, ButtonLink } from '@/shared/ui/button';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';

export function AgentListHeader({
  title,
  description,
  createLabel,
  onCreate,
}: {
  title: string;
  description: string;
  createLabel?: string;
  onCreate: () => void;
}) {
  const { msg } = useI18n();
  return (
    <ResourcePageHeader
      title={title}
      description={description}
      actions={
        createLabel ? (
          <>
            <Button type="button" size="lg" onClick={onCreate}>
              <Plus className="size-4" aria-hidden />
              {createLabel}
            </Button>

            <ButtonLink
              href="https://oma.mintlifysite.com/"
              target="_blank"
              rel="noreferrer"
              variant="outline"
              size="icon-lg"
              aria-label={msg('nav.documentation', 'Documentation')}
            >
              <BookOpen aria-hidden />
            </ButtonLink>
          </>
        ) : null
      }
    />
  );
}

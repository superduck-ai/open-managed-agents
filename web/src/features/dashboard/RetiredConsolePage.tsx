import { useI18n } from '../../shared/i18n';
import { ButtonLink } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '../../shared/ui/empty';

export function RetiredConsolePage({ organizationSettings = false }: { organizationSettings?: boolean }) {
  const { msg } = useI18n();
  return (
    <Empty>
      <EmptyHeader>
        <EmptyTitle>{msg('console.retired.title', 'This page has been retired')}</EmptyTitle>
        <EmptyDescription>
          {msg(
            'console.retired.description',
            'This page did not provide a connected service. Use the available console features to continue.',
          )}
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <ButtonLink href={organizationSettings ? '/settings/organization' : '/dashboard'}>
          {organizationSettings
            ? msg('account.organizationSettings', 'Organization settings')
            : msg('nav.backToApp', 'Back to app')}
        </ButtonLink>
      </EmptyContent>
    </Empty>
  );
}

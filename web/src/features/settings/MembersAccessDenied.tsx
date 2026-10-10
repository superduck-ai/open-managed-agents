import { ShieldAlert } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';

export function MembersAccessDenied({ description }: { description: string }) {
  const { msg } = useI18n();

  return (
    <Empty className="min-h-[320px]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ShieldAlert aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{msg('members.accessDeniedTitle', 'Access denied')}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

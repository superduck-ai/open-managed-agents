import { useI18n } from '../../shared/i18n';
import { AccessDenied } from '../../shared/ui/access-denied';

export function MembersAccessDenied({ description }: { description: string }) {
  const { msg } = useI18n();

  return <AccessDenied title={msg('members.accessDeniedTitle', 'Access denied')} description={description} />;
}

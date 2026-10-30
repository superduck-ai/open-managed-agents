import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useOrganizations } from '../../shared/organizations/context';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { useScopeConfirmation } from '../organizations/useScopeConfirmation';

export function EnterOrganizationButton({ organizationUuid }: { organizationUuid: string }) {
  const organizations = useOrganizations();
  const navigate = useNavigate();
  const { msg } = useI18n();
  const confirmation = useScopeConfirmation();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const enter = async () => {
    setPending(true);
    setFailed(false);
    try {
      if (!(await organizations?.enterOrganization?.(organizationUuid))) throw new Error('Organization unavailable');
      await navigate({ to: '/workbench' });
    } catch {
      setFailed(true);
    } finally {
      setPending(false);
    }
  };
  return (
    <div className="space-y-2">
      <Button
        disabled={pending || !organizations?.enterOrganization}
        onClick={() => confirmation.request(() => void enter())}
      >
        {msg('platformScope.enter', 'Enter organization console')}
      </Button>
      {failed && (
        <p role="alert" className="text-sm text-destructive">
          {msg('platformScope.failed', 'Organization access failed. Please retry.')}
        </p>
      )}
      {confirmation.dialog}
    </div>
  );
}

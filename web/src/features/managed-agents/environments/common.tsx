import { Building2, UserRound } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import type { EnvironmentApiResponse } from '../types';
import { objectRecord } from '../utils';
import { environmentScopeLabel } from '../resources/environment-model';

export function EnvironmentScope({ scope }: { scope: string }) {
  const { msg } = useI18n();
  const Icon = scope === 'account' ? UserRound : Building2;
  return (
    <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-xs text-foreground/80">
      <Icon className="size-3" strokeWidth={1.5} aria-hidden />
      {environmentScopeLabel(scope, msg)}
    </span>
  );
}

export function EnvironmentHosting({ entity }: { entity: EnvironmentApiResponse }) {
  const { msg } = useI18n();
  const selfHosted = objectRecord(entity.config).type === 'self_hosted';
  return (
    <span className="inline-flex items-center gap-2">
      {selfHosted
        ? msg('managedAgents.environments.selfHosted', 'Self-hosted')
        : msg('managedAgents.environments.cloud', 'Cloud')}
    </span>
  );
}

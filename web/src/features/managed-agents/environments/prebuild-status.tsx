import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { Button } from '../../../shared/ui/button';
import { Dialog, DialogTrigger } from '../../../shared/ui/dialog';
import type { EnvironmentApiResponse, EnvironmentEditValues } from '../types';
import { environmentEditValues } from '../resources/model';
import { buildSpecKey, useEnvironmentPrebuild } from './prebuild';
import { PrebuildLabel } from './prebuild-display';
import { EnvironmentPrebuildDialog } from './prebuild-dialog';

export function EnvironmentPrebuildStatus({
  entity,
  workspaceId,
  values,
  readOnly,
}: {
  entity?: EnvironmentApiResponse;
  workspaceId: string;
  values: EnvironmentEditValues;
  readOnly?: boolean;
}) {
  const { msg } = useI18n();
  const savedSpecKey = buildSpecKey(entity && environmentEditValues(entity));
  const draftSpecKey = buildSpecKey(values);
  const query = useEnvironmentPrebuild(workspaceId, entity?.id, savedSpecKey);
  const [open, setOpen] = useState(false);
  const prebuild = query.data;
  const changed = draftSpecKey !== savedSpecKey;
  const mutation = useMutation({
    mutationFn: (action: 'start' | 'cancel') =>
      action === 'start'
        ? anthropicBetaApi.environments.prebuild.start(entity!.id, workspaceId)
        : anthropicBetaApi.environments.prebuild.cancel(entity!.id, workspaceId, {
            job_id: prebuild!.jobId,
          }),
    onSettled: async () => {
      await query.refetch();
    },
  });
  if (!draftSpecKey && !savedSpecKey) return null;
  if (changed || !entity)
    return <span className="text-xs text-muted-foreground">{msg('environmentPrebuild.unsaved', 'Unsaved')}</span>;
  if (query.isPending)
    return (
      <span role="status" className="text-xs text-muted-foreground">
        {msg('common.loading', 'Loading...')}
      </span>
    );
  if (query.isError)
    return (
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="text-muted-foreground"
        onClick={() => void query.refetch()}
      >
        {msg('environmentPrebuild.statusUnavailable', 'Retry status')}
      </Button>
    );
  if (!prebuild || (prebuild.state === 'idle' && !prebuild.canStart)) return null;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!mutation.isPending) mutation.reset();
      }}
    >
      <DialogTrigger render={<Button type="button" variant="ghost" size="xs" className="text-muted-foreground" />}>
        <span aria-live="polite">
          <PrebuildLabel state={prebuild.state} />
        </span>
      </DialogTrigger>
      {open ? (
        <EnvironmentPrebuildDialog
          environmentId={entity.id}
          workspaceId={workspaceId}
          prebuild={prebuild}
          readOnly={readOnly}
          busy={mutation.isPending}
          error={mutation.isError}
          onAction={(action) => mutation.mutate(action)}
        />
      ) : null}
    </Dialog>
  );
}

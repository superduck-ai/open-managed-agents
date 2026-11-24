import { useState } from 'react';

import { useFormatters, useI18n } from '@/shared/i18n';
import { CopyButton } from '@/shared/ui/copy-button';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/shared/ui/sheet';

import type { BusinessIdentity } from './api';
import { IdentityStatus } from './IdentityTable';

export function IdentityDetailSheet({ identity, onClose }: { identity: BusinessIdentity; onClose: () => void }) {
  const { msg } = useI18n();
  const format = useFormatters();
  const [open, setOpen] = useState(true);
  return (
    <Sheet open={open} onOpenChange={setOpen} onOpenChangeComplete={(nextOpen) => !nextOpen && onClose()}>
      <SheetContent className="gap-0 sm:max-w-[480px]">
        <SheetHeader className="border-b-[0.5px] p-5 pr-12">
          <SheetTitle>{msg('managedAgents.identities.details', '身份详情')}</SheetTitle>
        </SheetHeader>
        <div className="subtle-scrollbar min-h-0 overflow-y-auto">
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 p-5">
            <dt className="text-muted-foreground">{msg('managedAgents.identities.displayName', '展示名')}</dt>
            <dd className="break-all">{identity.display_name}</dd>
            <dt className="text-muted-foreground">{msg('managedAgents.identities.identityId', '身份 ID')}</dt>
            <dd className="flex min-w-0 items-center gap-1">
              <code className="break-all text-xs">{identity.id}</code>
              <CopyButton value={identity.id} label={msg('managedAgents.identities.copyId', '复制身份 ID')} />
            </dd>
            <dt className="text-muted-foreground">{msg('managedAgents.identities.subject', '外部用户 ID')}</dt>
            <dd className="break-all">{identity.external_subject_id}</dd>
            <dt className="text-muted-foreground">{msg('managedAgents.identities.namespace', '命名空间')}</dt>
            <dd className="break-all">{identity.namespace || '—'}</dd>
            <dt className="text-muted-foreground">{msg('common.status', '状态')}</dt>
            <dd>
              <IdentityStatus enabled={identity.enabled} />
            </dd>
            <dt className="text-muted-foreground">{msg('managedAgents.identities.createdAt', '创建时间')}</dt>
            <dd>
              <time dateTime={identity.created_at}>
                {format.date(identity.created_at, { dateStyle: 'medium', timeStyle: 'short' })}
              </time>
            </dd>
          </dl>
        </div>
      </SheetContent>
    </Sheet>
  );
}

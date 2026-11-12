import { useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Bell, Mail } from 'lucide-react';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from '../../shared/ui/popover';
import { Dialog, DialogContent, DialogTitle, DialogDescription, DialogHeader } from '../../shared/ui/dialog';
import { listInvitations, type Invitation } from './api';
import { InvitationList } from './InvitationList';

export function InvitationNotifications() {
  const { account, status } = useAuth();
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<Invitation | null>(null);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [processing, setProcessing] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const details = useRef<HTMLDivElement>(null);
  const query = useQuery({
    queryKey: ['invitations', account?.uuid],
    queryFn: listInvitations,
    enabled: status === 'authenticated',
    retry: false,
    refetchInterval: 60000,
  });
  const count = query.data?.data.length;
  return (
    <>
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (next) void query.refetch();
        }}
      >
        <PopoverTrigger
          render={
            <Button
              ref={trigger}
              variant="ghost"
              size="icon-sm"
              className="relative"
              aria-label={
                count === undefined
                  ? msg('invitations.title', 'Organization invitations')
                  : msg('invitations.pendingCount', 'Invitations, {count} pending', { count })
              }
            />
          }
        >
          <Bell className="size-4" />
          {count !== undefined && count > 0 && (
            <span className="console-invitation-count" aria-hidden>
              {count > 99 ? '99+' : count}
            </span>
          )}
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)] p-3">
          <PopoverTitle className="mb-2">{msg('invitations.title', 'Organization invitations')}</PopoverTitle>
          {query.isLoading && (
            <p role="status" className="py-4 text-center text-xs text-muted-foreground">
              {msg('invitations.loading', 'Loading invitations…')}
            </p>
          )}
          {query.isError ? (
            <div role="alert" className="space-y-2 py-2 text-xs text-muted-foreground">
              <p>{msg('invitations.loadFailed', 'Invitations could not be loaded.')}</p>
              <Button variant="ghost" size="sm" onClick={() => void query.refetch()}>
                {msg('common.retry', 'Retry')}
              </Button>
            </div>
          ) : (
            query.data?.data.map((invitation) => (
              <Button
                key={invitation.id}
                variant="ghost"
                className="h-auto min-h-12 w-full justify-start gap-3 whitespace-normal text-left"
                onClick={() => {
                  setOpen(false);
                  setSelected(invitation);
                  setDetailsOpen(true);
                }}
              >
                <Mail className="size-4 shrink-0" />
                <span className="min-w-0">
                  <span className="block truncate">{invitation.organization_name}</span>
                  <span className="block text-xs font-normal text-muted-foreground">
                    {msg('invitations.joinOrganization', 'Invitation to join the organization')}
                  </span>
                </span>
              </Button>
            ))
          )}
          {!query.isLoading && !query.isError && count === 0 && (
            <p className="py-4 text-center text-xs text-muted-foreground">
              {msg('invitations.empty', 'No pending invitations.')}
            </p>
          )}
        </PopoverContent>
      </Popover>
      <Dialog
        open={detailsOpen}
        onOpenChange={(next) => {
          if (!processing) setDetailsOpen(next);
        }}
        onOpenChangeComplete={(next) => {
          if (!next) setSelected(null);
        }}
      >
        <DialogContent
          ref={details}
          initialFocus={details}
          closeButtonDisabled={processing}
          finalFocus={trigger}
          className="sm:max-w-md"
        >
          <DialogHeader>
            <DialogTitle>{msg('invitations.title', 'Organization invitations')}</DialogTitle>
            <DialogDescription>
              {msg(
                'invitations.stayInOrganization',
                'Accepting an invitation keeps you in the current organization. You can switch organizations later.',
              )}
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <InvitationList
              onProcessingChange={setProcessing}
              invitations={[selected]}
              loading={false}
              error={null}
              retry={() => void query.refetch()}
              onResolved={async () => {
                setDetailsOpen(false);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

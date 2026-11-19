import { useInfiniteQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useState } from 'react';

import { useFormatters, useI18n } from '@/shared/i18n';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import { CopyButton } from '@/shared/ui/copy-button';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '@/shared/ui/resource-table';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';

import { QuickViewButton } from '../components/QuickViewButton';
import { errorMessage } from '../utils';
import { IdentityChannelLogos } from './IdentityChannelLogos';
import { listIdentityAgents, type BusinessIdentity, type IdentityDirectoryRow, type IdentityPage } from './api';

export function IdentityStatus({ enabled, active }: { enabled: boolean; active?: boolean }) {
  const { msg } = useI18n();
  const label = !enabled
    ? msg('managedAgents.identities.disabled', '停用')
    : active
      ? msg('managedAgents.identities.active', '活跃')
      : msg('managedAgents.identities.enabled', '启用');
  return <Badge variant={enabled && active ? 'secondary' : 'outline'}>{label}</Badge>;
}

type IdentityRowActions = {
  onOpen: (identity: BusinessIdentity) => void;
};

export function IdentityTable({
  rows,
  loading,
  filtered,
  error,
  onRetry,
  orgUuid,
  workspaceId,
  agentOptions,
  ...actions
}: IdentityRowActions & {
  rows: IdentityDirectoryRow[];
  loading: boolean;
  filtered: boolean;
  error: unknown;
  onRetry: () => void;
  orgUuid: string;
  workspaceId: string;
  agentOptions: IdentityPage['agent_options'];
}) {
  const { msg } = useI18n();
  return (
    <Table aria-label={msg('nav.identities', '身份')} className="min-w-[740px]">
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead scope="col" className="w-9">
            <span className="sr-only">{msg('managedAgents.identities.agentDetails', '展开 Agent')}</span>
          </TableHead>
          <TableHead scope="col">{msg('managedAgents.identities.subject', '外部用户 ID')}</TableHead>
          <TableHead scope="col">{msg('managedAgents.identities.identityId', '身份 ID')}</TableHead>
          <TableHead scope="col">Agent</TableHead>
          <TableHead scope="col">{msg('managedAgents.identities.sessions', '会话')}</TableHead>
          <TableHead scope="col">{msg('managedAgents.identities.lastUsed', '最近活跃')}</TableHead>
          <TableHead scope="col">
            <span className="sr-only">{msg('managedAgents.identities.details', '身份详情')}</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading && <TableLoadingRow colSpan={7} label={msg('common.loading', '正在加载...')} />}
        {Boolean(error) && (
          <TableErrorRow
            colSpan={7}
            title={msg('managedAgents.identities.loadError', '身份暂时无法加载')}
            message={errorMessage(error)}
            retryLabel={msg('common.retry', '重试')}
            onRetry={onRetry}
          />
        )}
        {!loading && !error && rows.length === 0 && (
          <TableEmptyRow colSpan={7}>
            {filtered
              ? msg('managedAgents.identities.noMatches', '没有符合条件的身份')
              : msg('managedAgents.identities.empty', '还没有身份')}
          </TableEmptyRow>
        )}
        {rows.map((identity) => (
          <IdentityRow
            key={identity.id}
            identity={identity}
            orgUuid={orgUuid}
            workspaceId={workspaceId}
            agentOptions={agentOptions}
            {...actions}
          />
        ))}
      </TableBody>
    </Table>
  );
}

function IdentityRow({
  identity,
  orgUuid,
  workspaceId,
  agentOptions,
  onOpen,
}: IdentityRowActions & {
  identity: IdentityDirectoryRow;
  orgUuid: string;
  workspaceId: string;
  agentOptions: IdentityPage['agent_options'];
}) {
  const { msg } = useI18n();
  const format = useFormatters();
  const [expanded, setExpanded] = useState(false);
  const agents = useInfiniteQuery({
    queryKey: ['identity-agents', orgUuid, workspaceId, identity.id],
    enabled: expanded && identity.agents > 0,
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) => listIdentityAgents(orgUuid, workspaceId, identity.id, pageParam, signal),
    getNextPageParam: (page) => page.next_page,
  });
  const Chevron = expanded ? ChevronDown : ChevronRight;
  const panelId = `agents-${identity.id}`;

  return (
    <>
      <TableRow
        className="cursor-pointer"
        onClick={(event) => {
          const target = event.target;
          if (target instanceof Element && !target.closest('button, a') && event.currentTarget.contains(target)) {
            setExpanded((value) => !value);
          }
        }}
      >
        <TableCell>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-expanded={expanded}
            aria-controls={expanded ? panelId : undefined}
            aria-label={msg('managedAgents.identities.agentDetails', '展开 Agent')}
            onClick={() => setExpanded(!expanded)}
          >
            <Chevron className="size-4" aria-hidden />
          </Button>
        </TableCell>
        <TableCell>
          <div className="flex max-w-52 items-center gap-1">
            <code className="truncate text-xs" title={identity.external_subject_id}>
              {identity.external_subject_id}
            </code>
            <CopyButton
              value={identity.external_subject_id}
              label={msg('managedAgents.identities.copySubject', '复制外部用户 ID')}
            />
          </div>
        </TableCell>
        <TableCell>
          <div className="flex max-w-72 items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              className="-ml-2 min-w-0 justify-start"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
            >
              <span className="truncate" title={identity.display_name}>
                {identity.display_name}
              </span>
            </Button>
            <IdentityStatus enabled={identity.enabled} active={identity.active} />
          </div>
          <div className="flex items-center gap-1 text-muted-foreground">
            <code className="text-xs">{identity.id}</code>
            <CopyButton value={identity.id} label={msg('managedAgents.identities.copyId', '复制身份 ID')} />
          </div>
        </TableCell>
        <TableCell className="tabular-nums">{format.number(identity.agents)}</TableCell>
        <TableCell className="tabular-nums">{format.number(identity.sessions)}</TableCell>
        <TableCell className="text-muted-foreground">
          {identity.last_used_at ? (
            <time dateTime={identity.last_used_at}>
              {format.date(identity.last_used_at, { dateStyle: 'short', timeStyle: 'short' })}
            </time>
          ) : (
            '—'
          )}
        </TableCell>
        <TableCell className="text-right">
          <QuickViewButton name={identity.display_name || identity.id} onClick={() => onOpen(identity)} />
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow className="bg-muted/30 hover:bg-muted/30">
          <TableCell id={panelId} colSpan={7} className="whitespace-normal pl-12">
            {agents.isError && !agents.data ? (
              <Button size="sm" variant="outline" onClick={() => void agents.refetch()}>
                {msg('common.retry', '重试')}
              </Button>
            ) : identity.agents === 0 ? (
              <p className="py-2 text-muted-foreground">
                {msg('managedAgents.identities.noAgents', '暂无 Agent 使用记录')}
              </p>
            ) : agents.isPending ? (
              <p role="status" className="py-2 text-muted-foreground">
                {msg('common.loading', '正在加载...')}
              </p>
            ) : (
              <>
                <ul className="max-w-xl divide-y divide-border">
                  {agents.data?.pages
                    .flatMap((page) => page.data)
                    .map((agent) => (
                      <li key={agent.agent_id} className="flex items-center justify-between gap-4 py-2">
                        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                          <span className="min-w-0 truncate">
                            {agentOptions.find(({ id }) => id === agent.agent_id)?.name ?? agent.agent_id}
                          </span>
                          <IdentityChannelLogos channels={agent.channels} />
                        </div>
                        <span className="shrink-0 text-muted-foreground">
                          {msg('managedAgents.identities.sessions', '会话')} {format.number(agent.sessions)}
                        </span>
                      </li>
                    ))}
                </ul>
                {agents.hasNextPage && (
                  <Button
                    className="mt-2"
                    size="sm"
                    variant="outline"
                    disabled={agents.isFetching}
                    onClick={() => void agents.fetchNextPage()}
                  >
                    {agents.isFetchNextPageError
                      ? msg('common.retry', '重试')
                      : msg('managedAgents.identities.loadMoreAgents', '加载更多 Agent')}
                  </Button>
                )}
              </>
            )}
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

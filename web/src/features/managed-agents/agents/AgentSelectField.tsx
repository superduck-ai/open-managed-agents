import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Check, ChevronsUpDown } from 'lucide-react';
import { useEffect, useId, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '../../../shared/ui/command';
import { Label } from '../../../shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { dedupeAgentsById, retrieveAgent } from '../api';
import { type PageCursor } from '../types';
import { listAgentPickerPage } from './agent-picker-api';

function useAgentPicker(
  workspaceId: string,
  value: string,
  onChange: (id: string) => void,
  open: boolean,
  defaultFirst: boolean,
) {
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [search]);
  const query = useInfiniteQuery({
    queryKey: ['agent-picker', workspaceId, debouncedSearch],
    queryFn: ({ pageParam }) => listAgentPickerPage(workspaceId, debouncedSearch, pageParam),
    initialPageParam: null as PageCursor,
    getNextPageParam: (page, _pages, _last, cursors) => {
      const next = page.next_page;
      return next && !cursors.includes(next) ? next : undefined;
    },
    enabled: open || defaultFirst,
    retry: false,
  });
  const agents = dedupeAgentsById(query.data?.pages.flatMap((page) => page.data) ?? []);
  const selected = agents.find((agent) => agent.id === value);
  const selectedQuery = useQuery({
    queryKey: ['agent-picker-selected', workspaceId, value],
    queryFn: () => retrieveAgent(value, workspaceId),
    enabled: Boolean(value) && !selected,
    retry: false,
  });
  const firstId = agents[0]?.id;
  useEffect(() => {
    if (defaultFirst && !value && !debouncedSearch && firstId) onChange(firstId);
  }, [defaultFirst, value, debouncedSearch, firstId, onChange]);
  const searching = search.trim() !== debouncedSearch;
  const loadMore = () => {
    if (!searching && query.hasNextPage && !query.isFetching) void query.fetchNextPage({ cancelRefetch: false });
  };
  const selectedName = selected?.name || selectedQuery.data?.name || value;
  return { agents, selectedName, query, search, debouncedSearch, setSearch, searching, loadMore };
}

export function AgentSelectField({
  workspaceId,
  value,
  onChange,
  defaultFirst = false,
  manage = false,
}: {
  workspaceId: string;
  value: string;
  onChange: (id: string) => void;
  defaultFirst?: boolean;
  manage?: boolean;
}) {
  const { msg } = useI18n();
  const id = useId();
  const [open, setOpen] = useState(false);
  const { agents, selectedName, query, search, debouncedSearch, setSearch, searching, loadMore } = useAgentPicker(
    workspaceId,
    value,
    onChange,
    open,
    defaultFirst,
  );
  const label = selectedName || msg('managedAgents.deployments.selectAgent', 'Select an agent');
  return (
    <div className="min-w-0 space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <Label htmlFor={id}>{msg('managedAgents.common.agent', 'Agent')}</Label>
        {manage && (
          <a
            aria-label={msg('managedAgents.common.opensInNewTab', '{label} (opens in new tab)', {
              label: msg('managedAgents.agents.manage', 'Manage agents'),
            })}
            className="text-xs"
            href={`/workspaces/${workspaceId}/agents`}
            target="_blank"
            rel="noreferrer"
          >
            {msg('managedAgents.agents.manage', 'Manage agents')}
          </a>
        )}
      </div>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          id={id}
          render={
            <Button
              variant="outline"
              role="combobox"
              aria-expanded={open}
              className="h-10 w-full justify-between font-normal"
            />
          }
        >
          <span className="min-w-0 truncate">{label}</span>
          <ChevronsUpDown className="size-4 shrink-0" />
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-(--anchor-width) min-w-48 max-h-(--available-height) gap-0 overflow-hidden p-0"
        >
          <Command
            shouldFilter={false}
            className="min-h-0 [&_[data-slot=command-input-wrapper]:has(input:focus-visible)]:ring-0 [&_[data-slot=command-input-wrapper]:has(input:focus-visible)]:border-muted-foreground/40"
          >
            <CommandInput
              placeholder={msg('managedAgents.agentPicker.search', 'Search by name or agent ID...')}
              value={search}
              onValueChange={setSearch}
            />
            <CommandList
              key={debouncedSearch}
              className="min-h-0 max-h-72"
              onScroll={(event) => {
                const list = event.currentTarget;
                if (list.scrollHeight - list.scrollTop - list.clientHeight < 48) loadMore();
              }}
            >
              {!query.isPending && !searching && !query.isError && (
                <CommandEmpty>{msg('managedAgents.agentPicker.empty', 'No agents found.')}</CommandEmpty>
              )}
              {!searching &&
                agents.map((agent) => (
                  <CommandItem
                    key={agent.id}
                    value={agent.id}
                    aria-label={agent.name || agent.id}
                    onSelect={() => {
                      onChange(agent.id);
                      setOpen(false);
                    }}
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{agent.name || agent.id}</span>
                      <span className="block truncate text-xs text-muted-foreground">{agent.id}</span>
                    </span>
                    {agent.id === value && <Check className="size-4 shrink-0" />}
                  </CommandItem>
                ))}
              {(query.isFetching || searching) && (
                <p role="status" className="p-3 text-sm text-muted-foreground">
                  {msg('managedAgents.agents.loading', 'Loading agents...')}
                </p>
              )}
              {!searching && query.isError && (
                <div role="alert" className="p-2 text-sm">
                  <p>{msg('managedAgents.agentPicker.failed', 'Could not load agents.')}</p>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => (query.isFetchNextPageError ? loadMore() : void query.refetch())}
                  >
                    {msg('common.retry', 'Retry')}
                  </Button>
                </div>
              )}
              {!query.isError && !searching && query.hasNextPage && !query.isFetching && (
                <Button type="button" variant="ghost" className="w-full" onClick={loadMore}>
                  {msg('managedAgents.agentPicker.more', 'Load more')}
                </Button>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  );
}

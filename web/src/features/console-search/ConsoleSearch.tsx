import { useContext, useEffect, useRef, useState } from 'react';
import { ArrowUpRight, Search, FileText, Folder, ArrowRight, Building2, CornerDownLeft, Layers, X } from 'lucide-react';
import { AuthContext } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceSwitchPath } from '../../shared/workspaces/presentation';
import { useScopeConfirmation } from '../organizations/useScopeConfirmation';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogClose, DialogContent, DialogTitle, DialogDescription } from '../../shared/ui/dialog';
import { Command, CommandInput, CommandList, CommandItem, CommandGroup } from '../../shared/ui/command';
import { documentationSearchRecords, searchMatches, type SearchRecord } from './search-data';
import { useSearchData } from './use-search-data';
import { PlatformAdminContext } from '../platform-admin/context';
import './console-search.css';

export function ConsoleSearch({
  currentPath,
  onNavigate,
}: {
  currentPath: string;
  onNavigate?: (href: string) => void | Promise<void>;
}) {
  const { msg } = useI18n();
  const platform = useContext(PlatformAdminContext);
  const searchTitle = platform
    ? msg('search.platform', 'Search super admin…')
    : msg('search.console', 'Search console…');
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const auth = useContext(AuthContext);
  const { orgUuid, activeWorkspaceId } = useWorkspace();
  const scope = JSON.stringify([auth?.account?.uuid, platform?.bootstrap.account.uuid, orgUuid, activeWorkspaceId]);
  const [history, setHistory] = useState<{ scope: string; records: SearchRecord[] }>({ scope, records: [] });
  if (history.scope !== scope) setHistory({ scope, records: [] });
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setOpen((value) => !value);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  return (
    <>
      <Button
        ref={trigger}
        variant="outline"
        className="console-search-trigger"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        <Search className="size-4" />
        <span>{searchTitle}</span>
        <kbd className="ml-auto hidden text-xs sm:inline">{navigator.platform.includes('Mac') ? '⌘' : 'Ctrl'} K</kbd>
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          finalFocus={trigger}
          showCloseButton={false}
          className="console-search-dialog flex flex-col gap-0 overflow-hidden p-0"
        >
          <DialogTitle className="sr-only">{searchTitle}</DialogTitle>
          <DialogDescription className="sr-only">
            {platform
              ? msg('search.platformHelp', 'Search administration pages, organizations and documentation.')
              : msg('search.help', 'Search pages, workspaces, documentation and resources.')}
          </DialogDescription>
          <SearchContents
            searchTitle={searchTitle}
            recent={history.records}
            onRecent={(records) => setHistory({ scope, records })}
            currentPath={currentPath}
            query={query}
            onQuery={setQuery}
            onNavigate={onNavigate}
            onClose={() => setOpen(false)}
          />
        </DialogContent>
      </Dialog>
    </>
  );
}

function SearchContents({
  searchTitle,
  recent,
  onRecent,
  currentPath,
  query,
  onQuery,
  onNavigate,
  onClose,
}: {
  searchTitle: string;
  recent: SearchRecord[];
  onRecent: (records: SearchRecord[]) => void;
  currentPath: string;
  query: string;
  onQuery: (query: string) => void;
  onNavigate?: (href: string) => void | Promise<void>;
  onClose: () => void;
}) {
  const { msg, locale } = useI18n();
  const { selectWorkspace } = useWorkspace();
  const confirmation = useScopeConfirmation();
  const [docsLimit, setDocsLimit] = useState(5);
  const [keyboard, setKeyboard] = useState(false);
  const pointer = useRef({ x: 0, y: 0 });
  const resultList = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      if (resultList.current) resultList.current.scrollTop = 0;
    });
    return () => cancelAnimationFrame(frame);
  }, [query]);
  const { resources, debounced, scopeName, pages, workspaceRecords, resourceRecords, resourceLabel } =
    useSearchData(query);
  const docs = documentationSearchRecords(locale).filter((record) => searchMatches(record, query));
  const navigate = (href: string) => {
    if (onNavigate) void onNavigate(href);
    else window.location.assign(href);
  };
  const choose = (record: SearchRecord) => {
    if (record.kind === 'workspace' && record.workspaceId) {
      const id = record.workspaceId;
      confirmation.request(() => {
        selectWorkspace(id);
        navigate(workspaceSwitchPath(currentPath, id));
        onClose();
      });
      return;
    }
    onRecent([record, ...recent.filter((item) => item.id !== record.id)].slice(0, 5));
    onClose();
    if (record.kind === 'document') window.open(record.href, '_blank', 'noopener,noreferrer');
    else navigate(record.href);
  };
  const groups = searchGroups(
    query,
    { pages, docs: docs.slice(0, docsLimit), workspaceRecords, resourceRecords, recent, resourceLabel },
    msg,
  );
  const loading = Boolean(query.trim()) && (query.trim() !== debounced || resources.isFetching);
  return (
    <>
      <Command
        shouldFilter={false}
        loop={false}
        disablePointerSelection={keyboard}
        onKeyDownCapture={(event) => {
          if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) setKeyboard(true);
        }}
        onPointerMoveCapture={(event) => {
          if (pointer.current.x !== event.clientX || pointer.current.y !== event.clientY) setKeyboard(false);
          pointer.current = { x: event.clientX, y: event.clientY };
        }}
      >
        <div className="console-search-header">
          <CommandInput
            variant="palette"
            autoFocus
            value={query}
            onValueChange={(value) => {
              onQuery(value);
              setDocsLimit(5);
            }}
            placeholder={searchTitle}
          />
          <DialogClose render={<Button variant="ghost" size="icon-sm" className="mr-4 shrink-0" />}>
            <X className="size-4" />
            <span className="sr-only">{msg('common.close', 'Close')}</span>
          </DialogClose>
        </div>
        <CommandList ref={resultList} className="console-search-results" aria-busy={loading}>
          {groups
            .filter((group) => group.records.length)
            .map((group) => (
              <CommandGroup key={group.label} heading={group.label}>
                {group.records.map((record) => (
                  <SearchResult key={record.id} record={record} onSelect={() => choose(record)} />
                ))}
              </CommandGroup>
            ))}
          {query.trim() && docs.length > docsLimit && (
            <CommandItem value="more-docs" onSelect={() => setDocsLimit((value) => value + 10)}>
              {msg('search.moreDocs', 'Load more documentation')}
            </CommandItem>
          )}
          {query.trim() && resources.hasNextPage && (
            <CommandItem
              value="more-resources"
              disabled={resources.isFetchingNextPage}
              onSelect={() => void resources.fetchNextPage()}
            >
              {msg('search.moreResources', 'Load more results')}
            </CommandItem>
          )}
          {loading && (
            <p role="status" className="px-3 py-3 text-xs text-muted-foreground">
              {msg('search.loading', 'Searching…')}
            </p>
          )}
          {query.trim() && (resources.isError || resources.data?.pages.some((page) => page.errors.length)) && (
            <div role="alert" className="px-3 py-2 text-xs text-muted-foreground">
              {msg('search.resourceError', 'Some results could not be loaded.')}
              <Button variant="link" size="sm" onClick={() => void resources.refetch()}>
                {msg('common.retry', 'Retry')}
              </Button>
            </div>
          )}
          {!loading && !groups.some((group) => group.records.length) && (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {msg('search.empty', 'No matching results.')}
            </p>
          )}
        </CommandList>
        <div className="console-search-footer">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd>
            {msg('search.select', 'Select')}
          </span>
          <span>
            <kbd>↵</kbd>
            {msg('common.open', 'Open')}
          </span>
          <span>
            <kbd>Esc</kbd>
            {msg('common.close', 'Close')}
          </span>
          <span className="console-search-scope" title={scopeName}>
            <Layers className="size-3.5 shrink-0" />
            <span className="truncate">{scopeName}</span>
          </span>
        </div>
      </Command>
      {confirmation.dialog}
    </>
  );
}

function SearchResult({ record, onSelect }: { record: SearchRecord; onSelect: () => void }) {
  const icons = { page: ArrowRight, workspace: Building2, document: FileText, resource: Folder };
  const Icon = record.icon || icons[record.kind];
  const ActionIcon = record.kind === 'document' ? ArrowUpRight : CornerDownLeft;
  return (
    <CommandItem
      value={`${record.kind}:${record.id}`}
      onSelect={onSelect}
      className="console-search-result h-11 min-h-11 gap-3"
      title={`${record.name} ${record.context}`}
      onPointerDown={(event) => event.preventDefault()}
    >
      <Icon className="size-4" />
      <span className="flex min-w-0 flex-1 items-baseline gap-3 overflow-hidden whitespace-nowrap">
        <span className="max-w-[60%] shrink-0 truncate">{record.name}</span>
        <span className="min-w-0 truncate text-xs text-muted-foreground">{record.context}</span>
      </span>
      <ActionIcon className="console-search-result-action size-3.5" />
    </CommandItem>
  );
}

function searchGroups(
  query: string,
  records: {
    pages: SearchRecord[];
    docs: SearchRecord[];
    workspaceRecords: SearchRecord[];
    resourceLabel: string;
    resourceRecords: SearchRecord[];
    recent: SearchRecord[];
  },
  msg: ReturnType<typeof useI18n>['msg'],
) {
  const { pages, docs, workspaceRecords, resourceRecords, recent } = records;
  if (query.trim())
    return [
      { label: msg('search.pages', 'Pages'), records: pages.filter((record) => searchMatches(record, query)) },
      {
        label: msg('nav.workspaces', 'Workspaces'),
        records: workspaceRecords.filter((record) => searchMatches(record, query)),
      },
      { label: msg('nav.documentation', 'Documentation'), records: docs },
      { label: records.resourceLabel, records: resourceRecords },
    ];
  const currentRecent = recent.filter(
    (record) => record.kind !== 'page' || pages.some((page) => page.id === record.id),
  );
  const withoutRecent = (items: SearchRecord[]) =>
    items.filter((record) => !currentRecent.some((entry) => entry.id === record.id));
  return [
    { label: msg('search.recent', 'Recent'), records: currentRecent },
    { label: msg('search.pages', 'Pages'), records: withoutRecent(pages) },
    { label: msg('search.popularDocs', 'Popular documentation'), records: withoutRecent(docs.slice(0, 5)) },
  ];
}

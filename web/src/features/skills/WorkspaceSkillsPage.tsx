import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { Plus, Search } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Tabs, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { ResourceListState } from '@/shared/ui/resource-list-state';
import { ResourceListPagination } from '@/shared/ui/resource-list-pagination';
import { productIcons } from '@/shared/icons/product-icons';
import { formatSkillSource, useDashboardWorkspaceScope, type ConsoleSkill } from '../dashboard/model';
import { SkillCard } from './SkillCard';
import { SkillActions } from './SkillActions';
import { SkillImportDialog } from './SkillImportDialog';
import { SkillDrawer, DeleteSkillDialog } from './SkillDetail';
import {
  skillConsoleRequest,
  downloadSkillPackage,
  workspaceSkillContentUrl,
  inspectWorkspaceSkill,
  importWorkspaceSkill,
  updateWorkspaceSkillIcon,
  skillErrorMessage,
  type SkillCatalog,
} from './api';

const pageSize = 12;
export function SkillsPage({
  initialCreateOpen = false,
  initialSkillId,
}: { initialCreateOpen?: boolean; initialSkillId?: string } = {}) {
  const { msg } = useI18n();
  const { workspaceId } = useDashboardWorkspaceScope();
  const queries = useQueryClient();
  const { selected, basePath } = useLocation({
    select: (location) => ({
      selected: typeof location.search.skill === 'string' ? location.search.skill : null,
      basePath: location.pathname.replace(/\/skills(?:\/.*)?$/, '/skills'),
    }),
  });
  const navigate = useNavigate();
  const setSelected = useCallback(
    (skillId: string | null, replace = false) => {
      void navigate({ to: basePath, search: (previous) => ({ ...previous, skill: skillId || undefined }), replace });
    },
    [navigate, basePath],
  );
  const [upload, setUpload] = useState<{ target?: ConsoleSkill } | null>(initialCreateOpen ? {} : null);
  const [deleteTarget, setDeleteTarget] = useState<Pick<ConsoleSkill, 'id' | 'display_title' | 'source'> | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [source, setSource] = useState('');
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState('');
  const [page, setPage] = useState(0);
  const scope = useRef(workspaceId);
  useEffect(() => {
    if (initialSkillId) setSelected(initialSkillId, true);
  }, [initialSkillId, setSelected]);
  useEffect(() => {
    if (scope.current !== workspaceId) {
      scope.current = workspaceId;
      setPage(0);
      setUpload(null);
      setDeleteTarget(null);
      setSelected(null);
    }
  }, [workspaceId, setSelected]);
  const query = useQuery({
    queryKey: ['skills', workspaceId, 'catalog', source, search, sort, page],
    queryFn: ({ signal }) =>
      skillConsoleRequest<SkillCatalog>(
        workspaceId,
        `?${new URLSearchParams({ source, query: search, sort, offset: String(page * pageSize), limit: String(pageSize) })}`,
        { signal },
      ),
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[1] === workspaceId ? previous : undefined),
    retry: false,
  });
  const changed = useCallback(
    async (id?: string) => {
      await Promise.all([
        queries.invalidateQueries({ queryKey: ['skills', workspaceId] }),
        ...(id
          ? [
              queries.invalidateQueries({ queryKey: ['skill', workspaceId, id] }),
              queries.invalidateQueries({ queryKey: ['skillVersions', workspaceId, id] }),
            ]
          : []),
      ]);
    },
    [queries, workspaceId],
  );
  const confirmDelete = async () => {
    if (!deleteTarget || deleting || deleteTarget.source !== 'custom') return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await skillConsoleRequest(workspaceId, `/${encodeURIComponent(deleteTarget.id)}`, { method: 'DELETE' });
      if (selected === deleteTarget.id) setSelected(null);
      setDeleteTarget(null);
      if (query.data?.data.length === 1 && page > 0) setPage(page - 1);
      await changed();
    } catch (cause) {
      setDeleteError(skillErrorMessage(cause, msg));
    } finally {
      setDeleting(false);
    }
  };
  const counts = query.data?.counts;
  const tabs = [
    { value: '', label: msg('skills.all', 'All skills'), count: counts?.all },
    { value: 'anthropic', label: msg('skills.builtin', 'System built-in'), count: counts?.builtin },
    { value: 'custom', label: msg('skills.custom', 'Custom'), count: counts?.custom },
  ];
  return (
    <section className="skills-page">
      <ResourcePageHeader
        contentGap="content"
        title={msg('skills.title', 'Skills')}
        actions={
          <Button size="lg" onClick={() => setUpload({})}>
            <Plus size={16} aria-hidden />
            {msg('skills.import.title', 'Import skills')}
          </Button>
        }
      />
      <Tabs
        value={source}
        onValueChange={(value) => {
          setSource(value);
          setPage(0);
        }}
      >
        <TabsList
          variant="line"
          className="mb-5 max-w-full items-stretch group-data-[orientation=horizontal]/tabs:h-auto"
        >
          {tabs.map((tab) => (
            <TabsTrigger
              key={tab.value}
              value={tab.value}
              className="h-auto min-h-8 min-w-0 whitespace-normal sm:min-w-fit sm:whitespace-nowrap"
            >
              {tab.label}
              {tab.count !== undefined && (
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{tab.count}</span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div className="relative min-w-0 flex-1 sm:max-w-80">
          <Search className="absolute top-2.5 left-3 size-4 text-muted-foreground" aria-hidden />
          <Input
            className="pl-9"
            aria-label={msg('skills.search', 'Search skills')}
            placeholder={msg('skills.searchPlaceholder', 'Search name, description or ID')}
            maxLength={256}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(0);
            }}
          />
        </div>
        <Select
          value={sort}
          onValueChange={(value) => {
            setSort(value || '');
            setPage(0);
          }}
        >
          <SelectTrigger aria-label={msg('skills.sort', 'Sort skills')}>
            <SelectValue>
              {sort === 'name'
                ? msg('skills.sortName', 'Name')
                : sort === 'updated'
                  ? msg('skills.sortUpdated', 'Recently updated')
                  : msg('skills.sortDefault', 'Default order')}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">{msg('skills.sortDefault', 'Default order')}</SelectItem>
            <SelectItem value="name">{msg('skills.sortName', 'Name')}</SelectItem>
            <SelectItem value="updated">{msg('skills.sortUpdated', 'Recently updated')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {query.isPending ? (
        <p role="status" className="py-8 text-sm text-muted-foreground">
          {msg('skills.loading', 'Loading skills...')}
        </p>
      ) : query.isError ? (
        <div role="alert" className="py-8 text-sm text-destructive">
          {skillErrorMessage(query.error, msg)}
          <Button variant="outline" onClick={() => void query.refetch()}>
            {msg('common.retry', 'Retry')}
          </Button>
        </div>
      ) : (
        <>
          {query.data.data.length ? (
            <div className="skill-grid" aria-busy={query.isFetching}>
              {query.data.data.map((skill) => (
                <SkillCard
                  key={skill.id}
                  name={skill.display_title || skill.id}
                  iconKey={skill.icon_key}
                  description={skill.description}
                  onOpen={() => setSelected(skill.id)}
                  metadata={<span className="skill-card-source">{formatSkillSource(skill.source, msg)}</span>}
                  actions={
                    skill.source === 'custom' ? (
                      <SkillActions
                        name={skill.display_title || skill.id}
                        iconKey={skill.icon_key}
                        onUpdate={() => setUpload({ target: skill })}
                        onDownload={() =>
                          downloadSkillPackage(
                            workspaceSkillContentUrl(workspaceId, skill.id, skill.latest_version),
                            workspaceId,
                          )
                        }
                        onIcon={async (key) => {
                          await updateWorkspaceSkillIcon(workspaceId, skill.id, key);
                          await changed(skill.id);
                        }}
                        onDelete={() => {
                          setDeleteError(null);
                          setDeleteTarget(skill);
                        }}
                      />
                    ) : undefined
                  }
                />
              ))}
            </div>
          ) : (
            <ResourceListState icon={productIcons.skills} title={msg('skills.noMatches', 'No matching skills')} />
          )}
          {query.data.total_count > pageSize && (
            <ResourceListPagination
              currentPage={page + 1}
              totalPages={Math.max(1, Math.ceil(query.data.total_count / pageSize))}
              canPrevious={page > 0 && !query.isFetching}
              canNext={(page + 1) * pageSize < query.data.total_count && !query.isFetching}
              onPrevious={() => setPage(page - 1)}
              onNext={() => setPage(page + 1)}
            />
          )}
        </>
      )}
      <SkillDrawer
        skillId={selected}
        workspaceId={workspaceId}
        onClose={() => setSelected(null)}
        onDeleteSkill={(skill) => {
          setDeleteError(null);
          setDeleteTarget(skill);
        }}
        onVersionChanged={changed}
      />
      {upload && (
        <SkillImportDialog
          title={upload.target ? msg('skills.update', 'Update skill') : undefined}
          submitLabel={upload.target ? msg('skills.update.submit', 'Update') : undefined}
          multiple={!upload.target}
          initialIconKey={upload.target?.icon_key}
          onClose={() => setUpload(null)}
          onInspect={(entry) => inspectWorkspaceSkill(workspaceId, entry, upload.target)}
          onImport={(entry) => importWorkspaceSkill(workspaceId, entry, upload.target)}
          onCompleted={async (id) => {
            await changed(id);
            if (id) setSelected(id);
          }}
        />
      )}
      <DeleteSkillDialog
        skill={deleteTarget}
        isDeleting={deleting}
        error={deleteError}
        onOpenChange={(open) => {
          if (!open && !deleting) setDeleteTarget(null);
        }}
        onConfirm={() => void confirmDelete()}
      />
    </section>
  );
}

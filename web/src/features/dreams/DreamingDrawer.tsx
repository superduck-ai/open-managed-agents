import { useEffect, useMemo, useRef, useState } from 'react';
import { Archive, Ban, ChevronLeft, Plus, Sparkles } from 'lucide-react';

import { useAuth } from '../../shared/auth/context';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Field, FieldDescription, FieldLabel } from '../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Sheet, SheetContent, SheetFooter, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { Textarea } from '../../shared/ui/textarea';
import { listCreateAgentModels } from '../managed-agents/agents/create-dialog-api';
import { listDreamSessionOptions, listMemoryStoreOptions } from '../managed-agents/api';
import { type MemoryStoreApiResponse, type SessionApiResponse } from '../managed-agents/types';
import { errorMessage } from '../managed-agents/utils';
import {
  archiveDream,
  cancelDream,
  createDream,
  dreamErrorLabel,
  dreamInputMemoryStoreId,
  dreamModelId,
  dreamOutput,
  dreamSessionCount,
  dreamSessionIds,
  listDreams,
  retrieveDream,
  type Dream,
  type DreamError,
  type DreamUsage,
} from './api';
import {
  DREAM_ACTIVE_POLL_MS,
  dreamIsActive,
  mergeDreamList,
  shouldPollDreamDetail,
  shouldPollDreamList,
} from './poll';

const MAX_SELECTED_SESSIONS = 100;

type DrawerView = 'history' | 'create' | 'detail';

function sessionLabel(session: SessionApiResponse) {
  return session.title || session.id;
}

function dreamStatusClassName(status: Dream['status']) {
  if (status === 'completed') {
    return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
  }
  if (status === 'failed' || status === 'canceled') {
    return 'border-destructive/30 bg-destructive/10 text-destructive';
  }
  return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300';
}

function formatTimestamp(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false });
}

function storeLabel(dream: Dream, stores: MemoryStoreApiResponse[]) {
  const id = dreamInputMemoryStoreId(dream);
  return stores.find((store) => store.id === id)?.name || id;
}

function sessionLabelById(sessionId: string, sessions: SessionApiResponse[]) {
  const session = sessions.find((candidate) => candidate.id === sessionId);
  return session ? sessionLabel(session) : sessionId;
}

function DreamHistory({
  dreams,
  loading,
  error,
  hasMore,
  onCreate,
  onLoadMore,
  onSelect,
}: {
  dreams: Dream[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  onCreate: () => void;
  onLoadMore: () => void;
  onSelect: (dream: Dream) => void;
}) {
  return (
    <>
      <div className="flex items-center justify-between border-b border-border px-5 py-4">
        <p className="text-sm text-muted-foreground">{loading ? '正在加载…' : `共 ${dreams.length} 条记录`}</p>
        <Button size="sm" onClick={onCreate}>
          <Plus aria-hidden /> 发起新的
        </Button>
      </div>
      {error ? (
        <p className="mx-5 mt-4 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      ) : null}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {dreams.map((dream) => (
          <button
            key={dream.id}
            type="button"
            className="flex w-full cursor-pointer items-start justify-between gap-3 border-b border-border px-5 py-4 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
            aria-label={`查看 Dream ${dream.id} 详情`}
            onClick={() => onSelect(dream)}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="truncate font-mono text-sm font-medium">{dream.id}</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {dreamModelId(dream)} · {dreamSessionCount(dream)} 场会话 · {dreamInputMemoryStoreId(dream)}
                </p>
              </div>
              <span
                className={`shrink-0 rounded-md border px-2 py-1 text-xs font-medium ${dreamStatusClassName(dream.status)}`}
              >
                {dream.status}
              </span>
            </div>
          </button>
        ))}
        {!loading && !dreams.length && !error ? (
          <p className="px-5 py-10 text-center text-sm text-muted-foreground">还没有 Dream 记录。</p>
        ) : null}
        {hasMore ? (
          <div className="p-5">
            <Button className="w-full" variant="outline" disabled={loading} onClick={onLoadMore}>
              {loading ? '正在加载…' : '加载更多'}
            </Button>
          </div>
        ) : null}
      </div>
    </>
  );
}

const USAGE_ROWS: Array<{ key: keyof DreamUsage; label: string }> = [
  { key: 'input_tokens', label: '输入 tokens' },
  { key: 'output_tokens', label: '输出 tokens' },
  { key: 'cache_creation_input_tokens', label: '缓存写入' },
  { key: 'cache_read_input_tokens', label: '缓存读取' },
];

function DreamUsageSection({ usage }: { usage: DreamUsage | null | undefined }) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium">用量</h3>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-lg border border-border p-3 text-sm">
        {USAGE_ROWS.map(({ key, label }) => (
          <div key={key} className="flex items-center justify-between gap-3">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="font-mono">{(usage?.[key] ?? 0).toLocaleString('zh-CN')}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function DreamOutputSection({ dream, workspaceId }: { dream: Dream; workspaceId: string }) {
  const output = dreamOutput(dream);
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium">输出</h3>
      <div className="space-y-3 rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">
        {dream.status === 'pending' ? (
          <>
            <p>等待执行，暂无产物。</p>
            <p>
              后台 Dream Worker 会自动复用或创建默认环境，再克隆 Store、创建内部会话、挂载只读 transcript 并启动 Dream
              skill。
            </p>
          </>
        ) : output?.memory_store_id ? (
          <>
            <p>{dream.status === 'running' ? 'Dream 正在整理记忆。' : 'Dream 已结束，可检查输出 Store。'}</p>
            <p>
              输出 Store：
              <a
                className="font-mono text-primary underline"
                href={`/workspaces/${workspaceId}/memory-stores/${output.memory_store_id}`}
              >
                {output.memory_store_id}
              </a>
            </p>
            {dream.session_id ? (
              <p>
                巩固会话：
                <a
                  className="font-mono text-primary underline"
                  href={`/workspaces/${workspaceId}/sessions/${dream.session_id}`}
                >
                  {dream.session_id}
                </a>
              </p>
            ) : null}
            <p>可在会话详情检查输出 Store、dream skill 与 /mnt/transcripts/dream/*.jsonl 虚拟文件。</p>
          </>
        ) : (
          <p>暂无产物。</p>
        )}
      </div>
    </section>
  );
}

function DreamErrorSection({ error }: { error: DreamError }) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium text-destructive">错误</h3>
      <div
        role="alert"
        className="space-y-1 rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
      >
        <p className="font-medium">{dreamErrorLabel(error)}</p>
        <p className="font-mono text-xs">type: {error.type}</p>
        {error.message ? <p className="break-all whitespace-pre-wrap">message: {error.message}</p> : null}
      </div>
    </section>
  );
}

export function DreamDetails({
  dream,
  stores,
  sessions,
  workspaceId = 'default',
  loading,
  error,
  onBack,
  onArchive,
  onCancel,
}: {
  dream: Dream;
  stores: MemoryStoreApiResponse[];
  sessions: SessionApiResponse[];
  workspaceId?: string;
  loading: boolean;
  error: string | null;
  onBack: () => void;
  onArchive: () => Promise<void>;
  onCancel?: () => Promise<void>;
}) {
  const selectedSessions = dreamSessionIds(dream);
  const [archiving, setArchiving] = useState(false);
  const [canceling, setCanceling] = useState(false);
  const active = dreamIsActive(dream.status);

  const archive = async () => {
    setArchiving(true);
    try {
      await onArchive();
    } finally {
      setArchiving(false);
    }
  };

  const cancel = async () => {
    if (!onCancel) {
      return;
    }
    setCanceling(true);
    try {
      await onCancel();
    } finally {
      setCanceling(false);
    }
  };

  return (
    <>
      <SheetHeader className="border-b border-border px-5 py-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon-sm" aria-label="返回 Dreaming 列表" onClick={onBack}>
            <ChevronLeft aria-hidden />
          </Button>
          <SheetTitle className="truncate font-mono">{dream.id}</SheetTitle>
        </div>
      </SheetHeader>
      <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-5 py-5">
        {error ? (
          <p className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <span
          className={`inline-flex rounded-md border px-2 py-1 text-xs font-medium ${dreamStatusClassName(dream.status)}`}
        >
          {dream.status}
        </span>
        <section>
          <h3 className="mb-2 text-sm font-medium">基本信息</h3>
          <dl className="space-y-1 rounded-lg border border-border p-3 text-sm">
            <div className="grid grid-cols-[auto_1fr] gap-x-3">
              <dt className="text-muted-foreground">ID</dt>
              <dd className="break-all font-mono">{dream.id}</dd>
            </div>
            <div className="grid grid-cols-[auto_1fr] gap-x-3">
              <dt className="text-muted-foreground">模型</dt>
              <dd>{dreamModelId(dream)}</dd>
            </div>
            <div className="grid grid-cols-[auto_1fr] gap-x-3">
              <dt className="text-muted-foreground">创建时间</dt>
              <dd>{formatTimestamp(dream.created_at)}</dd>
            </div>
            <div className="grid grid-cols-[auto_1fr] gap-x-3">
              <dt className="text-muted-foreground">更新时间</dt>
              <dd>{formatTimestamp(dream.updated_at)}</dd>
            </div>
            {dream.ended_at ? (
              <div className="grid grid-cols-[auto_1fr] gap-x-3">
                <dt className="text-muted-foreground">结束时间</dt>
                <dd>{formatTimestamp(dream.ended_at)}</dd>
              </div>
            ) : null}
          </dl>
        </section>
        {dream.error ? <DreamErrorSection error={dream.error} /> : null}
        <section>
          <h3 className="mb-2 text-sm font-medium">输入</h3>
          <div className="space-y-3 rounded-lg border border-border p-3 text-sm">
            <div>
              <p className="text-muted-foreground">记忆存储</p>
              <p className="mt-1 break-all">{storeLabel(dream, stores)}</p>
            </div>
            <div>
              <p className="text-muted-foreground">重点回顾的 Session（{selectedSessions.length}）</p>
              {selectedSessions.length ? (
                <ul className="mt-1 list-disc space-y-1 pl-5">
                  {selectedSessions.map((sessionId) => (
                    <li key={sessionId} className="break-all">
                      {sessionLabelById(sessionId, sessions)}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="mt-1">未选择；本次 Dream 的会话输入为空。</p>
              )}
            </div>
            {dream.instructions ? (
              <div>
                <p className="text-muted-foreground">自定义指令</p>
                <p className="mt-1 whitespace-pre-wrap">{dream.instructions}</p>
              </div>
            ) : null}
          </div>
        </section>
        <DreamOutputSection dream={dream} workspaceId={workspaceId} />
        {dream.status !== 'pending' ? <DreamUsageSection usage={dream.usage} /> : null}
        {loading ? <p className="text-sm text-muted-foreground">正在刷新任务详情…</p> : null}
      </div>
      <SheetFooter className="border-t border-border px-5 py-4 sm:flex-row sm:justify-end">
        <Button variant="outline" onClick={onBack}>
          返回
        </Button>
        {active ? (
          <Button variant="outline" disabled={canceling} onClick={() => void cancel()}>
            <Ban aria-hidden />
            {canceling ? '正在取消…' : '取消'}
          </Button>
        ) : null}
        {!active && !dream.archived_at ? (
          <Button variant="outline" disabled={archiving} onClick={() => void archive()}>
            <Archive aria-hidden />
            {archiving ? '正在归档…' : '归档'}
          </Button>
        ) : null}
      </SheetFooter>
    </>
  );
}

function DreamCreateForm({
  stores,
  sessions,
  models,
  loading,
  onBack,
  onSubmit,
}: {
  stores: MemoryStoreApiResponse[];
  sessions: SessionApiResponse[];
  models: string[];
  loading: boolean;
  onBack: () => void;
  onSubmit: (input: {
    memoryStoreId: string;
    model: string;
    sessionIds: string[];
    instructions: string;
  }) => Promise<void>;
}) {
  const [memoryStoreId, setMemoryStoreId] = useState('');
  const [model, setModel] = useState('');
  const [sessionIds, setSessionIds] = useState<string[]>([]);
  const [instructions, setInstructions] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const selectedStore = stores.find((store) => store.id === memoryStoreId);
  const canSubmit = Boolean(memoryStoreId && model && sessionIds.length);

  const toggleSession = (sessionId: string) => {
    setSessionIds((current) => {
      if (current.includes(sessionId)) {
        return current.filter((id) => id !== sessionId);
      }
      return current.length >= MAX_SELECTED_SESSIONS ? current : [...current, sessionId];
    });
  };

  const submit = async () => {
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit({ memoryStoreId, model, sessionIds, instructions });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <SheetHeader className="border-b border-border px-5 py-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon-sm" aria-label="返回 Dreaming 列表" onClick={onBack}>
            ‹
          </Button>
          <SheetTitle>发起新的 Dreaming</SheetTitle>
        </div>
      </SheetHeader>
      <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-5">
        <Field>
          <FieldLabel>记忆存储</FieldLabel>
          <FieldDescription>
            每场 Dream 只整理一个记忆存储；结果写入克隆出的新 Store，原 Store 不会被修改。
          </FieldDescription>
          <Select value={memoryStoreId} onValueChange={(value) => setMemoryStoreId(value ?? '')}>
            <SelectTrigger className="h-10 w-full">
              <SelectValue placeholder="请选择记忆存储">{selectedStore?.name || selectedStore?.id}</SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {stores
                .filter((store) => !store.archived_at)
                .map((store) => (
                  <SelectItem key={store.id} value={store.id}>
                    {store.name || store.id}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel>模型</FieldLabel>
          <Select value={model} onValueChange={(value) => setModel(value ?? '')}>
            <SelectTrigger className="h-10 w-full">
              <SelectValue placeholder="选择模型" />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {models.map((modelId) => (
                <SelectItem key={modelId} value={modelId}>
                  {modelId}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <div className="flex items-center justify-between gap-3">
            <FieldLabel>重点回顾的 Session</FieldLabel>
            <span className="text-sm text-muted-foreground">
              {sessionIds.length} / {MAX_SELECTED_SESSIONS}
            </span>
          </div>
          <FieldDescription>选择 1～100 场会话；Dream 会从这些记录里挖掘需要巩固的内容。</FieldDescription>
          <div className="overflow-hidden rounded-lg border border-border">
            {sessions.map((session) => {
              const checked = sessionIds.includes(session.id);
              const disabled = !checked && sessionIds.length >= MAX_SELECTED_SESSIONS;
              return (
                <label
                  key={session.id}
                  className="flex cursor-pointer items-center gap-3 border-b border-border px-3 py-3 last:border-b-0 has-[[data-slot=checkbox]:disabled]:cursor-not-allowed has-[[data-slot=checkbox]:disabled]:opacity-50"
                >
                  <Checkbox checked={checked} disabled={disabled} onCheckedChange={() => toggleSession(session.id)} />
                  <span className="min-w-0 flex-1 truncate text-sm">{sessionLabel(session)}</span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">{session.id}</span>
                </label>
              );
            })}
            {!loading && !sessions.length ? (
              <p className="px-3 py-4 text-sm text-muted-foreground">没有可选择的 Session。</p>
            ) : null}
          </div>
        </Field>
        <Field>
          <FieldLabel htmlFor="dream-instructions">自定义指令（可选）</FieldLabel>
          <Textarea
            id="dream-instructions"
            value={instructions}
            maxLength={4096}
            onChange={(event) => setInstructions(event.target.value)}
            placeholder="如：重点关注 Python 项目约定"
            className="min-h-28 resize-y"
          />
          <FieldDescription>{instructions.length} / 4096</FieldDescription>
        </Field>
      </div>
      <SheetFooter className="border-t border-border px-5 py-4 sm:flex-row sm:justify-end">
        <Button variant="outline" onClick={onBack}>
          取消
        </Button>
        <Button disabled={!canSubmit || submitting || loading} onClick={() => void submit()}>
          {submitting ? '正在发起…' : '整理记忆'}
        </Button>
      </SheetFooter>
    </>
  );
}

export function DreamingDrawer({ workspaceId }: { workspaceId: string }) {
  const { csrfToken } = useAuth();
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<DrawerView>('history');
  const [dreams, setDreams] = useState<Dream[]>([]);
  const [stores, setStores] = useState<MemoryStoreApiResponse[]>([]);
  const [sessions, setSessions] = useState<SessionApiResponse[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [selectedDream, setSelectedDream] = useState<Dream | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [nextDreamPage, setNextDreamPage] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const detailRequest = useRef(0);

  const inputsReady = useMemo(() => Boolean(stores.length && models.length), [models.length, stores.length]);

  useEffect(() => {
    if (!open) {
      return;
    }
    let active = true;
    setLoading(true);
    setError(null);
    void Promise.all([
      listDreams().catch((dreamLoadError: unknown) => {
        if (active) {
          setError(errorMessage(dreamLoadError));
        }
        return { data: [], next_page: null };
      }),
      listMemoryStoreOptions(workspaceId),
      listDreamSessionOptions(workspaceId),
      listCreateAgentModels(workspaceId),
    ])
      .then(([dreamPage, storePage, sessionPage, modelOptions]) => {
        if (!active) {
          return;
        }
        setDreams(dreamPage.data ?? []);
        setNextDreamPage(dreamPage.next_page ?? null);
        setStores(storePage.data ?? []);
        setSessions((sessionPage.data ?? []) as SessionApiResponse[]);
        setModels(modelOptions.map((option) => option.id));
      })
      .catch((loadError: unknown) => {
        if (active) {
          setError(errorMessage(loadError));
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [open, workspaceId]);

  useEffect(() => {
    const pollList = shouldPollDreamList(open, view, dreams);
    const pollDetail = shouldPollDreamDetail(open, view, selectedDream);
    if (!pollList && !pollDetail) {
      return;
    }
    let cancelled = false;
    const tick = async () => {
      try {
        if (pollDetail && selectedDream) {
          const detail = await retrieveDream(selectedDream.id);
          if (cancelled) {
            return;
          }
          setSelectedDream(detail);
          setDreams((current) => current.map((dream) => (dream.id === detail.id ? detail : dream)));
          return;
        }
        const page = await listDreams();
        if (cancelled) {
          return;
        }
        setDreams((current) => mergeDreamList(current, page.data ?? []));
      } catch (pollError: unknown) {
        if (!cancelled) {
          setError(errorMessage(pollError));
        }
      }
    };
    const timer = window.setInterval(() => {
      void tick();
    }, DREAM_ACTIVE_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [dreams, open, selectedDream, view, workspaceId]);

  const submit = async (input: {
    memoryStoreId: string;
    model: string;
    sessionIds: string[];
    instructions: string;
  }) => {
    try {
      const dream = await createDream(input, csrfToken);
      setDreams((current) => [dream, ...current]);
      setView('history');
    } catch (submitError) {
      setError(errorMessage(submitError));
      setView('history');
    }
  };

  const selectDream = (dream: Dream) => {
    const request = detailRequest.current + 1;
    detailRequest.current = request;
    setSelectedDream(dream);
    setView('detail');
    setDetailLoading(true);
    setError(null);
    void retrieveDream(dream.id)
      .then((detail) => {
        if (detailRequest.current === request) setSelectedDream(detail);
      })
      .catch((detailError: unknown) => {
        if (detailRequest.current === request) setError(errorMessage(detailError));
      })
      .finally(() => {
        if (detailRequest.current === request) setDetailLoading(false);
      });
  };

  const loadMoreDreams = () => {
    if (!nextDreamPage || loading) return;
    setLoading(true);
    setError(null);
    void listDreams(nextDreamPage)
      .then((page) => {
        setDreams((current) => [...current, ...(page.data ?? [])]);
        setNextDreamPage(page.next_page ?? null);
      })
      .catch((pageError: unknown) => setError(errorMessage(pageError)))
      .finally(() => setLoading(false));
  };

  const archiveSelectedDream = async () => {
    if (!selectedDream) return;
    try {
      await archiveDream(selectedDream.id, csrfToken);
      setDreams((current) => current.filter((dream) => dream.id !== selectedDream.id));
      setSelectedDream(null);
      setView('history');
    } catch (archiveError) {
      setError(errorMessage(archiveError));
    }
  };

  const cancelSelectedDream = async () => {
    if (!selectedDream) return;
    try {
      const canceled = await cancelDream(selectedDream.id, csrfToken);
      setDreams((current) => current.map((dream) => (dream.id === canceled.id ? canceled : dream)));
      setSelectedDream(canceled);
    } catch (cancelError) {
      setError(errorMessage(cancelError));
    }
  };

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <Button
        type="button"
        onClick={() => {
          setView('history');
          setOpen(true);
        }}
      >
        <Sparkles aria-hidden /> Dreaming
      </Button>
      <SheetContent side="right" className="gap-0 p-0 sm:!max-w-3xl">
        {view === 'history' ? (
          <>
            <SheetHeader className="border-b border-border px-5 py-4">
              <SheetTitle>Dreaming</SheetTitle>
            </SheetHeader>
            <DreamHistory
              dreams={dreams}
              loading={loading}
              error={error}
              hasMore={Boolean(nextDreamPage)}
              onCreate={() => {
                if (inputsReady || !loading) setView('create');
              }}
              onLoadMore={loadMoreDreams}
              onSelect={selectDream}
            />
          </>
        ) : view === 'create' ? (
          <DreamCreateForm
            stores={stores}
            sessions={sessions}
            models={models}
            loading={loading}
            onBack={() => setView('history')}
            onSubmit={submit}
          />
        ) : selectedDream ? (
          <DreamDetails
            dream={selectedDream}
            stores={stores}
            sessions={sessions}
            workspaceId={workspaceId}
            loading={detailLoading}
            error={error}
            onBack={() => setView('history')}
            onArchive={archiveSelectedDream}
            onCancel={cancelSelectedDream}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

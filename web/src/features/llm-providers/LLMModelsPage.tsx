import { useCallback, useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import { AlertCircle, Bot, Plus, ShieldAlert } from 'lucide-react';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { canManageLLMProviders } from '../../shared/permissions/llm-providers';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';
import { Skeleton } from '../../shared/ui/skeleton';
import { toast } from '../../shared/ui/sonner';
import {
  createLLMProvider,
  deleteLLMProvider,
  listLLMProviders,
  previewProviderModels,
  syncProviderModels,
  updateLLMProvider,
  type LLMProvider,
  type LLMProviderInput,
} from './api';
import {
  ALL_PROVIDERS_ID,
  catalogModels,
  providerInput,
  readableError,
  resolvedProviderSelection,
  type CatalogModel,
} from './catalog';
import { ModelCatalog } from './ModelCatalog';
import { ProviderDeleteDialog } from './ProviderDeleteDialog';
import { ProviderFormDialog } from './ProviderFormDialog';

export function LLMModelsPage() {
  const { account } = useAuth();
  const { orgUuid } = useWorkspace();

  if (!canManageLLMProviders(account, orgUuid)) {
    return <LLMModelsAccessDenied />;
  }

  return <LLMModelsAdminPage />;
}

function LLMModelsAccessDenied() {
  const { msg } = useI18n();

  return (
    <Empty data-testid="llm-models-access-denied" className="min-h-[320px] border border-dashed border-border bg-card">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ShieldAlert aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{msg('llmModels.accessDeniedTitle', 'Administrator access required')}</EmptyTitle>
        <EmptyDescription>
          {msg('llmModels.accessDeniedDescription', 'Only organization administrators can manage model configuration.')}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

function LLMModelsAdminPage() {
  const location = useLocation();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const { csrfToken } = useAuth();
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { orgUuid, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const workspaceId = routeWorkspaceId ?? activeWorkspaceId;
  const [editing, setEditing] = useState<LLMProvider | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [formError, setFormError] = useState('');
  const [deleting, setDeleting] = useState<LLMProvider | null>(null);
  const [query, setQuery] = useState('');
  const [selectedProviderId, setSelectedProviderId] = useState(ALL_PROVIDERS_ID);
  const [isSyncing, setIsSyncing] = useState(false);
  const providersKey = useMemo(
    () => ['console', 'llm-providers', orgUuid, workspaceId] as const,
    [orgUuid, workspaceId],
  );

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  const providersQuery = useQuery({
    queryKey: providersKey,
    queryFn: () => listLLMProviders(orgUuid ?? '', workspaceId),
    enabled: Boolean(orgUuid && workspaceId),
    retry: false,
  });
  const providers = providersQuery.data ?? [];
  const activeProviderId = resolvedProviderSelection(providers, selectedProviderId);
  const catalog = catalogModels(providers);

  const invalidateCatalog = async () => {
    await queryClient.invalidateQueries({ queryKey: providersKey });
  };

  const discoverModels = useCallback(
    async (baseUrl: string, apiKey: string) => {
      if (!orgUuid) {
        throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
      }
      const preview = await previewProviderModels(
        orgUuid,
        workspaceId,
        { base_url: baseUrl, api_key: apiKey },
        csrfToken,
      );
      return preview.model_ids ?? [];
    },
    [csrfToken, msg, orgUuid, workspaceId],
  );

  const syncLiveModels = async (targets: LLMProvider[]) => {
    if (!orgUuid) {
      throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
    }
    if (targets.length === 0) {
      return;
    }
    setIsSyncing(true);
    try {
      const results = await Promise.allSettled(
        targets.map((provider) => syncProviderModels(orgUuid, workspaceId, provider.id, csrfToken)),
      );
      const skippedModelIds = new Set<string>();
      for (const result of results) {
        if (result.status === 'fulfilled') {
          for (const modelId of result.value.skipped_model_ids ?? []) {
            skippedModelIds.add(modelId);
          }
        }
      }
      const failed = results.find((result) => result.status === 'rejected');
      if (failed && failed.status === 'rejected') {
        throw failed.reason;
      }
      if (skippedModelIds.size > 0) {
        toast.info(
          msg('llmModels.syncSkipped', 'Skipped {count} model IDs already configured by another provider.', {
            count: String(skippedModelIds.size),
          }),
          { id: 'llm-models-sync-skipped' },
        );
      }
    } finally {
      setIsSyncing(false);
    }
  };

  const saveMutation = useMutation({
    mutationFn: async (input: LLMProviderInput) => {
      if (!orgUuid) {
        throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
      }
      return editing
        ? updateLLMProvider(orgUuid, workspaceId, editing.id, input, csrfToken)
        : createLLMProvider(orgUuid, workspaceId, input, csrfToken);
    },
    onSuccess: async () => {
      closeForm();
      await invalidateCatalog();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (provider: LLMProvider) => deleteLLMProvider(orgUuid ?? '', workspaceId, provider.id, csrfToken),
    onSuccess: async () => {
      setDeleting(null);
      await invalidateCatalog();
    },
  });

  const updateModelsMutation = useMutation({
    mutationFn: async ({ provider, modelIds }: { provider: LLMProvider; modelIds: string[] }) => {
      if (!orgUuid) {
        throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
      }
      return updateLLMProvider(orgUuid, workspaceId, provider.id, providerInput(provider, modelIds), csrfToken);
    },
    onSuccess: async () => {
      setQuery('');
      await invalidateCatalog();
    },
    onError: (error) => {
      toast.error(readableError(error, msg), { id: 'llm-models-update' });
    },
  });

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
    setFormError('');
    saveMutation.reset();
  };

  const openCreate = () => {
    setEditing(null);
    setFormError('');
    setFormOpen(true);
  };

  const submitForm = async (input: LLMProviderInput) => {
    try {
      await saveMutation.mutateAsync(input);
    } catch (error) {
      setFormError(readableError(error, msg));
    }
  };

  const addModel = (provider: LLMProvider, modelId: string) => {
    updateModelsMutation.mutate({ provider, modelIds: [...provider.model_ids, modelId] });
  };

  const refreshCatalog = async () => {
    const targets =
      activeProviderId === ALL_PROVIDERS_ID
        ? providers
        : providers.filter((provider) => provider.id === activeProviderId);
    try {
      await syncLiveModels(targets);
    } catch (error) {
      toast.error(readableError(error, msg), { id: 'llm-models-sync' });
    }
    await invalidateCatalog();
  };

  return (
    <section className="w-full max-w-none" data-testid="llm-models-page">
      <div className="mb-5 flex flex-col items-start justify-between gap-3 sm:flex-row">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-[28px] font-semibold leading-tight tracking-normal text-foreground">
              {msg('llmModels.title', 'LLM models')}
            </h1>
            {catalog.length > 0 ? <Badge variant="secondary">{catalog.length}</Badge> : null}
          </div>
          <p className="mt-2 max-w-[760px] text-sm leading-5 text-muted-foreground">
            {msg('llmModels.description', 'Manage providers and models for this workspace.')}
          </p>
        </div>
        {providers.length > 0 ? (
          <Button type="button" size="lg" onClick={openCreate}>
            <Plus className="size-4" aria-hidden />
            {msg('llmModels.addProvider', 'Add provider')}
          </Button>
        ) : null}
      </div>

      <CatalogBody
        providers={providers}
        catalog={catalog}
        selectedProviderId={activeProviderId}
        query={query}
        isLoading={providersQuery.isLoading}
        providersError={providersQuery.error}
        isRefreshing={providersQuery.isFetching || isSyncing}
        isAddingModel={updateModelsMutation.isPending}
        onOpenCreate={openCreate}
        onQueryChange={setQuery}
        onRefresh={() => void refreshCatalog()}
        onSelectProvider={setSelectedProviderId}
        onEditProvider={(provider) => {
          setEditing(provider);
          setFormError('');
          setFormOpen(true);
        }}
        onDeleteProvider={setDeleting}
        onAddModel={addModel}
      />

      {formOpen ? (
        <ProviderFormDialog
          provider={editing}
          error={formError}
          isPending={saveMutation.isPending}
          onClose={closeForm}
          onSubmit={submitForm}
          onDiscoverModels={discoverModels}
        />
      ) : null}
      <ProviderDeleteDialog
        provider={deleting}
        error={deleteMutation.isError ? readableError(deleteMutation.error, msg) : ''}
        isPending={deleteMutation.isPending}
        onClose={() => {
          setDeleting(null);
          deleteMutation.reset();
        }}
        onConfirm={(provider) => deleteMutation.mutate(provider)}
      />
    </section>
  );
}

function CatalogBody({
  providers,
  catalog,
  selectedProviderId,
  query,
  isLoading,
  providersError,
  isRefreshing,
  isAddingModel,
  onOpenCreate,
  onQueryChange,
  onRefresh,
  onSelectProvider,
  onEditProvider,
  onDeleteProvider,
  onAddModel,
}: {
  providers: LLMProvider[];
  catalog: CatalogModel[];
  selectedProviderId: string;
  query: string;
  isLoading: boolean;
  providersError: unknown;
  isRefreshing: boolean;
  isAddingModel: boolean;
  onOpenCreate: () => void;
  onQueryChange: (query: string) => void;
  onRefresh: () => void;
  onSelectProvider: (providerId: string) => void;
  onEditProvider: (provider: LLMProvider) => void;
  onDeleteProvider: (provider: LLMProvider) => void;
  onAddModel: (provider: LLMProvider, modelId: string) => void;
}) {
  const { msg } = useI18n();
  if (providersError) {
    return (
      <Alert variant="destructive">
        <AlertCircle aria-hidden />
        <AlertDescription>{readableError(providersError, msg)}</AlertDescription>
      </Alert>
    );
  }
  if (isLoading) {
    return <CatalogSkeleton />;
  }
  if (providers.length === 0) {
    return (
      <Empty className="min-h-[320px] border border-dashed border-border bg-card">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <Bot aria-hidden />
          </EmptyMedia>
          <EmptyTitle>{msg('llmModels.catalogEmptyTitle', 'No providers yet')}</EmptyTitle>
        </EmptyHeader>
        <EmptyContent>
          <Button type="button" onClick={onOpenCreate}>
            {msg('llmModels.addProvider', 'Add provider')}
          </Button>
        </EmptyContent>
      </Empty>
    );
  }
  return (
    <ModelCatalog
      providers={providers}
      models={catalog}
      selectedProviderId={selectedProviderId}
      query={query}
      isRefreshing={isRefreshing}
      isAddingModel={isAddingModel}
      onQueryChange={onQueryChange}
      onRefresh={onRefresh}
      onSelectProvider={onSelectProvider}
      onEditProvider={onEditProvider}
      onDeleteProvider={onDeleteProvider}
      onAddModel={onAddModel}
    />
  );
}

function CatalogSkeleton() {
  return (
    <div className="grid gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
      <div className="space-y-2">
        <Skeleton className="h-14 w-full rounded-xl" />
        <Skeleton className="h-14 w-full rounded-xl" />
      </div>
      <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
        <div className="border-b border-border p-3">
          <Skeleton className="h-9 w-full" />
        </div>
        <Skeleton className="h-12 w-full rounded-none" />
        <Skeleton className="h-12 w-full rounded-none" />
        <Skeleton className="h-12 w-full rounded-none" />
      </div>
    </div>
  );
}

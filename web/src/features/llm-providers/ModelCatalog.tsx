import { Check, Copy, KeyRound, Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { copyText } from '@/shared/lib/clipboard';
import { cn } from '@/shared/lib/utils';
import { useI18n } from '../../shared/i18n';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '../../shared/ui/accordion';
import { Button } from '../../shared/ui/button';
import { Card } from '../../shared/ui/card';
import { InputGroup, InputGroupAddon, InputGroupInput } from '../../shared/ui/input-group';
import type { LLMProvider } from './api';
import {
  ALL_PROVIDERS_ID,
  addableModelId,
  filterCatalogModels,
  groupCatalogModels,
  modelsForProvider,
  providerHost,
  type CatalogGroup,
  type CatalogModel,
} from './catalog';

type ModelCatalogProps = {
  canManage: boolean;
  organizationEditable: boolean;
  providers: LLMProvider[];
  models: CatalogModel[];
  selectedProviderId: string;
  query: string;
  isRefreshing: boolean;
  isAddingModel: boolean;
  onQueryChange: (query: string) => void;
  onRefresh: () => void;
  onSelectProvider: (providerId: string) => void;
  onEditProvider: (provider: LLMProvider) => void;
  onDeleteProvider: (provider: LLMProvider) => void;
  onAddModel: (provider: LLMProvider, modelId: string) => void;
};

export function ModelCatalog({
  canManage,
  organizationEditable,
  providers,
  models,
  selectedProviderId,
  query,
  isRefreshing,
  isAddingModel,
  onQueryChange,
  onRefresh,
  onSelectProvider,
  onEditProvider,
  onDeleteProvider,
  onAddModel,
}: ModelCatalogProps) {
  const { msg } = useI18n();
  const [openProviders, setOpenProviders] = useState<string[]>([]);
  const selected = providers.find((provider) => provider.id === selectedProviderId);
  const visibleModels = modelsForProvider(filterCatalogModels(models, query), selectedProviderId);
  const canEditSelected = canManage && (organizationEditable || selected?.scope !== 'organization');
  const candidateId = addableModelId(query, modelsForProvider(models, selectedProviderId));

  return (
    <div className="grid grid-cols-[minmax(0,1fr)] items-start gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
      <ProviderRail
        providers={providers}
        models={models}
        selectedProviderId={selectedProviderId}
        onSelectProvider={onSelectProvider}
      />
      <Card className="min-w-0 gap-0 py-0">
        <div className="flex items-center gap-2 border-b-[0.5px] border-border p-3">
          <InputGroup className="flex-1 shadow-none">
            <InputGroupAddon>
              <Search aria-hidden />
            </InputGroupAddon>
            <InputGroupInput
              value={query}
              onChange={(event) => onQueryChange(event.target.value)}
              placeholder={msg('llmModels.searchPlaceholder', 'Search models')}
              aria-label={msg('llmModels.searchPlaceholder', 'Search models')}
            />
          </InputGroup>
          <Button
            type="button"
            size="icon"
            variant="outline"
            aria-label={msg('llmModels.refreshModels', 'Refresh model list')}
            disabled={isRefreshing}
            onClick={onRefresh}
          >
            <RefreshCw className={cn('size-4', isRefreshing && 'animate-spin')} aria-hidden />
          </Button>
        </div>

        <ProviderModelGroups
          organizationEditable={organizationEditable}
          groups={groupCatalogModels(
            visibleModels,
            selected ? [selected] : providers,
            Boolean(selected) || !query.trim(),
          )}
          providers={providers}
          query={query}
          openProviders={openProviders}
          onOpenChange={setOpenProviders}
          editableProviderId={canEditSelected ? selected?.id : undefined}
          onEditProvider={onEditProvider}
          onDeleteProvider={onDeleteProvider}
        />

        {canEditSelected ? (
          <AddModelHint
            candidateId={candidateId}
            query={query}
            provider={selected}
            isAdding={isAddingModel}
            onAdd={() => {
              if (selected && candidateId) onAddModel(selected, candidateId);
            }}
          />
        ) : null}
      </Card>
    </div>
  );
}

function ProviderRail({
  providers,
  models,
  selectedProviderId,
  onSelectProvider,
}: {
  providers: LLMProvider[];
  models: CatalogModel[];
  selectedProviderId: string;
  onSelectProvider: (providerId: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <nav
      className="subtle-scrollbar flex min-w-0 gap-1 overflow-x-auto p-1 lg:flex-col lg:overflow-visible"
      data-testid="llm-provider-rail"
      aria-label={msg('llmModels.allProviders', 'All providers')}
    >
      <ProviderRailButton
        label={msg('llmModels.allProviders', 'All providers')}
        count={models.length}
        selected={selectedProviderId === ALL_PROVIDERS_ID}
        onClick={() => onSelectProvider(ALL_PROVIDERS_ID)}
      />
      {providers.map((provider) => (
        <ProviderRailButton
          key={provider.id}
          label={provider.name}
          detail={providerHost(provider.base_url)}
          count={provider.models.length}
          selected={selectedProviderId === provider.id}
          onClick={() => onSelectProvider(provider.id)}
        />
      ))}
    </nav>
  );
}

function ProviderRailButton({
  label,
  detail,
  count,
  selected,
  onClick,
}: {
  label: string;
  detail?: string;
  count: number;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant={selected ? 'secondary' : 'ghost'}
      onClick={onClick}
      aria-label={label}
      aria-pressed={selected}
      className="h-auto min-w-[180px] max-w-60 flex-col items-start gap-0.5 rounded-lg px-3 py-2.5 text-left whitespace-normal lg:w-full lg:min-w-0 lg:max-w-none"
    >
      <span className="flex w-full items-center justify-between gap-2">
        <span className="line-clamp-2 min-w-0 flex-1 text-sm font-medium [overflow-wrap:anywhere]" title={label}>
          {label}
        </span>
        <span className="shrink-0 text-xs font-normal text-muted-foreground tabular-nums">{count}</span>
      </span>
      {detail ? (
        <span className="w-full truncate text-xs font-normal text-muted-foreground" title={detail}>
          {detail}
        </span>
      ) : null}
    </Button>
  );
}

function ProviderIdentity({
  provider,
  modelCount,
  organizationReadOnly,
}: {
  provider: LLMProvider;
  modelCount: number;
  organizationReadOnly: boolean;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex min-w-0 flex-1 items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2 break-words text-sm font-semibold text-foreground">
          <span className="line-clamp-2 min-w-0 max-w-full [overflow-wrap:anywhere]" title={provider.name}>
            {provider.name}
          </span>
          <code className="text-xs font-normal text-muted-foreground">{provider.slug}</code>
        </p>
        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs font-normal text-muted-foreground">
          <span className="min-w-0 max-w-full truncate" title={provider.base_url}>
            {provider.base_url}
          </span>
          <span className="flex shrink-0 items-center gap-1.5">
            <KeyRound className="size-3.5" aria-hidden />
            •••• {provider.api_key_last4}
          </span>
        </div>
        {provider.scope === 'organization' && organizationReadOnly ? (
          <p className="mt-2 text-xs text-muted-foreground">
            {msg(
              'llmModels.organizationReadOnly',
              'Organization models are read-only here. Manage them in organization LLM configuration.',
            )}
          </p>
        ) : null}
      </div>
      <span className="shrink-0 text-xs font-normal text-muted-foreground tabular-nums">{modelCount}</span>
    </div>
  );
}

function ProviderModelGroups({
  organizationEditable,
  groups,
  providers,
  query,
  openProviders,
  onOpenChange,
  editableProviderId,
  onEditProvider,
  onDeleteProvider,
}: {
  organizationEditable: boolean;
  groups: CatalogGroup[];
  providers: LLMProvider[];
  query: string;
  openProviders: string[];
  onOpenChange: (ids: string[]) => void;
  editableProviderId?: string;
  onEditProvider: (provider: LLMProvider) => void;
  onDeleteProvider: (provider: LLMProvider) => void;
}) {
  const { msg } = useI18n();
  const groupValues = groups.map((group) => group.providerId);
  const searching = Boolean(query.trim());
  if (groups.length === 0) {
    return (
      <p className="px-4 py-5 text-sm text-muted-foreground">
        {msg('llmModels.noSearchResults', 'No models match this search.')}
      </p>
    );
  }

  return (
    <Accordion
      multiple
      value={searching ? groupValues : openProviders}
      onValueChange={(next) => {
        if (!searching)
          onOpenChange([...new Set([...openProviders.filter((id) => !groupValues.includes(id)), ...next])]);
      }}
    >
      {groups.map((group) => {
        const provider = providers.find((item) => item.id === group.providerId);
        return provider ? (
          <ProviderModelGroup
            key={group.providerId}
            group={group}
            provider={provider}
            query={query}
            organizationEditable={organizationEditable}
            canManage={provider.id === editableProviderId}
            onEdit={() => onEditProvider(provider)}
            onDelete={() => onDeleteProvider(provider)}
          />
        ) : null;
      })}
    </Accordion>
  );
}

function ProviderModelGroup({
  group,
  provider,
  query,
  organizationEditable,
  canManage,
  onEdit,
  onDelete,
}: {
  group: CatalogGroup;
  provider: LLMProvider;
  query: string;
  organizationEditable: boolean;
  canManage: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { msg } = useI18n();
  return (
    <AccordionItem value={group.providerId} data-testid={`llm-provider-group-${group.providerId}`}>
      <div className="mx-1 flex items-center rounded-lg">
        <div className="min-w-0 flex-1">
          <AccordionTrigger
            aria-label={msg('llmModels.providerGroup', '{provider} models', { provider: group.providerName })}
            className="min-h-16 min-w-0 gap-3 rounded-lg px-3 py-3 hover:bg-accent hover:no-underline"
          >
            <ProviderIdentity
              provider={provider}
              modelCount={provider.models.length}
              organizationReadOnly={!organizationEditable}
            />
          </AccordionTrigger>
        </div>
        {canManage ? (
          <div className="flex shrink-0 items-center gap-1 pr-2">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={msg('common.edit', 'Edit')}
              onClick={onEdit}
            >
              <Pencil aria-hidden />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={msg('common.delete', 'Delete')}
              onClick={onDelete}
            >
              <Trash2 aria-hidden />
            </Button>
          </div>
        ) : null}
      </div>
      <AccordionContent className="pb-0">
        <ModelRows
          models={group.models}
          emptyMessage={
            query.trim()
              ? msg('llmModels.noSearchResults', 'No models match this search.')
              : msg('llmModels.emptyModels', 'No models configured for this provider.')
          }
        />
      </AccordionContent>
    </AccordionItem>
  );
}

function ModelRows({ models, emptyMessage }: { models: CatalogModel[]; emptyMessage: string }) {
  if (models.length === 0) {
    return (
      <p className="px-4 py-5 text-sm text-muted-foreground" data-testid="llm-model-empty">
        {emptyMessage}
      </p>
    );
  }
  return (
    <ul className="divide-y-[0.5px] divide-border">
      {models.map((model) => (
        <ModelRow key={`${model.providerId}:${model.id}`} model={model} />
      ))}
    </ul>
  );
}

function ModelRow({ model }: { model: CatalogModel }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  const label = copied ? msg('common.copied', 'Copied') : msg('common.copyId', 'Copy ID');

  const copyId = async () => {
    await copyText(model.qualifiedId);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <li
      className="flex min-h-11 items-center justify-between gap-3 px-4 py-2 hover:bg-accent"
      data-testid="llm-model-row"
    >
      <div className="min-w-0 flex-1">
        {model.name ? <p className="break-words text-sm">{model.name}</p> : null}
        <p
          className={
            model.name
              ? 'break-all font-mono text-xs text-muted-foreground'
              : 'break-all font-mono text-sm text-foreground'
          }
        >
          {model.qualifiedId}
        </p>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="shrink-0 text-muted-foreground"
        aria-label={label}
        onClick={() => void copyId()}
      >
        {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
      </Button>
    </li>
  );
}

function AddModelHint({
  candidateId,
  query,
  provider,
  isAdding,
  onAdd,
}: {
  candidateId: string;
  query: string;
  provider?: LLMProvider;
  isAdding: boolean;
  onAdd: () => void;
}) {
  const { msg } = useI18n();
  if (!provider || !query.trim() || !candidateId) return null;
  const targetLabel = msg('llmModels.addModelToProvider', 'Add {modelId} to {provider}', {
    modelId: candidateId,
    provider: provider.name,
  });
  return (
    <div className="border-t-[0.5px] border-border p-3">
      <Button
        type="button"
        variant="outline"
        className="h-auto min-h-8 w-full justify-start text-left whitespace-normal"
        aria-label={targetLabel}
        title={targetLabel}
        disabled={isAdding}
        onClick={onAdd}
      >
        <Plus aria-hidden />
        {msg('llmModels.addModel', 'Add model')}
      </Button>
    </div>
  );
}

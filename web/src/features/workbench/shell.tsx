import { useI18n } from '../../shared/i18n';
import { Box, Check, CirclePlus, Loader2, Lock, Trash2, X } from 'lucide-react';
import { Dispatch, ReactNode, SetStateAction } from 'react';
import clsx from 'clsx';
import type { AuthAccount } from '../../shared/auth/api';
import { Button } from '@/shared/ui/button';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/shared/ui/command';
import { Label } from '@/shared/ui/label';
import { PopoverContent, PopoverHeader, PopoverTitle } from '@/shared/ui/popover';
import { Switch } from '@/shared/ui/switch';
import { WorkbenchPromptSummary } from './api';
import { formatPromptSummaryDate, promptSummaryDisplayTitle } from './model';

export function WorkbenchShell({ children }: { children: ReactNode }) {
  return <section className="workbench-shell">{children}</section>;
}

export function WorkbenchAccessUnavailable({ productName }: { productName: string }) {
  return (
    <WorkbenchShell>
      <div className="flex h-full items-center justify-center px-6">
        <section className="w-full max-w-[520px] rounded-lg border-[0.5px] border-border bg-card p-6 shadow-sm">
          <div className="mb-4 flex size-10 items-center justify-center rounded-md border-[0.5px] border-border bg-muted/50 text-muted-foreground">
            <Lock className="size-5" aria-hidden />
          </div>
          <h1 className="text-xl font-semibold text-foreground">Workbench access unavailable</h1>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            {productName} doesn't include access to the Workbench.
          </p>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            Your organization has disabled Workbench access for your user role.
          </p>
          <Button
            type="button"
            className="mt-5 rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background hover:opacity-90"
            onClick={() => {
              window.location.assign('/dashboard');
            }}
          >
            Go to Dashboard
          </Button>
        </section>
      </div>
    </WorkbenchShell>
  );
}

export function PromptPicker({
  prompts,
  selectedPromptId,
  search,
  setSearch,
  onlyMine,
  setOnlyMine,
  selectedPromptTitle,
  account,
  onClose,
  onCreate,
  isCreating,
  onSelect,
  onRequestDelete,
}: {
  prompts: WorkbenchPromptSummary[];
  selectedPromptId?: string;
  search: string;
  setSearch: Dispatch<SetStateAction<string>>;
  onlyMine: boolean;
  setOnlyMine: Dispatch<SetStateAction<boolean>>;
  selectedPromptTitle: string;
  account: AuthAccount | null;
  onClose: () => void;
  onCreate: () => void | Promise<void>;
  isCreating: boolean;
  onSelect: (prompt: WorkbenchPromptSummary) => void | Promise<void>;
  onRequestDelete: (prompt: WorkbenchPromptSummary) => void;
}) {
  const { locale, msg } = useI18n();
  const selected = prompts.find((prompt) => prompt.id === selectedPromptId);
  return (
    <PopoverContent
      aria-label={msg('workbench.prompts', 'Prompts')}
      align="start"
      sideOffset={10}
      className="w-[24rem] max-w-[calc(100vw-2.5rem)] gap-0 overflow-hidden p-0"
    >
      <Command
        defaultValue={
          selected
            ? `${promptSummaryDisplayTitle(selected, selectedPromptId, selectedPromptTitle)} ${selected.id}`
            : undefined
        }
        shouldFilter={false}
        label={msg('workbench.searchPrompts', 'Search prompts')}
        className="bg-transparent"
      >
        <PopoverHeader className="flex min-w-0 flex-row items-center justify-between border-b-[0.5px] px-4 py-3">
          <PopoverTitle className="truncate text-sm font-semibold text-foreground">
            {msg('workbench.prompts', 'Prompts')}
          </PopoverTitle>
          <Button
            type="button"
            aria-label={msg('workbench.closePrompts', 'Close prompts')}
            variant="ghost"
            size="icon-sm"
            className="shrink-0"
            onClick={onClose}
          >
            <X className="size-4" aria-hidden />
          </Button>
        </PopoverHeader>

        <CommandInput
          aria-label={msg('workbench.searchPrompts', 'Search prompts')}
          placeholder={msg('workbench.searchPrompts', 'Search prompts')}
          value={search}
          onValueChange={setSearch}
        />

        <div className="border-b-[0.5px] px-3 py-3">
          <Label className="flex w-full items-center justify-between gap-3 rounded-md px-2 py-2 text-sm text-foreground">
            <span>{msg('workbench.onlyMyPrompts', 'Only show my prompts')}</span>
            <Switch checked={onlyMine} onCheckedChange={setOnlyMine} />
          </Label>
        </div>

        <CommandList className="max-h-96 p-1">
          {prompts.length ? (
            <CommandGroup className="p-1">
              {prompts.map((item) => {
                const selected = item.id === selectedPromptId;
                const title = promptSummaryDisplayTitle(item, selectedPromptId, selectedPromptTitle);
                return (
                  <CommandItem
                    key={item.id}
                    role="option"
                    aria-current={selected ? 'true' : undefined}
                    aria-selected={selected}
                    value={`${title} ${item.id}`}
                    keywords={[
                      item.id,
                      item.creator?.tagged_id ?? '',
                      item.creator?.full_name ?? '',
                      item.creator?.email_address ?? '',
                    ]}
                    className={clsx(
                      'h-auto items-start gap-3 rounded-md px-2.5 py-2',
                      selected && 'bg-accent text-accent-foreground',
                    )}
                    onSelect={() => void onSelect(item)}
                  >
                    <div className="grid min-w-0 flex-1 gap-0.5">
                      <span className="truncate text-sm font-medium">{title}</span>
                      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Box className="size-3.5 shrink-0" aria-hidden />
                        <span className="min-w-0 truncate">{formatPromptSummaryDate(item, account, locale, msg)}</span>
                        <Lock className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                      </span>
                    </div>
                    {selected ? <Check className="size-4 shrink-0 text-primary" aria-hidden /> : null}
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      className="shrink-0 text-muted-foreground hover:text-foreground"
                      aria-label={msg('workbench.deletePrompt', 'Delete prompt')}
                      title={msg('workbench.deletePrompt', 'Delete prompt')}
                      onPointerDown={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                      }}
                      onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        onRequestDelete(item);
                      }}
                    >
                      <Trash2 className="size-4" aria-hidden />
                    </Button>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          ) : (
            <CommandEmpty>{msg('workbench.promptsEmpty', 'No prompts found')}</CommandEmpty>
          )}
        </CommandList>

        <div className="border-t-[0.5px] p-2">
          <Button
            type="button"
            variant="outline"
            className="w-full justify-start"
            disabled={isCreating}
            onClick={() => void onCreate()}
          >
            {isCreating ? (
              <Loader2 className="size-4 animate-spin" aria-hidden />
            ) : (
              <CirclePlus className="size-4" aria-hidden />
            )}
            {isCreating
              ? msg('workbench.creatingPrompt', 'Creating Prompt')
              : msg('workbench.createPrompt', 'Create New Prompt')}
          </Button>
        </div>
      </Command>
    </PopoverContent>
  );
}

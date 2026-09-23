import { useState, type FormEvent } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { ManagedWarningAlert } from '../components/common';
import { formatUsdCents, parseBudgetUsdInput, type SessionBudgetState } from '../resources/budget';

export function SessionBudgetBanner({
  state,
  busy,
  onChangeBudget,
}: {
  state: SessionBudgetState;
  busy: boolean;
  onChangeBudget: (usd: string | null) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [editOpen, setEditOpen] = useState(false);
  const [editValue, setEditValue] = useState('');
  const editParsed = parseBudgetUsdInput(editValue);
  const editInvalid = !editParsed.ok || editParsed.cents === null;

  const submitEdit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (editInvalid || busy) {
      return;
    }
    setEditOpen(false);
    void onChangeBudget(editValue.trim());
  };

  return (
    <>
      {state.reached ? (
        <ManagedWarningAlert className="mx-4 mb-4 max-w-2xl @min-[640px]:mx-6 @min-[1024px]:mx-8">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span>
              {msg(
                'managedAgents.budget.reached',
                'Budget reached: {{spent}} of {{budget}} spent. Only settlement events are accepted; change or remove the budget to resume.',
              )
                .replace('{{spent}}', formatUsdCents(state.spentCents))
                .replace('{{budget}}', formatUsdCents(state.budgetCents))}
            </span>
            <span className="flex items-center gap-2">
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-7 bg-background"
                disabled={busy}
                onClick={() => {
                  setEditValue((state.budgetCents / 100).toFixed(2));
                  setEditOpen(true);
                }}
              >
                {msg('managedAgents.budget.change', 'Change budget')}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="h-7 bg-background"
                disabled={busy}
                onClick={() => void onChangeBudget(null)}
              >
                {msg('managedAgents.budget.remove', 'Remove budget')}
              </Button>
            </span>
          </div>
        </ManagedWarningAlert>
      ) : (
        <p className="mx-4 mb-2 text-xs text-muted-foreground @min-[640px]:mx-6 @min-[1024px]:mx-8">
          {msg('managedAgents.budget.summary', 'Budget {{budget}} · spent {{spent}}')
            .replace('{{budget}}', formatUsdCents(state.budgetCents))
            .replace('{{spent}}', formatUsdCents(state.spentCents))}
        </p>
      )}
      <Dialog open={editOpen} onOpenChange={(open) => !open && setEditOpen(false)}>
        <DialogContent className="sm:max-w-[420px]">
          <form onSubmit={submitEdit}>
            <DialogHeader>
              <DialogTitle>{msg('managedAgents.budget.changeTitle', 'Change budget')}</DialogTitle>
              <DialogDescription>
                {msg(
                  'managedAgents.budget.changeHelp',
                  'Set a new USD cap. Saving re-arms enforcement and resumes the session.',
                )}
              </DialogDescription>
            </DialogHeader>
            <div className="mt-4">
              <Label htmlFor="session-budget-edit-input" className="text-sm font-medium">
                {msg('managedAgents.budget.title', 'Budget')} (USD)
              </Label>
              <Input
                id="session-budget-edit-input"
                inputMode="decimal"
                value={editValue}
                aria-invalid={editInvalid || undefined}
                className="mt-2 h-10 bg-secondary"
                onChange={(event) => setEditValue(event.target.value)}
                autoFocus
              />
              {editInvalid ? (
                <p className="mt-1.5 text-xs text-destructive">
                  {msg('managedAgents.budget.invalid', 'Enter a positive dollar amount, e.g. 25 or 12.50.')}
                </p>
              ) : null}
            </div>
            <DialogFooter className="mt-5">
              <Button type="button" variant="outline" onClick={() => setEditOpen(false)}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={editInvalid || busy}>
                {msg('common.save', 'Save')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

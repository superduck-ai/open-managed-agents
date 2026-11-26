import { useId } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import type { RuntimeBudgetMode } from '../../../shared/workspaces/runtime';
import { type ManagedEntityFormValues } from '../types';
import { parseBudgetUsdInput } from './budget';

export function BudgetField({
  values,
  onChange,
  mode = 'usd',
}: {
  values: ManagedEntityFormValues;
  onChange: (patch: Partial<ManagedEntityFormValues>) => void;
  mode?: RuntimeBudgetMode;
}) {
  const { msg } = useI18n();
  const id = `budget-field-${useId()}`;
  if (mode === 'none') {
    return null;
  }
  const isCredits = mode === 'credits';
  const invalid = !parseBudgetUsdInput(values.budgetUsd).ok;
  return (
    <div>
      <Label htmlFor={id} className="text-sm font-medium leading-5 text-foreground">
        {isCredits
          ? msg('managedAgents.budget.creditsTitle', 'Budget (Credits)')
          : msg('managedAgents.budget.title', 'Budget')}{' '}
        <span className="font-normal text-muted-foreground">
          {msg('managedAgents.common.optionalParen', '(optional)')}
        </span>
      </Label>
      <Input
        id={id}
        type="text"
        inputMode="decimal"
        value={values.budgetUsd}
        placeholder={
          isCredits
            ? msg('managedAgents.budget.creditsPlaceholder', 'e.g. 100')
            : msg('managedAgents.budget.placeholder', 'e.g. 25')
        }
        aria-invalid={invalid || undefined}
        className="mt-2 h-10"
        onChange={(event) => onChange({ budgetUsd: event.target.value, budgetMode: mode })}
      />
      <p className={`mt-1.5 text-xs ${invalid ? 'text-destructive' : 'text-muted-foreground'}`}>
        {invalid
          ? isCredits
            ? msg('managedAgents.budget.creditsInvalid', 'Enter a positive credit limit, e.g. 100 or 25.50.')
            : msg('managedAgents.budget.invalid', 'Enter a positive dollar amount, e.g. 25 or 12.50.')
          : isCredits
            ? msg(
                'managedAgents.budget.creditsHelp',
                'Credits cap per session. The session pauses when credit usage reaches it.',
              )
            : msg('managedAgents.budget.help', 'USD cap per session. The session pauses when spending reaches it.')}
      </p>
    </div>
  );
}

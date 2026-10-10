import { useId } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { type ManagedEntityFormValues } from '../types';
import { parseBudgetUsdInput } from './budget';

export function BudgetField({
  values,
  onChange,
}: {
  values: ManagedEntityFormValues;
  onChange: (patch: Partial<ManagedEntityFormValues>) => void;
}) {
  const { msg } = useI18n();
  const id = `budget-field-${useId()}`;
  const invalid = !parseBudgetUsdInput(values.budgetUsd).ok;
  return (
    <div>
      <Label htmlFor={id} className="text-sm font-medium leading-5 text-foreground">
        {msg('managedAgents.budget.title', 'Budget')}{' '}
        <span className="font-normal text-muted-foreground">
          {msg('managedAgents.common.optionalParen', '(optional)')}
        </span>
      </Label>
      <Input
        id={id}
        type="text"
        inputMode="decimal"
        value={values.budgetUsd}
        placeholder={msg('managedAgents.budget.placeholder', 'e.g. 25')}
        aria-invalid={invalid || undefined}
        className="managed-resource-field mt-2 h-10 border-border bg-secondary px-3 text-sm text-foreground placeholder:text-muted-foreground focus:border-ring focus:shadow-none focus-visible:shadow-none focus-visible:ring-0"
        onChange={(event) => onChange({ budgetUsd: event.target.value, budgetChanged: true })}
      />
      <p className={`mt-1.5 text-xs ${invalid ? 'text-destructive' : 'text-muted-foreground'}`}>
        {invalid
          ? msg('managedAgents.budget.invalid', 'Enter a positive dollar amount, e.g. 25 or 12.50.')
          : msg('managedAgents.budget.help', 'USD cap per session. The session pauses when spending reaches it.')}
      </p>
    </div>
  );
}

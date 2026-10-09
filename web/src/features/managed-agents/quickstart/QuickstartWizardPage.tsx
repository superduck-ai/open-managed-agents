import { Check } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { ManagedErrorAlert } from '../components/common';
import { errorMessage } from '../utils';
import type { AgentModelOption } from '../agents/create-dialog-model';
import { quickstartCopy } from './copy';
import { QuickstartStart, QuickstartAgent, QuickstartEnvironment } from './QuickstartConfiguration';
import { useQuickstartWizard } from './useQuickstartWizard';
import { quickstartBinding } from './model';
import { QuickstartIntegration } from './QuickstartIntegration';

export function QuickstartWizardPage({
  workspaceID,
  accountID,
  models,
}: {
  workspaceID: string;
  accountID: string;
  models: AgentModelOption[];
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const wizard = useQuickstartWizard(workspaceID, accountID, locale, models);
  return (
    <section className="mx-auto flex h-full min-h-0 w-full min-w-0 max-w-[1600px] flex-1 flex-col overflow-hidden">
      <nav aria-label={text.title} className="mx-auto w-full max-w-4xl shrink-0">
        <ol className="grid grid-cols-4">
          {text.steps.map((label, index) => (
            <li
              key={label}
              className="relative min-w-0"
              aria-current={wizard.progress.step === index ? 'step' : undefined}
            >
              {index < 3 && (
                <span
                  aria-hidden="true"
                  className={`absolute top-6 left-1/2 h-px w-full ${index < wizard.reachableStep - 1 ? 'bg-primary/60' : 'bg-muted-foreground/40'}`}
                />
              )}
              <Button
                variant="ghost"
                className={`relative h-auto w-full flex-col gap-3 px-1 py-2 text-center text-xs whitespace-normal hover:bg-transparent disabled:opacity-100 sm:text-sm dark:hover:bg-transparent ${index === wizard.progress.step ? 'text-foreground' : 'text-muted-foreground'}`}
                disabled={!wizard.canNavigate(index)}
                onClick={() => wizard.navigate(index)}
              >
                <span
                  className={`relative grid size-8 shrink-0 place-items-center rounded-full border ring-4 ring-background ${index === wizard.progress.step ? 'border-primary bg-primary text-primary-foreground' : 'border-muted-foreground/40 bg-background'}`}
                >
                  {index !== wizard.progress.step &&
                  index < wizard.reachableStep &&
                  (index > 0 || wizard.progress.step > 0 || wizard.agent) ? (
                    <Check className="size-4" />
                  ) : (
                    index + 1
                  )}
                </span>
                {label}
              </Button>
            </li>
          ))}
        </ol>
      </nav>
      <div className="max-h-24 shrink-0 overflow-y-auto">
        {wizard.resources.isPending && (
          <p role="status" className="mt-6 text-sm text-muted-foreground">
            {text.restore}
          </p>
        )}
        {(wizard.error || wizard.resources.error) && (
          <div className="mt-6 space-y-3">
            <ManagedErrorAlert>{wizard.error ?? errorMessage(wizard.resources.error)}</ManagedErrorAlert>
            {wizard.resources.isError && (
              <Button variant="outline" onClick={() => void wizard.resources.refetch()}>
                {text.retry}
              </Button>
            )}
          </div>
        )}
        {!wizard.storageAvailable && (
          <p role="status" className="mt-4 text-sm text-muted-foreground">
            {text.storage}
          </p>
        )}
        {wizard.progress.pending && !wizard.busy && (
          <div className="mt-6 space-y-3 rounded-lg border border-border p-4">
            <p className="text-sm text-muted-foreground">{text.unknown}</p>
            <Button disabled={wizard.busy} onClick={() => void wizard.recover()}>
              {text.recover}
            </Button>
          </div>
        )}
      </div>
      {wizard.progress.step === 0 && <QuickstartStart wizard={wizard} />}
      {wizard.progress.step === 1 && <QuickstartAgent wizard={wizard} models={models} workspaceID={workspaceID} />}
      {wizard.progress.step === 2 && <QuickstartEnvironment wizard={wizard} workspaceID={workspaceID} />}
      {wizard.agent && wizard.environment && (
        <div className={wizard.progress.step === 3 ? 'flex min-h-0 flex-1' : 'hidden'}>
          <QuickstartIntegration
            key={quickstartBinding(wizard.agent, wizard.environment.id)}
            wizard={wizard}
            workspaceID={workspaceID}
            accountID={accountID}
            binding={quickstartBinding(wizard.agent, wizard.environment.id)}
          />
        </div>
      )}
    </section>
  );
}

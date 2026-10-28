import './layout.css';
import { useState, type ReactNode } from 'react';
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
  startHeader,
}: {
  workspaceID: string;
  accountID: string;
  models: AgentModelOption[];
  startHeader?: ReactNode;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const wizard = useQuickstartWizard(workspaceID, accountID, locale, models);
  const [started, setStarted] = useState(wizard.progress.step > 0);
  return (
    <>
      {wizard.progress.step === 0 && startHeader}
      <section className="quickstart-wizard mx-auto flex h-full min-h-0 w-full min-w-0 max-w-[1440px] flex-1 flex-col overflow-hidden text-sm">
        {started && (
          <nav aria-label={text.title} className="w-full shrink-0">
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
                      className={`absolute top-[calc(var(--quickstart-step-size)/2+0.25rem)] left-1/2 h-[0.5px] w-full ${index < wizard.reachableStep - 1 ? 'bg-primary/60' : 'bg-muted-foreground/40'}`}
                    />
                  )}
                  <Button
                    variant="ghost"
                    className={`relative h-auto w-full flex-col gap-[clamp(0.375rem,1dvh,0.75rem)] px-1 py-1 text-center text-xs whitespace-normal hover:bg-transparent disabled:opacity-100 dark:hover:bg-transparent ${index === wizard.progress.step ? 'text-foreground' : 'text-muted-foreground'}`}
                    disabled={!wizard.canNavigate(index)}
                    onClick={() => wizard.navigate(index)}
                  >
                    <span
                      className={`relative grid size-(--quickstart-step-size) shrink-0 place-items-center rounded-full border-[0.5px] ring-4 ring-background ${index === wizard.progress.step ? 'border-primary bg-primary text-primary-foreground' : 'border-muted-foreground/40 bg-background'}`}
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
        )}
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
            <div className="mt-6 space-y-3 rounded-lg border-[0.5px] border-border p-4">
              <p className="text-sm text-muted-foreground">{text.unknown}</p>
              <Button disabled={wizard.busy} onClick={() => void wizard.recover()}>
                {text.recover}
              </Button>
              {wizard.canAbandonPending && (
                <div className="space-y-2">
                  <p className="text-sm text-muted-foreground">{text.abandonHint}</p>
                  <Button variant="outline" onClick={wizard.abandonPending}>
                    {text.abandon}
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
        {wizard.progress.step === 0 && (
          <QuickstartStart
            wizard={wizard}
            onStart={() => {
              setStarted(true);
              wizard.navigate(1);
            }}
          />
        )}
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
    </>
  );
}

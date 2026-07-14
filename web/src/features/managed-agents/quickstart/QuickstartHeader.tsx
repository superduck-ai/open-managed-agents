import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import clsx from 'clsx';
import { Check, Play, RotateCcw, Square } from 'lucide-react';
import { quickstartStepLabel } from '../labels';
import { quickstartSteps } from './steps';

export function QuickstartHeader({
  activeStep,
  canTestRun,
  hasAgent,
  hasTemplate,
  isTestRunActive,
  onTitleClick,
  onToggleTestRun,
}: {
  activeStep: number;
  canTestRun: boolean;
  hasAgent: boolean;
  hasTemplate: boolean;
  isTestRunActive: boolean;
  onTitleClick: () => void;
  onToggleTestRun: () => void;
}) {
  const { msg } = useI18n();

  return (
    <header className="grid min-h-12 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-4">
      <Button
        type="button"
        variant="ghost"
        className="w-max justify-self-start px-0 text-sm font-semibold text-foreground hover:bg-transparent hover:text-foreground"
        onClick={onTitleClick}
      >
        {msg('managedAgents.quickstart.title', 'Quickstart')}
        {hasTemplate ? <RotateCcw className="size-3.5 text-muted-foreground" aria-hidden /> : null}
      </Button>

      <ol
        data-testid="quickstart-progress"
        aria-label={msg('managedAgents.quickstart.title', 'Quickstart')}
        className="col-start-2 flex max-w-full items-center justify-self-center rounded-full border border-border bg-card px-2 py-2 shadow-xs sm:px-3 xl:px-4"
      >
        {quickstartSteps.map((step, index) => {
          const isComplete = hasTemplate && activeStep > index;
          const isActive = index === activeStep;
          return (
            <li key={step} className="flex min-w-0 items-center">
              <div className="flex min-w-0 items-center gap-2">
                <span
                  aria-current={isActive ? 'step' : undefined}
                  className={clsx(
                    'grid size-7 shrink-0 place-items-center rounded-full border text-xs font-semibold transition-colors',
                    isComplete || isActive
                      ? 'border-foreground bg-foreground text-background shadow-sm'
                      : 'border-border bg-secondary text-muted-foreground',
                  )}
                >
                  {isComplete ? <Check className="size-3.5" aria-hidden /> : index + 1}
                </span>
                <span
                  className={clsx(
                    'sr-only whitespace-nowrap text-sm xl:not-sr-only',
                    isActive
                      ? 'font-medium text-foreground'
                      : isComplete
                        ? 'font-medium text-foreground/80'
                        : 'text-muted-foreground',
                  )}
                >
                  {quickstartStepLabel(step, msg)}
                </span>
              </div>
              {index < quickstartSteps.length - 1 ? (
                <span
                  className={clsx(
                    'mx-1.5 h-px w-4 shrink-0 sm:mx-2 sm:w-6 xl:mx-3 xl:w-10',
                    activeStep > index ? 'bg-foreground/70' : 'bg-border',
                  )}
                  aria-hidden
                />
              ) : null}
            </li>
          );
        })}
      </ol>

      {hasTemplate && hasAgent ? (
        <Button
          type="button"
          size="sm"
          disabled={!canTestRun}
          className="col-start-3 justify-self-end gap-2"
          onClick={onToggleTestRun}
        >
          {isTestRunActive ? (
            <Square className="size-3.5" aria-hidden />
          ) : (
            <Play className="size-3.5 fill-current" aria-hidden />
          )}
          {isTestRunActive
            ? msg('managedAgents.quickstart.stopSession', 'Stop session')
            : msg('managedAgents.quickstart.testRun', 'Test run')}
        </Button>
      ) : null}
    </header>
  );
}

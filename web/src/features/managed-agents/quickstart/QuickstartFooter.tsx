import type { ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { quickstartCopy } from './copy';
import type { QuickstartWizard } from './useQuickstartWizard';

export function QuickstartFooter({ wizard, children }: { wizard: QuickstartWizard; children: ReactNode }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <div className="mt-auto w-full shrink-0 bg-background pt-[clamp(0.5rem,1.5dvh,1rem)]">
      <footer className="flex w-full flex-wrap items-center justify-between gap-3 border-t border-border pt-[clamp(0.5rem,1.5dvh,1rem)]">
        {wizard.progress.step === 0 ? (
          <p className="text-sm text-muted-foreground">{text.ready}</p>
        ) : (
          <Button
            type="button"
            variant="ghost"
            disabled={wizard.navigationDisabled}
            onClick={() => wizard.navigate(wizard.progress.step - 1)}
          >
            {text.back}
          </Button>
        )}
        <div className="ml-auto flex flex-wrap justify-end gap-3">{children}</div>
      </footer>
    </div>
  );
}

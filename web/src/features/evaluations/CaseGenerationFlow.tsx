import { Loader2 } from 'lucide-react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { DialogClose, DialogFooter } from '../../shared/ui/dialog';
import { CaseDraftReview } from './CaseDraftReview';
import { CasePlanEditor } from './CasePlanEditor';
import { planTotal } from './caseGeneration';
import type { CaseGeneration } from './useCaseGeneration';

export function CaseGenerationStepView({ generation, disabled }: { generation: CaseGeneration; disabled: boolean }) {
  if (generation.step === 'plan') {
    return (
      <CasePlanEditor
        scenarios={generation.scenarios}
        onChange={generation.setScenarios}
        disabled={disabled || generation.busy}
      />
    );
  }
  return (
    <CaseDraftReview
      drafts={generation.drafts}
      stats={generation.stats}
      onChange={generation.setDrafts}
      disabled={disabled || generation.busy}
    />
  );
}

export function CaseGenerationProgress({ generation }: { generation: CaseGeneration }) {
  const { msg } = useI18n();
  if (!generation.busy) return null;
  return (
    <div
      role="status"
      className="flex items-start gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm text-foreground"
    >
      <Loader2 className="mt-0.5 size-4 shrink-0 animate-spin" aria-hidden />
      <p>
        {generation.planning
          ? msg(
              'evaluations.planningStatus',
              'Planning the scenarios. This calls the model and usually takes a little while. Stay on this window.',
            )
          : msg('evaluations.draftingStatus', 'Generating cases. Larger batches take longer. Stay on this window.')}
      </p>
    </div>
  );
}

export function CaseGenerationFooter({
  generation,
  saving,
  finishLabel,
}: {
  generation: CaseGeneration;
  saving: boolean;
  finishLabel: string;
}) {
  const { msg } = useI18n();
  const pending = saving || generation.busy;
  const finishing = generation.step === 'review';
  const keepDrafts = hasGeneratedDrafts(generation) && !generation.planChanged;
  return (
    <DialogFooter className="border-t border-border px-6 py-4">
      {generation.step === 'setup' ? (
        <>
          <DialogClose render={<Button type="button" variant="outline" disabled={pending} />}>
            {msg('common.cancel', 'Cancel')}
          </DialogClose>
          <Button type="button" variant="outline" disabled={pending} onClick={generation.previewPlan}>
            {generation.planning ? <Loader2 className="animate-spin" aria-hidden /> : null}
            {generation.planning
              ? msg('evaluations.planning', 'Planning…')
              : msg('evaluations.previewPlan', 'Review the plan first')}
          </Button>
        </>
      ) : (
        <Button type="button" variant="outline" disabled={pending} onClick={planSecondaryAction(generation)}>
          {planSecondaryLabel(generation, finishing, msg)}
        </Button>
      )}
      <Button
        type={keepDrafts ? 'button' : 'submit'}
        disabled={pending || (finishing && generation.selected.length === 0)}
        onClick={keepDrafts ? generation.returnToDrafts : undefined}
      >
        {pending ? <Loader2 className="animate-spin" aria-hidden /> : null}
        {generationSubmitLabel(generation, finishLabel, msg)}
      </Button>
    </DialogFooter>
  );
}

function hasGeneratedDrafts(generation: CaseGeneration) {
  return generation.step === 'plan' && generation.drafts.length > 0;
}

function planSecondaryAction(generation: CaseGeneration) {
  if (!hasGeneratedDrafts(generation)) return generation.back;
  return generation.planChanged ? generation.returnToDrafts : generation.regenerate;
}

function planSecondaryLabel(
  generation: CaseGeneration,
  finishing: boolean,
  msg: (id: string, fallback: string, values?: Record<string, number>) => string,
) {
  if (!hasGeneratedDrafts(generation)) {
    return finishing ? msg('evaluations.adjustPlan', 'Adjust plan') : msg('evaluations.previousStep', 'Back');
  }
  return generation.planChanged
    ? msg('evaluations.returnToDrafts', 'Back to cases')
    : msg('evaluations.regenerateDrafts', 'Regenerate {count} cases', { count: planTotal(generation.scenarios) });
}

function generationSubmitLabel(
  generation: CaseGeneration,
  finishLabel: string,
  msg: (id: string, fallback: string, values?: Record<string, number>) => string,
) {
  if (generation.planning) return msg('evaluations.planning', 'Planning…');
  if (generation.busy) return msg('evaluations.drafting', 'Generating cases…');
  if (hasGeneratedDrafts(generation)) {
    return generation.planChanged
      ? msg('evaluations.regenerateDrafts', 'Regenerate {count} cases', { count: planTotal(generation.scenarios) })
      : msg('evaluations.returnToDrafts', 'Back to cases');
  }
  if (generation.step === 'setup' || generation.step === 'plan') {
    const count = generation.step === 'setup' ? Number(generation.setup.count) : planTotal(generation.scenarios);
    return msg('evaluations.generateDrafts', 'Generate {count} cases', { count });
  }
  return finishLabel;
}

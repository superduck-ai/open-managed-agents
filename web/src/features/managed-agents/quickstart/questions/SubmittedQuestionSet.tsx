import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../../shared/ui/collapsible';
import clsx from 'clsx';
import { ChevronDown } from 'lucide-react';
import { type QuestionSetController } from './useQuestionSet';

export function SubmittedQuestionSet({
  controller,
  fallbackResult,
}: {
  controller: QuestionSetController;
  fallbackResult?: string;
}) {
  const { msg } = useI18n();
  const { active, actions, completion } = controller;

  if (!completion.isSubmittedQuestionSet) {
    return (
      <p className="px-2 text-xs text-muted-foreground">
        {active.answer
          ? active.answer.answers.join(', ') || msg('managedAgents.quickstart.skipped', 'Skipped')
          : fallbackResult}
      </p>
    );
  }

  return (
    <Collapsible open={completion.reviewOpen} onOpenChange={actions.setReviewVisibility}>
      <CollapsibleTrigger
        type="button"
        aria-label={
          completion.reviewOpen
            ? msg('managedAgents.quickstart.hideAnswers', 'Hide answers')
            : msg('managedAgents.quickstart.reviewAnswers', 'Review answers')
        }
        className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <span>
          {msg('managedAgents.quickstart.answersConfirmed', '{count} answers confirmed', {
            count: completion.submittedAnswers.length,
          })}
        </span>
        <ChevronDown
          className={clsx('ml-auto size-4 transition-transform', completion.reviewOpen && 'rotate-180')}
          aria-hidden
        />
      </CollapsibleTrigger>
      <CollapsibleContent className="px-2 pt-2">
        <p className="text-xs text-muted-foreground">
          {active.answer?.answers.join(', ') || msg('managedAgents.quickstart.skipped', 'Skipped')}
        </p>
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button type="button" variant="secondary" size="sm" disabled={active.index === 0} onClick={actions.previous}>
            {msg('managedAgents.quickstart.prev', 'Previous')}
          </Button>
          <Button type="button" variant="secondary" size="sm" disabled={active.isLast} onClick={actions.nextReview}>
            {msg('managedAgents.quickstart.nextQuestion', 'Next')}
          </Button>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

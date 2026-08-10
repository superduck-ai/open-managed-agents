import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../../shared/ui/collapsible';
import { type QuickstartQuestion } from '../../types';
import { parseSubmittedQuestionAnswers } from '../questionModel';
import clsx from 'clsx';
import { ChevronDown } from 'lucide-react';
import { useState } from 'react';

export function SubmittedQuestionSet({
  questions,
  fallbackResult,
}: {
  questions: QuickstartQuestion[];
  fallbackResult?: string;
}) {
  const { msg } = useI18n();
  const [answerIndex, setAnswerIndex] = useState(0);
  const [reviewOpen, setReviewOpen] = useState(false);
  const answers = parseSubmittedQuestionAnswers(fallbackResult);
  const reviewCount = Math.min(questions.length, answers.length);
  const answer = answers[answerIndex];
  const isQuestionSet = questions.length > 1 && reviewCount > 0;

  if (!isQuestionSet) {
    return (
      <>
        <p className="px-2 pb-1.5 text-sm font-semibold text-foreground">{questions[0]?.question}</p>
        <p className="px-2 text-xs text-muted-foreground">
          {answer ? answer.answers.join(', ') || msg('managedAgents.quickstart.skipped', 'Skipped') : fallbackResult}
        </p>
      </>
    );
  }

  return (
    <Collapsible
      open={reviewOpen}
      onOpenChange={(open) => {
        if (open) {
          setAnswerIndex(0);
        }
        setReviewOpen(open);
      }}
    >
      <div className="flex items-center justify-between gap-3 px-2 pb-1.5">
        <p className="text-sm font-semibold text-foreground">
          {reviewOpen
            ? questions[answerIndex]?.question
            : msg('managedAgents.quickstart.questionSetCompleted', 'Question set completed')}
        </p>
        {reviewOpen ? (
          <div className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground/70">
            {answerIndex + 1}/{reviewCount}
          </div>
        ) : null}
      </div>
      <CollapsibleTrigger
        type="button"
        aria-label={
          reviewOpen
            ? msg('managedAgents.quickstart.hideAnswers', 'Hide answers')
            : msg('managedAgents.quickstart.reviewAnswers', 'Review answers')
        }
        className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <span>
          {msg('managedAgents.quickstart.answersConfirmed', '{count} answers confirmed', { count: reviewCount })}
        </span>
        <ChevronDown className={clsx('ml-auto size-4 transition-transform', reviewOpen && 'rotate-180')} aria-hidden />
      </CollapsibleTrigger>
      <CollapsibleContent className="px-2 pt-2">
        <p className="text-xs text-muted-foreground">
          {answer?.answers.join(', ') || msg('managedAgents.quickstart.skipped', 'Skipped')}
        </p>
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={answerIndex === 0}
            onClick={() => setAnswerIndex((index) => Math.max(0, index - 1))}
          >
            {msg('managedAgents.quickstart.prev', 'Previous')}
          </Button>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={answerIndex === reviewCount - 1}
            onClick={() => setAnswerIndex((index) => Math.min(reviewCount - 1, index + 1))}
          >
            {msg('managedAgents.quickstart.nextQuestion', 'Next')}
          </Button>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

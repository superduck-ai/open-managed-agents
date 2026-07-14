import { useI18n } from '../../../../shared/i18n';
import { type QuickstartQuestion, type QuickstartToolCall, type QuickstartToolExecutionResult } from '../../types';
import { QuickstartAssistantTurn } from '../chatLayout';
import { type QuickstartInteractionResultText } from '../quickstartPromptText';
import { QuestionSetEditor } from './QuestionSetEditor';
import { SubmittedQuestionSet } from './SubmittedQuestionSet';
import { useQuestionSet } from './useQuestionSet';

export function AskUserQuestionsCard({
  call,
  questions,
  results,
  onCompleteTool,
}: {
  call: QuickstartToolCall;
  questions: QuickstartQuestion[];
  results: QuickstartInteractionResultText;
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
}) {
  const { msg } = useI18n();
  const controller = useQuestionSet({ call, questions, onCompleteTool });
  const { active, completion } = controller;

  return (
    <QuickstartAssistantTurn>
      <div
        data-testid="quickstart-question-card"
        className="flex w-full flex-col gap-2 rounded-xl border border-border bg-card px-4 pt-4 pb-3 shadow-xs outline-none"
      >
        <div className="flex items-center justify-between gap-3 px-2 pb-1.5">
          <p className="text-sm font-semibold text-foreground">
            {completion.isSubmittedQuestionSet && !completion.reviewOpen
              ? msg('managedAgents.quickstart.questionSetCompleted', 'Question set completed')
              : active.question?.question}
          </p>
          {questions.length > 1 && (!completion.submitted || completion.reviewOpen) ? (
            <div className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground/70">
              {active.index + 1}/{questions.length}
            </div>
          ) : null}
        </div>

        {completion.submitted ? (
          <SubmittedQuestionSet controller={controller} fallbackResult={call.result} />
        ) : (
          <QuestionSetEditor
            controller={controller}
            onSkip={() => void onCompleteTool(call, { content: results.questionSkipped })}
          />
        )}
      </div>
    </QuickstartAssistantTurn>
  );
}

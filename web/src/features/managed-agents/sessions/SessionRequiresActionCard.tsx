import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  Questionnaire,
  QuestionnaireActions,
  QuestionnaireChoice,
  QuestionnaireChoiceDescription,
  QuestionnaireChoices,
  QuestionnaireError,
  QuestionnaireInput,
  QuestionnaireItem,
  QuestionnaireNext,
  QuestionnairePrevious,
  QuestionnaireProgress,
  QuestionnaireSubmit,
  QuestionnaireTitle,
} from '../../../shared/ui/questionnaire';
import { type SessionToolConfirmationInput, type ToolCallEntry } from '../types';
import { parseQuestionInput } from '../quickstart/questionModel';
import { objectRecord } from '../utils';
import { sessionEventStringField } from '../api';
import { sessionEventType, sessionToolUseInput } from './sessionTraceModel';
import { Check, HelpCircle, X } from 'lucide-react';
import { type FormEvent, useMemo, useState } from 'react';

export function isAskUserQuestionCall(toolCall: ToolCallEntry): boolean {
  const rawName = (
    sessionEventStringField(toolCall.event, 'name') ||
    sessionEventStringField(toolCall.event, 'tool_name') ||
    toolCall.name ||
    ''
  )
    .trim()
    .toLowerCase()
    .replace(/[\s_-]+/g, '');
  return rawName === 'askuserquestion' || rawName === 'askuserquestions';
}

export function sessionToolConfirmationPublicId(toolCall: ToolCallEntry): string {
  const event = toolCall.event;
  if (typeof event.id === 'string' && event.id.trim()) {
    return event.id.trim();
  }
  return toolCall.rawEventId;
}

export function SessionRequiresActionCard({
  toolCall,
  onConfirm,
  disabled = false,
}: {
  toolCall: ToolCallEntry;
  onConfirm: (input: SessionToolConfirmationInput) => Promise<void>;
  disabled?: boolean;
}) {
  const isQuestionnaire = isAskUserQuestionCall(toolCall);
  if (isQuestionnaire) {
    return (
      <SessionQuestionnaireActionCard
        key={sessionToolConfirmationPublicId(toolCall)}
        toolCall={toolCall}
        onConfirm={onConfirm}
        disabled={disabled}
      />
    );
  }
  return <SessionToolApprovalActionCard toolCall={toolCall} onConfirm={onConfirm} disabled={disabled} />;
}

export function SessionToolApprovalActionCard({
  toolCall,
  onConfirm,
  disabled,
}: {
  toolCall: ToolCallEntry;
  onConfirm: (input: SessionToolConfirmationInput) => Promise<void>;
  disabled: boolean;
}) {
  const { msg } = useI18n();
  const [submitting, setSubmitting] = useState(false);
  const toolUseId = sessionToolConfirmationPublicId(toolCall);
  const sessionThreadId = sessionEventStringField(toolCall.event, 'session_thread_id') || undefined;

  const handleDecision = async (result: 'allow' | 'deny') => {
    if (submitting || disabled) {
      return;
    }
    setSubmitting(true);
    try {
      await onConfirm({
        toolUseId,
        result,
        ...(sessionEventType(toolCall.event) === 'agent.custom_tool_use' ? { customTool: true } : {}),
        sessionThreadId,
      });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      data-testid="session-tool-approval-card"
      className="flex w-full flex-wrap items-center gap-2 rounded-md bg-muted/45 px-2 py-1.5 text-foreground"
    >
      <span className="min-w-0 flex-1 text-xs text-muted-foreground">
        {msg('managedAgents.sessions.detail.requiresApproval', 'Requires approval')}
      </span>
      <div className="inline-flex shrink-0 items-center gap-1">
        <Button
          type="button"
          variant="secondary"
          size="xs"
          data-testid="tool-deny-button"
          disabled={submitting || disabled}
          onClick={() => void handleDecision('deny')}
        >
          {msg('managedAgents.sessions.detail.deny', 'Deny')}
        </Button>
        <Button
          type="button"
          variant="default"
          size="xs"
          data-testid="tool-allow-button"
          disabled={submitting || disabled}
          onClick={() => void handleDecision('allow')}
        >
          <Check aria-hidden />
          {msg('managedAgents.sessions.detail.approve', 'Approve')}
        </Button>
      </div>
    </div>
  );
}

export function SessionQuestionnaireActionCard({
  toolCall,
  onConfirm,
  disabled,
}: {
  toolCall: ToolCallEntry;
  onConfirm: (input: SessionToolConfirmationInput) => Promise<void>;
  disabled: boolean;
}) {
  const { msg } = useI18n();
  const [submitting, setSubmitting] = useState(false);
  const toolUseId = sessionToolConfirmationPublicId(toolCall);
  const sessionThreadId = sessionEventStringField(toolCall.event, 'session_thread_id') || undefined;

  const rawInput = objectRecord(sessionToolUseInput(toolCall.event));
  const questions = useMemo(() => parseQuestionInput(rawInput), [rawInput]);
  const items = useMemo(
    () =>
      questions.map((question, index) => ({
        choices: question.options.map((option) => ({ value: option.label })),
        name: `question-${index}`,
      })),
    [questions],
  );

  const [activeItem, setActiveItem] = useState(items[0]?.name ?? '');
  const [drafts, setDrafts] = useState<Record<string, { options: string[]; other: string }>>({});

  const activeDraft = drafts[activeItem];
  const activeItemAnswered = Boolean(activeDraft?.options.length || activeDraft?.other?.trim());

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting || disabled || !activeItemAnswered) {
      return;
    }
    setSubmitting(true);
    try {
      const answers: Record<string, string | string[]> = {};
      questions.forEach((question, index) => {
        const itemKey = items[index]?.name ?? `question-${index}`;
        const draft = drafts[itemKey];
        const selectedOptions = draft?.options ?? [];
        const customText = draft?.other?.trim() ?? '';
        if (question.multiSelect) {
          const combined = [...selectedOptions, ...(customText ? [customText] : [])];
          answers[question.question] = combined;
        } else {
          answers[question.question] = customText || selectedOptions[0] || '';
        }
      });

      await onConfirm({
        toolUseId,
        result: 'allow',
        answers,
        customTool: true,
        sessionThreadId,
      });
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeny = async () => {
    if (submitting || disabled) {
      return;
    }
    setSubmitting(true);
    try {
      await onConfirm({
        toolUseId,
        result: 'deny',
        customTool: true,
        sessionThreadId,
      });
    } finally {
      setSubmitting(false);
    }
  };

  if (!questions.length) {
    return <SessionToolApprovalActionCard toolCall={toolCall} onConfirm={onConfirm} disabled={disabled} />;
  }

  return (
    <div
      data-testid="session-questionnaire-card"
      className="w-full rounded-xl border border-border bg-card p-4 text-foreground shadow-xs"
    >
      <div className="mb-3 flex items-center justify-between border-b border-border pb-2.5">
        <div className="flex items-center gap-2">
          <div className="flex size-6 items-center justify-center rounded-md bg-primary/10 text-primary">
            <HelpCircle className="size-3.5" aria-hidden />
          </div>
          <span className="text-xs font-semibold tracking-wide text-foreground">
            {msg('managedAgents.sessions.detail.questionsPrompt', 'Agent asked questions')}
          </span>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          data-testid="questionnaire-deny-button"
          disabled={submitting || disabled}
          onClick={() => void handleDeny()}
          className="h-6 px-2 text-xs text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
        >
          <X className="mr-1 size-3" aria-hidden />
          {msg('managedAgents.sessions.detail.deny', 'Deny')}
        </Button>
      </div>

      <Questionnaire
        data-testid="session-questionnaire"
        items={items}
        shortcuts="letters"
        onItemChange={setActiveItem}
        onSubmit={(event) => void handleSubmit(event)}
        className="w-full gap-3 py-0.5"
      >
        {questions.length > 1 ? (
          <QuestionnaireProgress
            className="mb-1 text-xs"
            render={(props, state) => (
              <div {...props}>
                {state.current} / {state.total}
              </div>
            )}
          />
        ) : null}
        {questions.map((question, index) => {
          const itemName = items[index]?.name ?? `question-${index}`;
          const currentDraft = drafts[itemName];
          return (
            <QuestionnaireItem key={itemName} multiple={question.multiSelect} name={itemName} className="gap-2.5">
              <QuestionnaireTitle className="text-sm font-medium leading-snug text-foreground">
                {question.question}
              </QuestionnaireTitle>
              <QuestionnaireChoices
                role={question.multiSelect ? undefined : 'radiogroup'}
                aria-label={question.multiSelect ? undefined : question.question}
                className="gap-1.5"
              >
                {question.options.map((option) => (
                  <QuestionnaireChoice
                    key={option.label}
                    value={option.label}
                    checked={currentDraft?.options.includes(option.label) ?? false}
                    className="min-h-9 rounded-lg border border-input bg-transparent px-3 py-2 text-xs transition-colors hover:bg-muted/50 data-checked:border-primary data-checked:bg-primary/5 sm:text-sm"
                    onChange={(event) => {
                      setDrafts((current) => {
                        const draft = current[itemName] ?? { options: [], other: '' };
                        const options = question.multiSelect
                          ? event.target.checked
                            ? [...new Set([...draft.options, option.label])]
                            : draft.options.filter((label) => label !== option.label)
                          : event.target.checked
                            ? [option.label]
                            : [];
                        return { ...current, [itemName]: { options, other: question.multiSelect ? draft.other : '' } };
                      });
                    }}
                  >
                    <span className="font-medium">{option.label}</span>
                    {option.description ? (
                      <QuestionnaireChoiceDescription className="text-[11px] text-muted-foreground sm:text-xs">
                        {option.description}
                      </QuestionnaireChoiceDescription>
                    ) : null}
                  </QuestionnaireChoice>
                ))}
                <QuestionnaireInput
                  aria-label={msg('managedAgents.quickstart.somethingElse', 'Something else')}
                  placeholder={msg('managedAgents.quickstart.somethingElse', 'Something else')}
                  value={currentDraft?.other ?? ''}
                  className="h-8 min-h-8 rounded-lg px-2.5 py-1 text-xs sm:text-sm"
                  onChange={(event) => {
                    setDrafts((current) => {
                      const draft = current[itemName] ?? { options: [], other: '' };
                      return {
                        ...current,
                        [itemName]: {
                          options: question.multiSelect ? draft.options : [],
                          other: event.target.value,
                        },
                      };
                    });
                  }}
                />
              </QuestionnaireChoices>
              <QuestionnaireError />
            </QuestionnaireItem>
          );
        })}
        <QuestionnaireActions className="mt-2 min-h-8 gap-2 sm:min-h-7">
          <QuestionnairePrevious variant="secondary" size="xs" className="h-7 text-xs">
            {msg('managedAgents.quickstart.prev', 'Previous')}
          </QuestionnairePrevious>
          <div className="col-start-2" />
          <QuestionnaireNext disabled={!activeItemAnswered || submitting} size="xs" className="h-7 text-xs">
            {msg('managedAgents.quickstart.nextQuestion', 'Next')}
          </QuestionnaireNext>
          <QuestionnaireSubmit disabled={!activeItemAnswered || submitting} size="xs" className="h-7 text-xs">
            {msg('managedAgents.sessions.detail.confirmAction', 'Confirm')}
          </QuestionnaireSubmit>
        </QuestionnaireActions>
      </Questionnaire>
    </div>
  );
}

import { useI18n } from '../../../shared/i18n';
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
  QuestionnaireSkip,
  QuestionnaireSubmit,
  QuestionnaireTitle,
} from '../../../shared/ui/questionnaire';
import { type QuickstartQuestion } from '../types';
import { useMemo, useState, type FormEvent } from 'react';

export function QuickstartQuestions({
  questions,
  disabled,
  onAnswer,
}: {
  questions: QuickstartQuestion[];
  disabled: boolean;
  onAnswer: (answers: Array<{ header: string; question: string; answers: string[] }>) => void;
}) {
  const { msg } = useI18n();
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
  const activeItemAnswered = Boolean(activeDraft?.options.length || activeDraft?.other.trim());

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const answers = questions.map((question, index) => ({
      answers: [
        ...(drafts[items[index].name]?.options ?? []),
        ...(drafts[items[index].name]?.other.trim() ? [drafts[items[index].name].other.trim()] : []),
      ],
      header: question.header,
      question: question.question,
    }));
    if (!disabled) onAnswer(answers);
  };

  return (
    <fieldset disabled={disabled} className="min-w-0 border-0 p-0">
      <Questionnaire
        data-testid="quickstart-question-card"
        items={items}
        shortcuts="letters"
        onItemChange={setActiveItem}
        onSubmit={handleSubmit}
        className="max-w-xl py-2"
      >
        {questions.length > 1 ? (
          <QuestionnaireProgress
            className="min-w-0"
            render={(props, state) => (
              <div {...props}>
                {state.current} / {state.total}
              </div>
            )}
          />
        ) : null}
        {questions.map((question, index) => (
          <QuestionnaireItem key={items[index].name} multiple={question.multiSelect} name={items[index].name}>
            <QuestionnaireTitle>{question.question}</QuestionnaireTitle>
            <QuestionnaireChoices
              role={question.multiSelect ? undefined : 'radiogroup'}
              aria-label={question.multiSelect ? undefined : question.question}
            >
              {question.options.map((option) => (
                <QuestionnaireChoice
                  key={option.label}
                  value={option.label}
                  checked={drafts[items[index].name]?.options.includes(option.label) ?? false}
                  onChange={(event) => {
                    const itemName = items[index].name;
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
                    <QuestionnaireChoiceDescription>{option.description}</QuestionnaireChoiceDescription>
                  ) : null}
                </QuestionnaireChoice>
              ))}
              <QuestionnaireInput
                aria-label={msg('managedAgents.quickstart.somethingElse', 'Something else')}
                placeholder={msg('managedAgents.quickstart.somethingElse', 'Something else')}
                value={drafts[items[index].name]?.other ?? ''}
                onChange={(event) => {
                  const itemName = items[index].name;
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
        ))}
        <QuestionnaireActions>
          <QuestionnairePrevious variant="secondary">
            {msg('managedAgents.quickstart.prev', 'Previous')}
          </QuestionnairePrevious>
          <QuestionnaireSkip
            variant="ghost"
            onClick={(event) => {
              event.preventDefault();
              onAnswer([]);
            }}
          >
            {msg('managedAgents.quickstart.skip', 'Skip')}
          </QuestionnaireSkip>
          <QuestionnaireNext disabled={!activeItemAnswered}>
            {msg('managedAgents.quickstart.nextQuestion', 'Next')}
          </QuestionnaireNext>
          <QuestionnaireSubmit disabled={!activeItemAnswered}>{msg('common.confirm', 'Confirm')}</QuestionnaireSubmit>
        </QuestionnaireActions>
      </Questionnaire>
    </fieldset>
  );
}

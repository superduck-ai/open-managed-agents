import { useState } from 'react';
import { type QuickstartQuestion, type QuickstartToolCall, type QuickstartToolExecutionResult } from '../../types';
import { parseSubmittedQuestionAnswers } from '../questionModel';

export function useQuestionSet({
  call,
  questions,
  onCompleteTool,
}: {
  call: QuickstartToolCall;
  questions: QuickstartQuestion[];
  onCompleteTool: (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => Promise<void>;
}) {
  const [questionIndex, setQuestionIndex] = useState(0);
  const [selected, setSelected] = useState<Record<number, string[]>>({});
  const [otherValues, setOtherValues] = useState<Record<number, string>>({});
  const [reviewOpen, setReviewOpen] = useState(false);
  const question = questions[questionIndex];
  const selectedOptions = selected[questionIndex] ?? [];
  const otherValue = otherValues[questionIndex] ?? '';
  const submitted = call.status === 'completed';
  const submittedAnswers = submitted ? parseSubmittedQuestionAnswers(call.result) : [];
  const activeSubmittedAnswer = submittedAnswers[questionIndex] ?? submittedAnswers[0];
  const isSubmittedQuestionSet = submitted && questions.length > 1 && submittedAnswers.length > 0;
  const hasAnswer = selectedOptions.length > 0 || otherValue.trim() !== '';
  const isLast = questionIndex === questions.length - 1;

  const submit = async () => {
    const answers = questions.map((item, index) => {
      const labels = selected[index] ?? [];
      const other = otherValues[index]?.trim();
      return {
        header: item.header,
        question: item.question,
        answers: other ? [...labels, other] : labels,
      };
    });
    await onCompleteTool(call, { content: JSON.stringify({ answers }) });
  };

  const selectOption = (label: string) => {
    if (submitted || !question) {
      return;
    }
    setSelected((current) => {
      const values = current[questionIndex] ?? [];
      if (!question.multiSelect) {
        return { ...current, [questionIndex]: [label] };
      }
      return {
        ...current,
        [questionIndex]: values.includes(label) ? values.filter((value) => value !== label) : [...values, label],
      };
    });
  };

  const setMultiSelectOption = (label: string, checked: boolean) => {
    if (submitted || !question?.multiSelect) {
      return;
    }
    setSelected((current) => {
      const values = current[questionIndex] ?? [];
      const nextValues = checked
        ? values.includes(label)
          ? values
          : [...values, label]
        : values.filter((value) => value !== label);
      return { ...current, [questionIndex]: nextValues };
    });
  };

  const nextOrSubmit = async () => {
    if (!hasAnswer) {
      return;
    }
    if (isLast) {
      await submit();
      return;
    }
    setQuestionIndex((index) => index + 1);
  };

  const previous = () => setQuestionIndex((index) => Math.max(0, index - 1));
  const nextReview = () => setQuestionIndex((index) => Math.min(questions.length - 1, index + 1));
  const setOtherValue = (value: string) => setOtherValues((current) => ({ ...current, [questionIndex]: value }));
  const setReviewVisibility = (open: boolean) => {
    if (open) {
      setQuestionIndex(0);
    }
    setReviewOpen(open);
  };

  return {
    active: {
      answer: activeSubmittedAnswer,
      controlName: `quickstart-question-${call.id}-${questionIndex}`,
      hasAnswer,
      index: questionIndex,
      isLast,
      otherValue,
      question,
      selectedOptions,
    },
    completion: { isSubmittedQuestionSet, reviewOpen, submitted, submittedAnswers },
    actions: {
      nextOrSubmit,
      nextReview,
      previous,
      selectOption,
      setMultiSelectOption,
      setOtherValue,
      setReviewVisibility,
    },
  };
}

export type QuestionSetController = ReturnType<typeof useQuestionSet>;

import { type QuickstartQuestion } from '../types';
import { toRecord } from '../utils';

export function parseQuestionInput(input: Record<string, unknown>): QuickstartQuestion[] {
  const questions = Array.isArray(input.questions) ? input.questions : [];
  return questions
    .map((item): QuickstartQuestion | null => {
      const question = toRecord(item);
      if (!question) {
        return null;
      }
      const options = Array.isArray(question.options)
        ? question.options
            .map((option) => {
              const typedOption = toRecord(option);
              if (!typedOption || typeof typedOption.label !== 'string') {
                return null;
              }
              return {
                label: typedOption.label,
                description: typeof typedOption.description === 'string' ? typedOption.description : '',
              };
            })
            .filter((option): option is { label: string; description: string } => Boolean(option))
        : [];
      return {
        header: typeof question.header === 'string' ? question.header : 'Question',
        question: typeof question.question === 'string' ? question.question : 'Choose an option.',
        multiSelect: question.multiSelect === true,
        options,
      };
    })
    .filter((question): question is QuickstartQuestion => Boolean(question));
}

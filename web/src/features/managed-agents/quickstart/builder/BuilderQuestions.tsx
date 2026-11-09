import { QuickstartQuestions } from '../QuickstartQuestions';
import { parseQuestionInput } from '../questionModel';
import { questionInputSchema } from './tools';
import type { BuilderTool } from './model';

export function BuilderQuestions({
  tool,
  disabled,
  onAnswer,
}: {
  tool: BuilderTool;
  disabled: boolean;
  onAnswer: (answer: unknown) => void;
}) {
  questionInputSchema.parse(tool.input);
  return <QuickstartQuestions questions={parseQuestionInput(tool.input)} disabled={disabled} onAnswer={onAnswer} />;
}

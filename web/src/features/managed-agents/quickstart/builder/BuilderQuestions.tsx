import { useState } from 'react';
import { Button } from '../../../../shared/ui/button';
import { Input } from '../../../../shared/ui/input';
import { Checkbox } from '../../../../shared/ui/checkbox';
import { RadioGroup, RadioGroupItem } from '../../../../shared/ui/radio-group';
import { Field, FieldLabel } from '../../../../shared/ui/field';
import { questionInputSchema } from './tools';
import type { BuilderTool } from './model';
import { useI18n } from '../../../../shared/i18n';

export function BuilderQuestions({
  tool,
  disabled,
  onAnswer,
}: {
  tool: BuilderTool;
  disabled: boolean;
  onAnswer: (answer: unknown) => void;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { questions } = questionInputSchema.parse(tool.input);
  const [answers, setAnswers] = useState<Record<string, string[]>>({});
  const [custom, setCustom] = useState<Record<string, string>>({});
  return (
    <form
      className="space-y-5"
      onSubmit={(event) => {
        event.preventDefault();
        onAnswer(
          questions.map((question, index) => ({
            question: question.question,
            answers: [...(answers[String(index)] ?? []), ...(custom[String(index)] ? [custom[String(index)]] : [])],
          })),
        );
      }}
    >
      {questions.map((question, index) => (
        <Field key={index}>
          <FieldLabel>{question.question}</FieldLabel>
          <div className="flex flex-col gap-2">
            <QuestionChoices
              options={question.options}
              multiple={Boolean(question.multiSelect || question.multi_select)}
              values={answers[String(index)] ?? []}
              disabled={disabled}
              onChange={(values) => {
                setAnswers((current) => ({ ...current, [index]: values }));
                if (!question.multiSelect && !question.multi_select)
                  setCustom((current) => ({ ...current, [index]: '' }));
              }}
            />
            <Input
              aria-label={zh ? '其他回答' : 'Your answer'}
              placeholder={zh ? '其他回答…' : 'Your answer…'}
              value={custom[String(index)] ?? ''}
              disabled={disabled}
              onChange={(event) => {
                setCustom((value) => ({ ...value, [index]: event.target.value }));
                if (!question.multiSelect && !question.multi_select) setAnswers((value) => ({ ...value, [index]: [] }));
              }}
            />
          </div>
        </Field>
      ))}
      <Button type="submit" disabled={disabled}>
        {zh ? '继续' : 'Continue'}
      </Button>
    </form>
  );
}

function QuestionChoices({
  options,
  multiple,
  values,
  disabled,
  onChange,
}: {
  options: Array<{ label: string }>;
  multiple: boolean;
  values: string[];
  disabled: boolean;
  onChange: (values: string[]) => void;
}) {
  const classes = 'flex cursor-pointer items-center gap-3 rounded-lg border-[0.5px] border-border px-3 py-2';
  if (!multiple)
    return (
      <RadioGroup value={values[0] ?? ''} disabled={disabled} onValueChange={(value) => onChange([String(value)])}>
        {options.map((option) => (
          <label key={option.label} className={classes}>
            <RadioGroupItem value={option.label} />
            <span>{option.label}</span>
          </label>
        ))}
      </RadioGroup>
    );
  return options.map((option) => (
    <label key={option.label} className={classes}>
      <Checkbox
        disabled={disabled}
        checked={values.includes(option.label)}
        onCheckedChange={(checked) =>
          onChange(checked ? [...values, option.label] : values.filter((value) => value !== option.label))
        }
      />
      <span>{option.label}</span>
    </label>
  ));
}

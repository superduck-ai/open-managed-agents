import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { Checkbox } from '../../../../shared/ui/checkbox';
import { Input } from '../../../../shared/ui/input';
import { Label } from '../../../../shared/ui/label';
import { RadioGroup, RadioGroupItem } from '../../../../shared/ui/radio-group';
import clsx from 'clsx';
import { Pencil } from 'lucide-react';
import { type QuestionSetController } from './useQuestionSet';

export function QuestionSetEditor({ controller, onSkip }: { controller: QuestionSetController; onSkip: () => void }) {
  const { msg } = useI18n();
  const { active, actions } = controller;
  const question = active.question;
  if (!question) {
    return null;
  }
  const radioValue = active.selectedOptions[0] ?? '';

  return (
    <>
      {question.multiSelect ? (
        <div role="group" aria-label={question.question} className="-my-1">
          {question.options.map((option, index) => {
            const checked = active.selectedOptions.includes(option.label);
            const optionId = `${active.controlName}-checkbox-${index}`;
            return (
              <div key={option.label} className="relative">
                {index > 0 ? <span className="absolute inset-x-2 top-0 h-px bg-secondary" /> : null}
                <div
                  className={clsx(
                    'flex items-start gap-3 rounded-lg px-2 py-1.5 text-left hover:bg-accent',
                    checked && 'bg-accent',
                  )}
                >
                  <Checkbox
                    id={optionId}
                    checked={checked}
                    onCheckedChange={(nextChecked) => actions.setMultiSelectOption(option.label, nextChecked === true)}
                    className="mt-1 size-5 rounded-md border-border bg-accent text-primary"
                  />
                  <Label htmlFor={optionId} className="min-w-0 cursor-pointer items-start leading-6 font-normal">
                    <span className="block text-sm text-foreground">{option.label}</span>
                    <span className="mt-0.5 block text-xs text-muted-foreground/70">{option.description}</span>
                  </Label>
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <RadioGroup
          aria-label={question.question}
          name={active.controlName}
          value={radioValue}
          onValueChange={actions.selectOption}
          className="-my-1 gap-0"
        >
          {question.options.map((option, index) => {
            const checked = radioValue === option.label;
            const optionId = `${active.controlName}-radio-${index}`;
            return (
              <div key={option.label} className="relative">
                {index > 0 ? <span className="absolute inset-x-2 top-0 h-px bg-secondary" /> : null}
                <Label
                  htmlFor={optionId}
                  className={clsx(
                    'w-full cursor-pointer items-start gap-3 rounded-lg px-2 py-1.5 text-left leading-6 font-normal hover:bg-accent',
                    checked && 'bg-accent',
                  )}
                >
                  <RadioGroupItem
                    id={optionId}
                    value={option.label}
                    className="mt-1 size-5 border-border bg-accent text-primary"
                  />
                  <span className="min-w-0">
                    <span className="block text-sm text-foreground">{option.label}</span>
                    <span className="mt-0.5 block text-xs text-muted-foreground/70">{option.description}</span>
                  </span>
                </Label>
              </div>
            );
          })}
        </RadioGroup>
      )}
      <div className="relative -my-1">
        <span className="absolute inset-x-2 top-0 h-px bg-secondary" />
        <Label
          htmlFor={`${active.controlName}-other`}
          className="w-full items-center gap-3 rounded-lg px-2 py-1.5 text-left leading-6 font-normal"
        >
          <span className="grid size-6 shrink-0 place-items-center rounded-md border border-border bg-accent text-muted-foreground">
            <Pencil className="size-3.5" aria-hidden />
          </span>
          <Input
            id={`${active.controlName}-other`}
            value={active.otherValue}
            placeholder={msg('managedAgents.quickstart.somethingElse', 'Something else')}
            className="h-auto rounded-none border-none bg-transparent p-0 text-sm placeholder:text-muted-foreground/70 focus-visible:ring-0"
            onChange={(event) => actions.setOtherValue(event.target.value)}
          />
        </Label>
      </div>
      <div className="mt-2 flex items-center justify-end gap-2 px-2">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-muted-foreground hover:bg-accent hover:text-foreground"
          onClick={onSkip}
        >
          {msg('managedAgents.quickstart.skip', 'Skip')}
        </Button>
        {active.index > 0 ? (
          <Button type="button" variant="secondary" size="sm" onClick={actions.previous}>
            {msg('managedAgents.quickstart.prev', 'Previous')}
          </Button>
        ) : null}
        <Button type="button" size="sm" disabled={!active.hasAnswer} onClick={actions.nextOrSubmit}>
          {active.isLast ? msg('common.confirm', 'Confirm') : msg('managedAgents.quickstart.nextQuestion', 'Next')}
        </Button>
      </div>
    </>
  );
}

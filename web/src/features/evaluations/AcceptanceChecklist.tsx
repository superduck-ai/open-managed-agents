import { X } from 'lucide-react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';

export function AcceptanceChecklist({ items, onChange }: { items: string[]; onChange: (items: string[]) => void }) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-3">
      <Label>{msg('evaluations.acceptance', 'Acceptance')}</Label>
      <div className="grid gap-2">
        {items.map((item, index) => (
          <div key={index} className="grid grid-cols-[1.5rem_minmax(0,1fr)_1.5rem] items-center gap-2">
            <span className="inline-flex size-6 items-center justify-center rounded-full bg-muted text-xs text-muted-foreground">
              {index + 1}
            </span>
            <Input
              value={item}
              placeholder={msg('evaluations.checklistPlaceholder', 'One check the answer must satisfy.')}
              aria-label={msg('evaluations.acceptance', 'Acceptance')}
              onChange={(event) =>
                onChange(items.map((current, currentIndex) => (currentIndex === index ? event.target.value : current)))
              }
            />
            {items.length > 1 ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={msg('evaluations.removeCheckpoint', 'Remove check')}
                onClick={() => onChange(items.filter((_, currentIndex) => currentIndex !== index))}
              >
                <X />
              </Button>
            ) : (
              <span />
            )}
          </div>
        ))}
      </div>
      <button
        type="button"
        className="w-fit text-sm text-muted-foreground hover:text-foreground"
        onClick={() => onChange([...items, ''])}
      >
        + {msg('evaluations.addCheckpoint', 'Add check')}
      </button>
    </div>
  );
}

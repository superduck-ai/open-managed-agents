import { AlertCircle } from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useI18n } from '../i18n';
import { Alert, AlertDescription } from '../ui/alert';
import { Button } from '../ui/button';
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../ui/dialog';
import { Field, FieldLabel } from '../ui/field';
import { Input } from '../ui/input';
import { RadioGroup, RadioGroupItem } from '../ui/radio-group';
import { workspaceColors } from './presentation';

type EditWorkspaceDialogProps = {
  open: boolean;
  workspace: { id: string; name: string; display_color?: string; color?: string } | null;
  onClose: () => void;
  onSave: (name: string, displayColor: string) => Promise<void>;
};

export function EditWorkspaceDialog({ open, workspace, onClose, onSave }: EditWorkspaceDialogProps) {
  const { msg } = useI18n();
  const nameRef = useRef<HTMLInputElement>(null);
  const errorRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState('');
  const [selectedColor, setSelectedColor] = useState<string>(workspaceColors[1].value);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  useEffect(() => {
    if (!open || !workspace) {
      return;
    }
    setName(workspace.name);
    setSelectedColor(workspace.display_color || workspace.color || workspaceColors[1].value);
    setSubmitting(false);
    setError('');
  }, [open, workspace]);

  const canSave = name.trim().length > 0 && !submitting;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSave) {
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      await onSave(name.trim(), selectedColor);
      onClose();
    } catch (saveError) {
      setError(
        saveError && typeof saveError === 'object' && 'message' in saveError && typeof saveError.message === 'string'
          ? saveError.message
          : msg('workspace.update.error', 'Failed to update workspace.'),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next, details) => {
        if (!next && submitting) {
          details.cancel();
          return;
        }
        if (!next) onClose();
      }}
    >
      <DialogContent
        className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-[520px]"
        initialFocus={nameRef}
        closeButtonDisabled={submitting}
      >
        <DialogHeader>
          <DialogTitle>{msg('workspace.edit.title', 'Edit workspace')}</DialogTitle>
        </DialogHeader>

        <form className="flex min-h-0 flex-col gap-5" onSubmit={handleSubmit}>
          <div className="subtle-scrollbar min-h-0 flex-1 overflow-y-auto pr-1">
            <fieldset disabled={submitting} className="min-w-0 space-y-5">
              <Field className="gap-2">
                <FieldLabel htmlFor="workspace-edit-name">{msg('common.name', 'Name')}</FieldLabel>
                <Input
                  id="workspace-edit-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  ref={nameRef}
                />
              </Field>

              <Field className="gap-2">
                <FieldLabel>{msg('common.color', 'Color')}</FieldLabel>
                <RadioGroup
                  aria-label={msg('workspace.colorAria', 'Workspace color')}
                  value={selectedColor}
                  onValueChange={(nextValue) => {
                    if (nextValue) {
                      setSelectedColor(nextValue);
                    }
                  }}
                  className="grid w-max max-w-max grid-cols-5 gap-2"
                >
                  {workspaceColors.map((color) => (
                    <RadioGroupItem
                      key={color.name}
                      value={color.value}
                      aria-label={msg(`workspace.color.${color.name.toLowerCase()}`, color.name)}
                      className="size-8 rounded-md border-[0.5px] border-border bg-transparent p-0 after:hidden focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-foreground data-checked:outline-[0.5px] data-checked:outline-offset-2 data-checked:outline-foreground [&>[data-slot=radio-group-indicator]]:hidden"
                      style={{ backgroundColor: color.displayValue }}
                    />
                  ))}
                </RadioGroup>
              </Field>

              {error ? (
                <Alert
                  ref={errorRef}
                  tabIndex={-1}
                  variant="destructive"
                  className="outline-none [overflow-wrap:anywhere]"
                >
                  <AlertCircle className="size-4" aria-hidden />
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              ) : null}
            </fieldset>
          </div>
          <DialogFooter className="shrink-0">
            <DialogClose render={<Button type="button" variant="outline" disabled={submitting} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={!canSave}>
              {submitting ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

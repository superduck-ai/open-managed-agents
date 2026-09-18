import { AlertCircle } from 'lucide-react';
import { useEffect, useState, type FormEvent } from 'react';
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
  const [name, setName] = useState('');
  const [selectedColor, setSelectedColor] = useState<string>(workspaceColors[1].value);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

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
      onOpenChange={(next) => {
        if (!next && !submitting) onClose();
      }}
    >
      <DialogContent className="sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{msg('workspace.edit.title', 'Edit workspace')}</DialogTitle>
        </DialogHeader>

        <form className="space-y-5" onSubmit={handleSubmit}>
          <Field className="gap-2">
            <FieldLabel htmlFor="workspace-edit-name">{msg('common.name', 'Name')}</FieldLabel>
            <Input id="workspace-edit-name" value={name} onChange={(event) => setName(event.target.value)} autoFocus />
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
              className="grid w-max grid-cols-5 gap-2"
            >
              {workspaceColors.map((color) => (
                <RadioGroupItem
                  key={color.name}
                  value={color.value}
                  aria-label={color.name}
                  className="size-8 rounded-md border border-border bg-transparent p-0 after:hidden focus-visible:ring-2 focus-visible:ring-ring/50 [&>[data-slot=radio-group-indicator]]:hidden"
                  style={{ backgroundColor: color.value }}
                />
              ))}
            </RadioGroup>
          </Field>

          {error ? (
            <Alert variant="destructive">
              <AlertCircle className="size-4" aria-hidden />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
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

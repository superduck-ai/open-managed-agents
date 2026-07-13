import { AlertCircle } from 'lucide-react';
import { useEffect, useState, type FormEvent } from 'react';
import { useI18n } from '../i18n';
import { Alert, AlertDescription } from '../ui/alert';
import { Button } from '../ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '../ui/dialog';
import { Field, FieldDescription, FieldLabel } from '../ui/field';
import { Input } from '../ui/input';
import { RadioGroup, RadioGroupItem } from '../ui/radio-group';
import { workspaceColors } from './presentation';

type CreateWorkspaceDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (name: string, displayColor: string) => Promise<void>;
  trigger?: React.ReactElement;
};

export function CreateWorkspaceDialog({ open, onOpenChange, onCreate, trigger }: CreateWorkspaceDialogProps) {
  const { msg } = useI18n();
  const [name, setName] = useState('');
  const [selectedColor, setSelectedColor] = useState(workspaceColors[1].value);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const canCreate = name.trim().length > 0 && !submitting;

  useEffect(() => {
    if (open) {
      return;
    }
    setName('');
    setSelectedColor(workspaceColors[1].value);
    setSubmitting(false);
    setError('');
  }, [open]);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canCreate) {
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      await onCreate(name.trim(), selectedColor);
      onOpenChange(false);
    } catch (createError) {
      setError(
        createError instanceof Error
          ? createError.message
          : msg('workspace.create.error', 'Failed to create workspace.'),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger ? <DialogTrigger render={trigger} /> : null}
      <DialogContent className="sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{msg('workspace.create.title', 'Create workspace')}</DialogTitle>
          <DialogDescription className="max-w-[430px]">
            {msg('workspace.create.body', 'Workspaces allow you to separate API Keys and configurations by use case.')}
          </DialogDescription>
        </DialogHeader>

        <form className="space-y-5" onSubmit={handleSubmit}>
          <Field className="gap-2">
            <FieldLabel htmlFor="workspace-name">{msg('common.name', 'Name')}</FieldLabel>
            <Input
              id="workspace-name"
              value={name}
              placeholder={msg('workspace.create.namePlaceholder', 'Enter a name for your workspace')}
              onChange={(event) => setName(event.target.value)}
              autoFocus
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
              className="grid w-max grid-cols-5 gap-2"
            >
              {workspaceColors.map((color) => (
                <RadioGroupItem
                  key={color.name}
                  value={color.value}
                  aria-label={color.name}
                  className="size-8 rounded-md border border-border bg-transparent p-0 after:hidden focus-visible:ring-2 focus-visible:ring-ring/50 data-checked:border-primary data-checked:ring-2 data-checked:ring-ring/60 [&>[data-slot=radio-group-indicator]]:hidden"
                  style={{ backgroundColor: color.value }}
                />
              ))}
            </RadioGroup>
          </Field>

          <Field className="gap-2">
            <FieldLabel htmlFor="workspace-geo">{msg('workspace.geo', 'Workspace geo')}</FieldLabel>
            <Input id="workspace-geo" value="US" readOnly aria-readonly="true" />
            <FieldDescription>
              {msg(
                'workspace.geoHelp',
                "Control where your workspace data, including files, conversation history, and workspace artifacts, is stored. This can't be changed after creation.",
              )}
            </FieldDescription>
          </Field>

          {error ? (
            <Alert variant="destructive">
              <AlertCircle className="size-4" aria-hidden />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={!canCreate}>
              {submitting ? msg('common.creating', 'Creating...') : msg('common.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

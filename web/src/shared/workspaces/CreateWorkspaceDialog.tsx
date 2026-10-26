import { AlertCircle } from 'lucide-react';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useI18n } from '../i18n';
import { Alert, AlertDescription } from '../ui/alert';
import { Button } from '../ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
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
  const nameRef = useRef<HTMLInputElement>(null);
  const errorRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState('');
  const [selectedColor, setSelectedColor] = useState(workspaceColors[1].value);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);
  const canCreate = name.trim().length > 0 && !submitting;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canCreate) {
      return;
    }
    if (name.trim().toLowerCase() === 'default') {
      setError(
        msg('workspace.create.reservedName', 'Default is reserved for the default workspace. Choose another name.'),
      );
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      await onCreate(name.trim(), selectedColor);
      onOpenChange(false);
    } catch (createError) {
      setError(
        createError &&
          typeof createError === 'object' &&
          'message' in createError &&
          typeof createError.message === 'string'
          ? createError.message
          : msg('workspace.create.error', 'Failed to create workspace.'),
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
        onOpenChange(next);
      }}
      onOpenChangeComplete={(next) => {
        if (!next) {
          setName('');
          setSelectedColor(workspaceColors[1].value);
          setSubmitting(false);
          setError('');
        }
      }}
    >
      {trigger ? <DialogTrigger render={trigger} /> : null}
      <DialogContent
        className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-[520px]"
        initialFocus={nameRef}
        closeButtonDisabled={submitting}
      >
        <DialogHeader>
          <DialogTitle>{msg('workspace.create.title', 'Create workspace')}</DialogTitle>
        </DialogHeader>

        <form className="flex min-h-0 flex-col gap-5" onSubmit={handleSubmit}>
          <div className="subtle-scrollbar min-h-0 flex-1 overflow-y-auto pr-1">
            <fieldset disabled={submitting} className="min-w-0 space-y-5">
              <Field className="gap-2">
                <FieldLabel htmlFor="workspace-name">{msg('common.name', 'Name')}</FieldLabel>
                <Input
                  id="workspace-name"
                  value={name}
                  placeholder={msg('workspace.create.namePlaceholder', 'Enter a name for your workspace')}
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
                      className="size-8 rounded-md border-[0.5px] border-border bg-transparent p-0 after:hidden focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-foreground data-checked:border-primary data-checked:outline-[0.5px] data-checked:outline-offset-2 data-checked:outline-foreground [&>[data-slot=radio-group-indicator]]:hidden"
                      style={{ backgroundColor: color.displayValue }}
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
            <Button type="submit" disabled={!canCreate}>
              {submitting ? msg('common.creating', 'Creating...') : msg('common.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

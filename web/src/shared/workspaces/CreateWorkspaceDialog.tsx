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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select';
import { listOrganizationRuntimeProfiles, type OrganizationRuntimeProfile } from './api';
import { useWorkspace } from './context';
import { workspaceColors } from './presentation';

type CreateWorkspaceDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (name: string, displayColor: string, runtimeProfileId?: string) => Promise<void>;
  trigger?: React.ReactElement;
  orgUuid?: string;
};

function useAvailableRuntimeProfiles(open: boolean, orgUuid?: string) {
  const [profiles, setProfiles] = useState<OrganizationRuntimeProfile[]>([]);
  const [defaultProfileId, setDefaultProfileId] = useState('');

  useEffect(() => {
    if (!open || !orgUuid) {
      setProfiles([]);
      setDefaultProfileId('');
      return;
    }
    let active = true;
    void listOrganizationRuntimeProfiles(orgUuid)
      .then((result) => {
        if (!active) return;
        const available = result.profiles.filter(
          (profile) => profile.status !== 'archived' && profile.status !== 'disabled',
        );
        setProfiles(available);
        const preferred =
          available.find((profile) => profile.id === result.defaultProfileId) ??
          available.find((profile) => profile.uuid === result.defaultProfileUuid) ??
          available[0];
        setDefaultProfileId(preferred ? preferred.id || preferred.uuid : '');
      })
      .catch(() => {
        if (!active) return;
        setProfiles([]);
        setDefaultProfileId('');
      });
    return () => {
      active = false;
    };
  }, [open, orgUuid]);

  return { profiles, defaultProfileId };
}

function runtimeProfileOptionValue(profile: OrganizationRuntimeProfile) {
  return profile.id || profile.uuid;
}

export function CreateWorkspaceDialog({ open, onOpenChange, onCreate, trigger, orgUuid }: CreateWorkspaceDialogProps) {
  const { msg } = useI18n();
  const workspaceContext = useWorkspace();
  const effectiveOrgUuid = orgUuid ?? workspaceContext.orgUuid;
  const { profiles: runtimeProfiles, defaultProfileId } = useAvailableRuntimeProfiles(open, effectiveOrgUuid);
  const nameRef = useRef<HTMLInputElement>(null);
  const errorRef = useRef<HTMLDivElement>(null);
  const [name, setName] = useState('');
  const [selectedColor, setSelectedColor] = useState(workspaceColors[1].value);
  const [selectedRuntimeProfileId, setSelectedRuntimeProfileId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  useEffect(() => {
    if (defaultProfileId) {
      setSelectedRuntimeProfileId((current) => current || defaultProfileId);
    }
  }, [defaultProfileId]);

  const showRuntimeSelector = runtimeProfiles.length > 1;
  const activeRuntimeProfileId = showRuntimeSelector
    ? runtimeProfiles.some((profile) => runtimeProfileOptionValue(profile) === selectedRuntimeProfileId)
      ? selectedRuntimeProfileId
      : defaultProfileId
    : '';
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
      if (showRuntimeSelector && activeRuntimeProfileId) {
        await onCreate(name.trim(), selectedColor, activeRuntimeProfileId);
      } else {
        await onCreate(name.trim(), selectedColor);
      }
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

  const runtimeItems = runtimeProfiles.map((profile) => ({
    value: runtimeProfileOptionValue(profile),
    label: profile.name,
  }));

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
          setSelectedRuntimeProfileId('');
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

              {showRuntimeSelector ? (
                <Field className="gap-2">
                  <FieldLabel htmlFor="workspace-runtime-profile">
                    {msg('workspace.runtimeEngine', 'Runtime engine')}
                  </FieldLabel>
                  <Select
                    value={activeRuntimeProfileId}
                    items={runtimeItems}
                    onValueChange={(nextValue) => {
                      if (nextValue) {
                        setSelectedRuntimeProfileId(nextValue);
                      }
                    }}
                  >
                    <SelectTrigger
                      id="workspace-runtime-profile"
                      aria-label={msg('workspace.runtimeEngine', 'Runtime engine')}
                      className="w-full"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {runtimeProfiles.map((profile) => {
                        const value = runtimeProfileOptionValue(profile);
                        return (
                          <SelectItem key={value} value={value} label={profile.name}>
                            {profile.name}
                          </SelectItem>
                        );
                      })}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {msg(
                      'workspace.runtimeEngineHelp',
                      "Choose which managed agent runtime powers this workspace. This can't be changed after creation.",
                    )}
                  </FieldDescription>
                </Field>
              ) : null}

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

import { useI18n } from '../../../shared/i18n';
import { ManagedTextArea, ManagedTextField } from '../components/common';

export function EnvironmentCreationFields({
  description,
  onDescriptionChange,
}: {
  description: string;
  onDescriptionChange: (description: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <ManagedTextField
        label={msg('managedAgents.environments.hostingType', 'Hosting type')}
        value={msg('managedAgents.environments.cloud', 'Cloud')}
        disabled
        onChange={() => undefined}
      />
      <ManagedTextArea
        label={msg('common.description', 'Description')}
        value={description}
        placeholder={msg('managedAgents.common.descriptionPlaceholder', 'Add a description')}
        onChange={onDescriptionChange}
      />
    </>
  );
}

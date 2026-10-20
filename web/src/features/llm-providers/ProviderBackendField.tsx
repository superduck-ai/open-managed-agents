import { useI18n } from '../../shared/i18n';
import { Field, FieldDescription, FieldLabel } from '../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import type { APIBackend } from './api';

const backends = [
  { value: 'messages', label: 'Anthropic Messages' },
  { value: 'chat_completions', label: 'OpenAI Chat Completions' },
];

export function ProviderBackendField({
  value,
  onChange,
}: {
  value: APIBackend;
  onChange: (value: APIBackend) => void;
}) {
  const { msg } = useI18n();
  return (
    <Field>
      <FieldLabel htmlFor="llm-provider-backend">{msg('llmModels.apiBackend', 'API backend')}</FieldLabel>
      <Select
        value={value}
        items={backends}
        onValueChange={(next) => {
          if (next === 'messages' || next === 'chat_completions') onChange(next);
        }}
      >
        <SelectTrigger id="llm-provider-backend" className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {backends.map((backend) => (
            <SelectItem key={backend.value} value={backend.value}>
              {backend.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {value === 'chat_completions' ? (
        <FieldDescription>
          {msg('llmModels.chatBrainOnly', 'Chat Completions is supported in brain mode only.')}
        </FieldDescription>
      ) : null}
    </Field>
  );
}

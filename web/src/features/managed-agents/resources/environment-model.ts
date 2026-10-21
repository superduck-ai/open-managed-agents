import {
  type I18nMsg,
  type EnvironmentEditValues,
  type EnvironmentResourceValues,
  type ManagedEntitySection,
} from '../types';
import { errorMessage } from '../utils';
import { environmentConfigBody, environmentMetadataBody } from './model';

export type EnvironmentOperation = 'archive' | 'create' | 'delete' | 'list' | 'load' | 'update' | 'work';

export type EnvironmentValidationErrors = {
  name?: string;
  packages: Record<number, string>;
  metadataRows: Record<number, { key?: string; value?: string }>;
  metadata?: string;
  initScript?: string;
  environmentRows?: Record<number, { key?: string; value?: string }>;
  resources?: Partial<Record<keyof EnvironmentResourceValues, string>>;
};

const environmentVariableName = /^[A-Za-z_][A-Za-z0-9_]*$/;
const reservedEnvironmentPrefixes = ['ANTHROPIC_', 'CLAUDE_', 'OMA_'];

function environmentVariableKeyError(key: string, counts: Map<string, number>, msg: I18nMsg) {
  if (!environmentVariableName.test(key)) {
    return msg(
      'environmentTemplate.validation.variableName',
      'Use letters, digits and underscores, not starting with a digit.',
    );
  }
  if (reservedEnvironmentPrefixes.some((prefix) => key.toUpperCase().startsWith(prefix))) {
    return msg(
      'environmentTemplate.validation.variableReserved',
      'ANTHROPIC_, CLAUDE_ and OMA_ prefixes are reserved.',
    );
  }
  if ((counts.get(key) ?? 0) > 1) {
    return msg('environmentTemplate.validation.variableDuplicate', 'Environment variable names must be unique.');
  }
  return undefined;
}

export function validateEnvironmentTemplate(values: EnvironmentEditValues, msg: I18nMsg) {
  const errors: Pick<EnvironmentValidationErrors, 'initScript' | 'environmentRows' | 'resources'> = {
    environmentRows: {},
    resources: {},
  };
  if (utf8ByteLength(values.initScript.trim()) > 64 * 1024) {
    errors.initScript = msg(
      'environmentTemplate.validation.scriptLength',
      'The init script must be 64 KiB or smaller.',
    );
  }
  const counts = new Map<string, number>();
  values.environmentRows.forEach((row) => counts.set(row.key.trim(), (counts.get(row.key.trim()) ?? 0) + 1));
  values.environmentRows.forEach((row, index) => {
    const key = row.key.trim();
    if (!key && !row.value) return;
    const error = key
      ? environmentVariableKeyError(key, counts, msg)
      : msg('environmentTemplate.validation.variableRequired', 'Enter a variable name.');
    if (error) errors.environmentRows![index] = { key: error };
  });
  for (const name of ['cpu', 'memory', 'disk'] as const) {
    const text = values.resources[name].trim();
    if (text && !/^[1-9][0-9]*$/.test(text)) {
      errors.resources![name] = msg('environmentTemplate.validation.resource', 'Enter a positive whole number.');
    }
  }
  return errors;
}

const environmentErrorPatterns: Array<{ pattern: RegExp; key: string; fallback: string }> = [
  {
    pattern: /environment name already exists/i,
    key: 'managedAgents.environments.errors.nameExists',
    fallback: 'An environment with this name already exists.',
  },
  {
    pattern: /environment not found|^not found$/i,
    key: 'managedAgents.environments.errors.notFound',
    fallback: 'Environment not found.',
  },
  {
    pattern: /name (?:is required|cannot be null|must be a string|must be non-empty)/i,
    key: 'managedAgents.environments.validation.nameRequired',
    fallback: 'Enter an environment name.',
  },
  {
    pattern: /config\.packages\..+ entries must be non-empty strings/i,
    key: 'managedAgents.environments.validation.packageRequired',
    fallback: 'Package values must contain non-empty package tokens.',
  },
  {
    pattern: /config\.packages\..+ entries must be at most 255 characters/i,
    key: 'managedAgents.environments.validation.packageTooLong',
    fallback: 'Each package token must be 255 UTF-8 bytes or fewer.',
  },
  {
    pattern: /metadata may contain at most 16 entries/i,
    key: 'managedAgents.environments.validation.metadataLimit',
    fallback: 'Metadata can contain at most 16 entries.',
  },
  {
    pattern: /metadata keys must be between 1 and 64 characters/i,
    key: 'managedAgents.environments.validation.metadataKeyLength',
    fallback: 'Metadata keys must be between 1 and 64 UTF-8 bytes.',
  },
  {
    pattern: /metadata values must be at most 512 characters/i,
    key: 'managedAgents.environments.validation.metadataValueLength',
    fallback: 'Metadata values must be 512 UTF-8 bytes or fewer.',
  },
  {
    pattern: /metadata must be an object/i,
    key: 'managedAgents.environments.errors.invalidMetadata',
    fallback: 'The server rejected the metadata configuration.',
  },
  {
    pattern: /config\.packages must be an object/i,
    key: 'managedAgents.environments.errors.invalidPackages',
    fallback: 'The server rejected the packages configuration.',
  },
  {
    pattern: /config\.networking/i,
    key: 'managedAgents.environments.errors.invalidNetworking',
    fallback: 'The server rejected the networking configuration.',
  },
  {
    pattern: /environment has active work/i,
    key: 'managedAgents.environments.errors.activeWork',
    fallback: 'This environment cannot be deleted while it has active work.',
  },
  {
    pattern: /environments api requires beta=true/i,
    key: 'managedAgents.environments.errors.betaRequired',
    fallback: 'The Environments API beta header is missing.',
  },
];

const operationMessages: Record<EnvironmentOperation, { key: string; fallback: string }> = {
  archive: { key: 'managedAgents.environments.errors.archive', fallback: 'Could not archive the environment.' },
  create: { key: 'managedAgents.environments.errors.create', fallback: 'Could not create the environment.' },
  delete: { key: 'managedAgents.environments.errors.delete', fallback: 'Could not delete the environment.' },
  list: { key: 'managedAgents.environments.errors.list', fallback: 'Could not load environments.' },
  load: { key: 'managedAgents.environments.errors.load', fallback: 'Could not load the environment.' },
  update: { key: 'managedAgents.environments.errors.update', fallback: 'Could not save the environment.' },
  work: { key: 'managedAgents.environments.errors.work', fallback: 'Could not load the work queue.' },
};

const utf8Encoder = new TextEncoder();

function utf8ByteLength(value: string) {
  return utf8Encoder.encode(value).length;
}

export function environmentErrorMessage(error: unknown, operation: EnvironmentOperation, msg: I18nMsg) {
  const detail = errorMessage(error);
  const known = environmentErrorPatterns.find(({ pattern }) => pattern.test(detail));
  if (known) {
    return msg(known.key, known.fallback);
  }
  const summary = operationMessages[operation];
  const localizedSummary = msg(summary.key, summary.fallback);
  if (/^could not (?:archive|create|delete|list|retrieve|update) environment/i.test(detail)) {
    return localizedSummary;
  }
  return msg('managedAgents.environments.errors.withDetail', '{summary} Server detail: {detail}', {
    summary: localizedSummary,
    detail,
  });
}

export function managedEntityErrorMessage(
  section: ManagedEntitySection,
  error: unknown,
  operation: EnvironmentOperation,
  msg: I18nMsg,
) {
  return section === 'environments' ? environmentErrorMessage(error, operation, msg) : errorMessage(error);
}

export function validateEnvironment(
  values: EnvironmentEditValues,
  msg: I18nMsg,
  initialValues?: EnvironmentEditValues,
): EnvironmentValidationErrors {
  const errors: EnvironmentValidationErrors = { packages: {}, metadataRows: {} };
  const initialMetadata = initialValues ? environmentMetadataBody(initialValues) : {};
  Object.assign(errors, validateEnvironmentTemplate(values, msg));
  if (!values.name.trim()) {
    errors.name = msg('managedAgents.environments.validation.nameRequired', 'Enter an environment name.');
  }
  values.packages.forEach((row, index) => {
    const tokens = row.value.split(/\s+/).filter(Boolean);
    if (tokens.some((token) => utf8ByteLength(token) > 255)) {
      errors.packages[index] = msg(
        'managedAgents.environments.validation.packageTooLong',
        'Each package token must be 255 UTF-8 bytes or fewer.',
      );
    }
  });
  if (values.metadataRows.length > 16) {
    errors.metadata = msg(
      'managedAgents.environments.validation.metadataLimit',
      'Metadata can contain at most 16 entries.',
    );
  }
  const keyCounts = new Map<string, number>();
  values.metadataRows.forEach((row) => {
    const key = row.key;
    if (key.trim()) {
      keyCounts.set(key, (keyCounts.get(key) ?? 0) + 1);
    }
  });
  values.metadataRows.forEach((row, index) => {
    const key = row.key;
    const keyPresent = Boolean(key.trim());
    const active = Boolean(keyPresent || row.value);
    const rowErrors: { key?: string; value?: string } = {};
    if (active && !keyPresent) {
      rowErrors.key = msg('managedAgents.environments.validation.metadataKeyRequired', 'Enter a metadata key.');
    } else if (utf8ByteLength(key) > 64) {
      rowErrors.key = msg(
        'managedAgents.environments.validation.metadataKeyLength',
        'Metadata keys must be between 1 and 64 UTF-8 bytes.',
      );
    } else if (keyPresent && (keyCounts.get(key) ?? 0) > 1) {
      rowErrors.key = msg(
        'managedAgents.environments.validation.metadataKeyDuplicate',
        'Metadata keys must be unique.',
      );
    }
    if (keyPresent && row.value === '' && initialMetadata[key] !== '') {
      rowErrors.value = msg(
        'managedAgents.environments.validation.metadataValueRequiredOnUpdate',
        'Enter a metadata value, or remove the row to delete this entry.',
      );
    } else if (utf8ByteLength(row.value) > 512) {
      rowErrors.value = msg(
        'managedAgents.environments.validation.metadataValueLength',
        'Metadata values must be 512 UTF-8 bytes or fewer.',
      );
    }
    if (rowErrors.key || rowErrors.value) {
      errors.metadataRows[index] = rowErrors;
    }
  });
  return errors;
}

export function hasEnvironmentValidationErrors(errors: EnvironmentValidationErrors) {
  return Boolean(
    errors.name ||
    errors.metadata ||
    errors.initScript ||
    Object.keys(errors.packages).length ||
    Object.keys(errors.metadataRows).length ||
    Object.keys(errors.environmentRows ?? {}).length ||
    Object.keys(errors.resources ?? {}).length,
  );
}

export function environmentFormFingerprint(values: EnvironmentEditValues) {
  const metadata = environmentMetadataBody(values);
  return JSON.stringify({
    name: values.name.trim(),
    description: values.description.trim(),
    config: environmentConfigBody(values),
    metadata: Object.fromEntries(
      Object.entries(metadata).sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0)),
    ),
  });
}

export function localizedRelativeTime(
  value: string,
  format: (value: number, unit: Intl.RelativeTimeFormatUnit) => string,
) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return '—';
  }
  const seconds = Math.round((timestamp - Date.now()) / 1000);
  if (Math.abs(seconds) < 60) {
    return format(0, 'second');
  }
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) {
    return format(minutes, 'minute');
  }
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) {
    return format(hours, 'hour');
  }
  return format(Math.round(hours / 24), 'day');
}

export function environmentWorkStatusLabel(status: string | undefined, msg: I18nMsg) {
  const value = status?.trim() || 'queued';
  const known: Record<string, [string, string]> = {
    queued: ['managedAgents.environments.work.status.queued', 'Queued'],
    starting: ['managedAgents.environments.work.status.starting', 'Starting'],
    active: ['managedAgents.environments.work.status.active', 'Active'],
    stopping: ['managedAgents.environments.work.status.stopping', 'Stopping'],
    stopped: ['managedAgents.environments.work.status.stopped', 'Stopped'],
    failed: ['managedAgents.environments.work.status.failed', 'Failed'],
  };
  const translation = known[value.toLowerCase()];
  if (translation) {
    return msg(translation[0], translation[1]);
  }
  return msg('managedAgents.environments.work.status.unknown', 'Unknown status ({status})', { status: value });
}

export function environmentScopeLabel(scope: string | undefined, msg: I18nMsg) {
  const value = scope?.trim();
  if (!value) {
    return '—';
  }
  const known: Record<string, [string, string]> = {
    account: ['managedAgents.environments.account', 'Account'],
    organization: ['managedAgents.environments.organization', 'Organization'],
    workspace: ['managedAgents.environments.workspace', 'Workspace'],
  };
  const translation = known[value.toLowerCase()];
  return translation ? msg(translation[0], translation[1]) : value;
}

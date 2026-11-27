import type {
  RuntimeProfileCreateInput,
  RuntimeProfileRecord,
  RuntimeProfileStatus,
  RuntimeProfileUpdateInput,
} from '../../../shared/api/platform-admin';

export const QODER_CLOUD_DEFAULT_ENDPOINT = 'https://api.qoder.com.cn/api/v1/cloud';

export type RuntimeProfileFormValues = {
  name: string;
  providerKind: string;
  endpointUrl: string;
  credentialToken: string;
  enabled: boolean;
};

export function initialRuntimeProfileFormValues(profile?: RuntimeProfileRecord): RuntimeProfileFormValues {
  if (!profile) {
    return {
      name: '',
      providerKind: 'qoder_cloud',
      endpointUrl: QODER_CLOUD_DEFAULT_ENDPOINT,
      credentialToken: '',
      enabled: true,
    };
  }
  return {
    name: profile.name,
    providerKind: profile.provider_kind,
    endpointUrl: profile.endpoint_url,
    credentialToken: '',
    enabled: profile.status === 'active',
  };
}

export function runtimeProfileFormReady(values: RuntimeProfileFormValues, profile?: RuntimeProfileRecord): boolean {
  if (!values.name.trim()) {
    return false;
  }
  const isNative = (profile?.provider_kind ?? values.providerKind) === 'oma_native';
  if (!isNative && !values.endpointUrl.trim()) {
    return false;
  }
  if (!profile && !values.credentialToken.trim()) {
    return false;
  }
  return true;
}

export function buildRuntimeProfileCreateInput(values: RuntimeProfileFormValues): RuntimeProfileCreateInput {
  const status: RuntimeProfileStatus = values.enabled ? 'active' : 'disabled';
  const token = values.credentialToken.trim();
  return {
    name: values.name.trim(),
    provider_kind: values.providerKind,
    endpoint_url: values.endpointUrl.trim(),
    isolation_mode: 'shared_upstream',
    status,
    ...(token ? { credentials: { api_key: token } } : {}),
  };
}

export function buildRuntimeProfileUpdateInput(
  values: RuntimeProfileFormValues,
  profile: RuntimeProfileRecord,
): RuntimeProfileUpdateInput {
  const token = values.credentialToken.trim();
  const status: RuntimeProfileStatus = profile.builtin ? 'active' : values.enabled ? 'active' : 'disabled';
  return {
    expected_version: profile.version,
    name: values.name.trim(),
    ...(profile.builtin ? {} : { endpoint_url: values.endpointUrl.trim(), status }),
    ...(token ? { credentials: { api_key: token } } : {}),
  };
}

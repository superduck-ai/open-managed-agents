package admin

import (
	"encoding/json"
	"errors"
	"strings"
)

func validateAPIKeyStatus(status string, allowExpired bool) error {
	switch status {
	case "active", "inactive", "archived":
		return nil
	case "expired":
		if allowExpired {
			return nil
		}
	}
	return errors.New("invalid API key status")
}

func normalizeExternalKeyProviderConfig(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, errors.New("provider_config is required")
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("provider_config must be an object")
	}
	providerType, _ := payload["type"].(string)
	switch providerType {
	case "aws":
		if requiredString(payload, "kms_arn") == "" || requiredString(payload, "role_arn") == "" {
			return nil, errors.New("aws provider_config requires kms_arn and role_arn")
		}
		if requiredString(payload, "region") == "" {
			region := awsRegionFromKMSARN(requiredString(payload, "kms_arn"))
			if region == "" {
				return nil, errors.New("aws provider_config requires region or a valid kms_arn")
			}
			payload["region"] = region
		}
	case "gcp":
		if requiredString(payload, "key_name") == "" {
			return nil, errors.New("gcp provider_config requires key_name")
		}
	case "azure":
		if requiredString(payload, "key_name") == "" || requiredString(payload, "tenant_id") == "" || requiredString(payload, "vault_uri") == "" {
			return nil, errors.New("azure provider_config requires key_name, tenant_id, and vault_uri")
		}
	default:
		return nil, errors.New("provider_config.type must be aws, gcp, or azure")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func externalKeyProviderEqual(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return string(a) == string(b)
	}
	return jsonEqual(left, right)
}

func jsonEqual(left, right any) bool {
	encodedLeft, err := json.Marshal(left)
	if err != nil {
		return false
	}
	encodedRight, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(encodedLeft) == string(encodedRight)
}

func requiredString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func awsRegionFromKMSARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 4 || parts[0] != "arn" || parts[2] != "kms" {
		return ""
	}
	return parts[3]
}

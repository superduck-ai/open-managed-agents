package admin

import (
	"encoding/json"
	"errors"
	"strings"
)

type dataResidency struct {
	AllowedInferenceGeos any    `json:"allowed_inference_geos"`
	DefaultInferenceGeo  string `json:"default_inference_geo"`
	WorkspaceGeo         string `json:"workspace_geo"`
}

func defaultDataResidency() dataResidency {
	return dataResidency{
		AllowedInferenceGeos: "unrestricted",
		DefaultInferenceGeo:  "global",
		WorkspaceGeo:         "us",
	}
}

func dataResidencyFromRequest(req *dataResidencyRequest, current *dataResidency) (dataResidency, error) {
	next := defaultDataResidency()
	if current != nil {
		next = *current
	}
	if req == nil {
		return next, nil
	}
	if req.WorkspaceGeo != nil {
		next.WorkspaceGeo = strings.TrimSpace(*req.WorkspaceGeo)
	}
	if req.DefaultInferenceGeo != nil {
		next.DefaultInferenceGeo = strings.TrimSpace(*req.DefaultInferenceGeo)
	}
	if req.AllowedInferenceGeos != nil {
		allowed, err := normalizeAllowedInferenceGeos(req.AllowedInferenceGeos)
		if err != nil {
			return dataResidency{}, err
		}
		next.AllowedInferenceGeos = allowed
	}
	return validateDataResidency(next)
}

func decodeDataResidency(raw []byte) (dataResidency, error) {
	if len(raw) == 0 {
		return defaultDataResidency(), nil
	}
	var value dataResidency
	if err := json.Unmarshal(raw, &value); err != nil {
		return dataResidency{}, err
	}
	return validateDataResidency(value)
}

func encodeDataResidency(value dataResidency) ([]byte, error) {
	validated, err := validateDataResidency(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(validated)
}

func validateDataResidency(value dataResidency) (dataResidency, error) {
	if value.WorkspaceGeo == "" {
		value.WorkspaceGeo = "us"
	}
	if value.DefaultInferenceGeo == "" {
		value.DefaultInferenceGeo = "global"
	}
	if value.AllowedInferenceGeos == nil {
		value.AllowedInferenceGeos = "unrestricted"
	}
	allowed, err := normalizeAllowedInferenceGeos(value.AllowedInferenceGeos)
	if err != nil {
		return dataResidency{}, err
	}
	value.AllowedInferenceGeos = allowed
	if list, ok := allowed.([]string); ok && !containsString(list, value.DefaultInferenceGeo) {
		return dataResidency{}, errors.New("default_inference_geo must be included in allowed_inference_geos")
	}
	return value, nil
}

func normalizeAllowedInferenceGeos(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		if typed != "unrestricted" {
			return nil, errors.New("allowed_inference_geos must be unrestricted or an array of strings")
		}
		return "unrestricted", nil
	case []string:
		if len(typed) == 0 {
			return nil, errors.New("allowed_inference_geos must not be empty")
		}
		for _, item := range typed {
			if strings.TrimSpace(item) == "" {
				return nil, errors.New("allowed_inference_geos must contain non-empty strings")
			}
		}
		return typed, nil
	case []any:
		if len(typed) == 0 {
			return nil, errors.New("allowed_inference_geos must not be empty")
		}
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok || strings.TrimSpace(str) == "" {
				return nil, errors.New("allowed_inference_geos must contain non-empty strings")
			}
			result = append(result, strings.TrimSpace(str))
		}
		return result, nil
	default:
		return nil, errors.New("allowed_inference_geos must be unrestricted or an array of strings")
	}
}

func validateWorkspaceRole(role string, allowBilling bool) error {
	switch role {
	case "workspace_user", "workspace_developer", "workspace_restricted_developer", "workspace_admin":
		return nil
	case "workspace_billing":
		if allowBilling {
			return nil
		}
	}
	return errors.New("invalid workspace role")
}

func validateTags(tags map[string]string) error {
	for key := range tags {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return errors.New("tag keys must be non-empty")
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "anthropic") {
			return errors.New("tag keys may not begin with anthropic")
		}
	}
	return nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

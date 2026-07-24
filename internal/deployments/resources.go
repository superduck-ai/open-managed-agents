package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"

	"github.com/google/uuid"
)

type normalizedDeploymentResource struct {
	payload  map[string]any
	secret   map[string]any
	fileSpec *sessionresource.FileSpec
}

func (h *Handler) normalizeResources(
	r *http.Request,
	principal auth.Principal,
	raw json.RawMessage,
) (json.RawMessage, json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), json.RawMessage(`{}`), nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil, errors.New("resources must be an array")
	}
	if len(items) > 500 {
		return nil, nil, errors.New("resources may contain at most 500 entries")
	}

	resources := make([]map[string]any, 0, len(items))
	secrets := map[string]any{}
	fileSpecs := make([]sessionresource.FileSpec, 0, len(items))
	for index, fields := range items {
		resource, err := h.normalizeResource(r, principal, fields)
		if err != nil {
			return nil, nil, err
		}
		resources = append(resources, resource.payload)
		if resource.fileSpec != nil {
			fileSpecs = append(fileSpecs, *resource.fileSpec)
		}
		if resource.secret != nil {
			secrets[strconv.Itoa(index)] = resource.secret
		}
	}
	if err := sessionresource.ValidateFileSpecs(fileSpecs); err != nil {
		return nil, nil, err
	}

	resourcesRaw, err := httpapi.MarshalRaw(resources)
	if err != nil {
		return nil, nil, err
	}
	secretsRaw, err := httpapi.MarshalRaw(secrets)
	if err != nil {
		return nil, nil, err
	}
	return resourcesRaw, secretsRaw, nil
}

func (h *Handler) normalizeResource(
	r *http.Request,
	principal auth.Principal,
	fields map[string]json.RawMessage,
) (normalizedDeploymentResource, error) {
	resourceType, err := parseRequiredStringField(fields, "type")
	if err != nil {
		return normalizedDeploymentResource{}, err
	}
	resource := normalizedDeploymentResource{
		payload: map[string]any{"type": resourceType},
	}
	switch resourceType {
	case sessionresource.FileType:
		fileID, err := sessionresource.ParseFileID(fields)
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		if _, err := h.db.GetFile(r.Context(), principal.WorkspaceID, fileID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return normalizedDeploymentResource{}, fmt.Errorf("file not found: %s", fileID)
			}
			return normalizedDeploymentResource{}, err
		}
		fileSpec, err := sessionresource.NormalizeFileSpec(fileID, fields["source"], fields["mount_path"])
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload = fileSpec.PayloadFields("")
		resource.fileSpec = &fileSpec
	case "github_repository":
		repoURL, err := parseRequiredStringField(fields, "url")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		mountPath, err := optionalStringWithDefault(fields["mount_path"], defaultRepoMountPath(repoURL), "mount_path")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload["url"] = repoURL
		resource.payload["mount_path"] = mountPath
		if raw, ok := fields["checkout"]; ok && !httpapi.IsJSONNull(raw) {
			if err := validateCheckout(raw); err != nil {
				return normalizedDeploymentResource{}, err
			}
			resource.payload["checkout"] = agentsnapshot.RawJSONValue(raw, nil)
		}
		if raw, ok := fields["authorization_token"]; ok && !httpapi.IsJSONNull(raw) {
			token, err := parseRequiredRawString(raw, "authorization_token")
			if err != nil {
				return normalizedDeploymentResource{}, err
			}
			resource.secret = map[string]any{"authorization_token": token}
		}
	case "memory_store":
		memoryStoreID, err := parseRequiredStringField(fields, "memory_store_id")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceID, memoryStoreID)
		if err != nil {
			return normalizedDeploymentResource{}, resourceReferenceError{
				ResourceType: "memory_store",
				ResourceID:   memoryStoreID,
				Err:          err,
			}
		}
		if store.ArchivedAt != nil {
			return normalizedDeploymentResource{}, resourceReferenceError{
				ResourceType: "memory_store",
				ResourceID:   memoryStoreID,
				Err:          db.ErrInvalidState,
			}
		}
		resource.payload["memory_store_id"] = memoryStoreID
		access, err := optionalStringWithDefault(fields["access"], "read_write", "access")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		if access != "read_write" && access != "read_only" {
			return normalizedDeploymentResource{}, errors.New("access must be read_write or read_only")
		}
		resource.payload["access"] = access
		copyOptionalPayloadString(resource.payload, fields, "instructions")
	default:
		return normalizedDeploymentResource{}, errors.New(
			"resource type must be file, github_repository, or memory_store",
		)
	}
	return resource, nil
}

type resourceReferenceError struct {
	ResourceType string
	ResourceID   string
	Err          error
}

func (e resourceReferenceError) Error() string {
	return e.ResourceType + " reference failed: " + e.ResourceID
}

func (e resourceReferenceError) Unwrap() error {
	return e.Err
}

func sessionResourcesFromDeployment(
	deployment db.Deployment,
	now time.Time,
) ([]db.SessionResource, []db.SessionFileMount, error) {
	var configs []json.RawMessage
	if len(deployment.Resources) > 0 && !httpapi.IsJSONNull(deployment.Resources) {
		if err := json.Unmarshal(deployment.Resources, &configs); err != nil {
			return nil, nil, errors.New("stored resources are invalid")
		}
	}
	var secrets map[string]json.RawMessage
	if len(deployment.ResourceSecrets) > 0 && !httpapi.IsJSONNull(deployment.ResourceSecrets) {
		_ = json.Unmarshal(deployment.ResourceSecrets, &secrets)
	}

	resources := make([]db.SessionResource, 0, len(configs))
	fileMounts := make([]db.SessionFileMount, 0, len(configs))
	fileSpecs := make([]sessionresource.FileSpec, 0, len(configs))
	for index, configRaw := range configs {
		var config map[string]any
		if err := json.Unmarshal(configRaw, &config); err != nil {
			return nil, nil, errors.New("stored resources are invalid")
		}
		resourceType, _ := config["type"].(string)
		resourceID, err := ids.New("sesrsc_")
		if err != nil {
			return nil, nil, err
		}

		payload := cloneMap(config)
		if resourceType == sessionresource.FileType {
			fileSpec, err := sessionresource.ParseStoredFileSpec(configRaw)
			if err != nil {
				return nil, nil, err
			}
			payload = fileSpec.PayloadFields(resourceID)
			mount, err := fileSpec.SessionFileMount(resourceID)
			if err != nil {
				return nil, nil, err
			}
			fileSpecs = append(fileSpecs, fileSpec)
			fileMounts = append(fileMounts, mount)
		} else {
			payload["id"] = resourceID
			payload["type"] = resourceType
		}
		payloadRaw, err := httpapi.MarshalRaw(payload)
		if err != nil {
			return nil, nil, err
		}

		var secretRaw json.RawMessage
		if secrets != nil {
			secretRaw = secrets[strconv.Itoa(index)]
		}
		resources = append(resources, db.SessionResource{
			UUID:           uuid.NewString(),
			ExternalID:     resourceID,
			OrganizationID: deployment.OrganizationID,
			WorkspaceID:    deployment.WorkspaceID,
			ResourceType:   resourceType,
			Payload:        payloadRaw,
			SecretPayload:  secretRaw,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	if err := sessionresource.ValidateFileSpecs(fileSpecs); err != nil {
		return nil, nil, err
	}
	return resources, fileMounts, nil
}

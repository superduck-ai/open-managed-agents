package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type normalizedDeploymentResource struct {
	resourceType string
	payload      deploymentResourcePayload
	token        string
	referenceID  string
	mountPath    string
}

const deploymentMountPathDefaulted = "_oma_mount_path_defaulted"

type deploymentResourceRequest struct {
	Type               json.RawMessage `json:"type"`
	FileID             json.RawMessage `json:"file_id"`
	Source             json.RawMessage `json:"source"`
	MountPath          json.RawMessage `json:"mount_path"`
	URL                json.RawMessage `json:"url"`
	Checkout           json.RawMessage `json:"checkout"`
	AuthorizationToken json.RawMessage `json:"authorization_token"`
	MemoryStoreID      json.RawMessage `json:"memory_store_id"`
	Access             json.RawMessage `json:"access"`
	Instructions       json.RawMessage `json:"instructions"`
}

type deploymentResourcePayload struct {
	Type          string          `json:"type"`
	FileID        string          `json:"file_id,omitempty"`
	Source        string          `json:"source,omitempty"`
	MountPath     string          `json:"mount_path,omitempty"`
	URL           string          `json:"url,omitempty"`
	Checkout      json.RawMessage `json:"checkout,omitempty"`
	MemoryStoreID string          `json:"memory_store_id,omitempty"`
	Access        string          `json:"access,omitempty"`
	Instructions  *string         `json:"instructions,omitempty"`
}

func (h *Handler) normalizeResources(
	r *http.Request,
	principal auth.Principal,
	raw json.RawMessage,
) (json.RawMessage, json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), json.RawMessage(`{}`), nil
	}
	var items []deploymentResourceRequest
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil, errors.New("resources must be an array")
	}
	if len(items) > sessioncontract.MaxResources {
		return nil, nil, fmt.Errorf("resources may contain at most %d entries", sessioncontract.MaxResources)
	}

	resources := make([]deploymentResourcePayload, 0, len(items))
	resourceSecrets := map[string]json.RawMessage{}
	gitSpecs := make([]sessionresource.GitRepositorySpec, 0, len(items))
	fileMountPaths := make([]string, 0, len(items))
	for index, fields := range items {
		resource, err := h.normalizeResource(r, principal, fields)
		if err != nil {
			return nil, nil, err
		}
		resources = append(resources, resource.payload)
		if resource.resourceType == sessionresource.FileType {
			fileMountPaths = append(fileMountPaths, resource.mountPath)
		}
		if resource.resourceType == sessionresource.GitRepositoryType {
			gitSpecs = append(gitSpecs, sessionresource.GitRepositorySpec{URL: resource.payload.URL, MountPath: resource.mountPath})
			secret, err := sessionresource.EncryptGitToken(r.Context(), h.secretService, secrets.ResourceBinding{
				OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
			}, resource.token)
			if err != nil {
				return nil, nil, err
			}
			resourceSecrets[strconv.Itoa(index)] = secret
		}
	}
	if err := sessionresource.ValidateGitRepositoryConflicts(gitSpecs); err != nil {
		return nil, nil, err
	}
	if len(fileMountPaths) > sessionresource.MaxFileResources {
		return nil, nil, fmt.Errorf("at most %d managed-agent file resources are allowed", sessionresource.MaxFileResources)
	}
	if err := sandboxmount.ValidateFileMountPaths(fileMountPaths); err != nil {
		return nil, nil, err
	}

	resourcesRaw, err := httpapi.MarshalRaw(resources)
	if err != nil {
		return nil, nil, err
	}
	secretsRaw, err := httpapi.MarshalRaw(resourceSecrets)
	if err != nil {
		return nil, nil, err
	}
	return resourcesRaw, secretsRaw, nil
}

// normalizeResource keeps tokens separate from public configuration for subsequent encryption.
func (h *Handler) normalizeResource(
	r *http.Request,
	principal auth.Principal,
	fields deploymentResourceRequest,
) (normalizedDeploymentResource, error) {
	resourceType, err := parseRequiredRawString(fields.Type, "type")
	if err != nil {
		return normalizedDeploymentResource{}, err
	}
	resource := normalizedDeploymentResource{
		resourceType: resourceType,
		payload:      deploymentResourcePayload{Type: resourceType},
	}
	switch resourceType {
	case sessionresource.FileType:
		fileID, err := parseRequiredRawString(fields.FileID, "file_id")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		file, err := h.db.GetFile(r.Context(), principal.WorkspaceUUID, fileID)
		if err != nil {
			return normalizedDeploymentResource{}, resourceReferenceError{
				ResourceType: sessionresource.FileType,
				ResourceID:   fileID,
				Err:          err,
			}
		}
		if _, err := sessionresource.NormalizeFileSpec(
			fileID,
			file.Filename,
			fields.Source,
			fields.MountPath,
		); err != nil {
			return normalizedDeploymentResource{}, err
		}
		defaultMountPath := sandboxmount.DefaultFileMountPath(fileID, file.Filename)
		mountPath, err := optionalStringWithDefault(fields.MountPath, defaultMountPath, "mount_path")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload = deploymentResourcePayload{
			Type:      sessionresource.FileType,
			FileID:    fileID,
			Source:    sandboxmount.FileSource,
			MountPath: mountPath,
		}
		resource.referenceID = fileID
		resource.mountPath = mountPath
	case sessionresource.GitRepositoryType:
		spec, err := sessionresource.NormalizeGitRepositorySpec(fields.URL, fields.MountPath, fields.Checkout)
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload.URL = spec.URL
		resource.payload.MountPath = spec.MountPath
		resource.mountPath = spec.MountPath
		if spec.Checkout != nil {
			resource.payload.Checkout, err = json.Marshal(spec.Checkout)
			if err != nil {
				return normalizedDeploymentResource{}, err
			}
		}
		resource.token, err = sessionresource.ParseGitTokenInput(fields.AuthorizationToken)
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
	case "memory_store":
		memoryStoreID, err := parseRequiredRawString(fields.MemoryStoreID, "memory_store_id")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload.MemoryStoreID = memoryStoreID
		resource.referenceID = memoryStoreID
		access, err := optionalStringWithDefault(fields.Access, "read_write", "access")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		if access != "read_write" && access != "read_only" {
			return normalizedDeploymentResource{}, errors.New("access must be read_write or read_only")
		}
		resource.payload.Access = access
		if len(fields.Instructions) > 0 {
			if httpapi.IsJSONNull(fields.Instructions) {
				return normalizedDeploymentResource{}, errors.New("instructions is required")
			}
			var instructions string
			if err := json.Unmarshal(fields.Instructions, &instructions); err != nil {
				return normalizedDeploymentResource{}, errors.New("instructions must be a string")
			}
			if utf8.RuneCountInString(instructions) > 4096 {
				return normalizedDeploymentResource{}, errors.New("instructions must be at most 4096 characters")
			}
			resource.payload.Instructions = &instructions
		}
	default:
		return normalizedDeploymentResource{}, errors.New(
			"resource type must be file, github_repository, or memory_store",
		)
	}

	if resource.resourceType == "memory_store" {
		store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceUUID, resource.referenceID)
		if err != nil {
			return normalizedDeploymentResource{}, resourceReferenceError{
				ResourceType: "memory_store",
				ResourceID:   resource.referenceID,
				Err:          err,
			}
		}
		if store.ArchivedAt != nil {
			return normalizedDeploymentResource{}, resourceReferenceError{
				ResourceType: "memory_store",
				ResourceID:   resource.referenceID,
				Err:          db.ErrInvalidState,
			}
		}
	}
	return resource, nil
}

type deploymentResourceEnvelope struct {
	Type      string `json:"type"`
	FileID    string `json:"file_id"`
	MountPath string `json:"mount_path"`
}

func deploymentResourcesResponse(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return json.RawMessage(`[]`), nil
	}
	var resources []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &resources); err != nil {
		return nil, errors.New("stored deployment resources are invalid")
	}
	for _, resource := range resources {
		var envelope deploymentResourceEnvelope
		encoded, err := json.Marshal(resource)
		if err != nil || json.Unmarshal(encoded, &envelope) != nil {
			return nil, errors.New("stored deployment resource is invalid")
		}
		delete(resource, "authorization_token")
		delete(resource, "source")
		delete(resource, deploymentMountPathDefaulted)
		if envelope.Type != sessionresource.FileType || envelope.FileID == "" {
			continue
		}
		publicMountPath, err := sandboxmount.FileBackingPath(envelope.MountPath)
		if err != nil {
			return nil, errors.New("stored deployment file mount_path is invalid")
		}
		mountPath, err := json.Marshal(publicMountPath)
		if err != nil {
			return nil, err
		}
		resource["mount_path"] = mountPath
	}
	response, err := httpapi.MarshalRaw(resources)
	if err != nil {
		return nil, err
	}
	return response, nil
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
	sessionID string,
	deployment db.Deployment,
	now time.Time,
) ([]db.CreateSessionResourceInput, error) {
	var configs []json.RawMessage
	if len(deployment.Resources) > 0 && !httpapi.IsJSONNull(deployment.Resources) {
		if err := json.Unmarshal(deployment.Resources, &configs); err != nil {
			return nil, errors.New("stored resources are invalid")
		}
	}
	var resourceSecrets map[string]json.RawMessage
	if len(deployment.ResourceSecrets) > 0 && !httpapi.IsJSONNull(deployment.ResourceSecrets) {
		if err := json.Unmarshal(deployment.ResourceSecrets, &resourceSecrets); err != nil {
			return nil, errors.New("stored resource secrets are invalid")
		}
	}

	resources := make([]db.CreateSessionResourceInput, 0, len(configs))
	fileSpecs := make([]sessionresource.FileSpec, 0, len(configs))
	gitSpecs := make([]sessionresource.GitRepositorySpec, 0, len(configs))
	for index, configRaw := range configs {
		var config map[string]any
		if err := json.Unmarshal(configRaw, &config); err != nil || config == nil {
			return nil, errors.New("stored resources are invalid")
		}
		resourceType, _ := config["type"].(string)
		resourceID, err := ids.New("sesrsc_")
		if err != nil {
			return nil, markRunPreparationRetryable(err)
		}

		payload := maps.Clone(config)
		var fileMount *db.SessionFileMount
		if resourceType == sessionresource.FileType {
			fileSpec, err := sessionresource.ParseStoredFileSpec(configRaw)
			if err != nil {
				return nil, err
			}
			payload = fileSpec.PayloadFields(resourceID)
			binding, err := fileSpec.SessionFileBinding(resourceID)
			if err != nil {
				return nil, err
			}
			fileSpecs = append(fileSpecs, fileSpec)
			fileMount = &db.SessionFileMount{
				ResourceExternalID: binding.ResourceID,
				FileExternalID:     binding.FileID,
				Path:               binding.Path,
			}
		} else {
			payload["id"] = resourceID
			payload["type"] = resourceType
		}
		payloadRaw, err := httpapi.MarshalRaw(payload)
		if err != nil {
			return nil, err
		}

		var secretRaw json.RawMessage
		if resourceType == sessionresource.GitRepositoryType {
			spec, err := sessionresource.ParseStoredGitRepositorySpec(configRaw)
			if err != nil {
				return nil, err
			}
			gitSpecs = append(gitSpecs, spec)
			secretRaw = resourceSecrets[strconv.Itoa(index)]
			if _, err := sessionresource.ParseGitTokenEnvelope(secretRaw); err != nil {
				return nil, err
			}
		}
		resources = append(resources, db.CreateSessionResourceInput{
			Resource: db.SessionResource{
				UUID:              uuid.NewV4().String(),
				ExternalID:        resourceID,
				OrganizationUUID:  deployment.OrganizationUUID,
				WorkspaceUUID:     deployment.WorkspaceUUID,
				SessionExternalID: sessionID,
				ResourceType:      resourceType,
				Payload:           payloadRaw,
				SecretPayload:     secretRaw,
				CreatedAt:         now,
				UpdatedAt:         now,
			},
			FileMount: fileMount,
		})
	}
	if err := sessionresource.ValidateGitRepositoryConflicts(gitSpecs); err != nil {
		return nil, err
	}
	if err := sessionresource.ValidateFileSpecs(fileSpecs); err != nil {
		return nil, err
	}
	return resources, nil
}

package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
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
	Name               json.RawMessage `json:"name"`
	Description        json.RawMessage `json:"description"`
}

type deploymentResourcePayload struct {
	Type          string                       `json:"type"`
	FileID        string                       `json:"file_id,omitempty"`
	Source        string                       `json:"source,omitempty"`
	MountPath     string                       `json:"mount_path,omitempty"`
	URL           string                       `json:"url,omitempty"`
	Checkout      json.RawMessage              `json:"checkout,omitempty"`
	MemoryStoreID string                       `json:"memory_store_id,omitempty"`
	Access        sessionresource.MemoryAccess `json:"access,omitempty"`
	Instructions  *string                      `json:"instructions,omitempty"`
}

// normalizeResources validates resource references and group limits, keeping Git tokens
// encrypted and separate from the public configuration.
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
	memoryStores := sessionresource.NewMemoryAttachSet()
	for index, fields := range items {
		resource, err := h.normalizeResource(r, principal, fields)
		if err != nil {
			return nil, nil, err
		}
		resources = append(resources, resource.payload)
		if resource.resourceType == sessionresource.FileType {
			fileMountPaths = append(fileMountPaths, resource.mountPath)
		}
		if resource.resourceType == sessionresource.MemoryStoreType {
			if err := memoryStores.Claim(resource.referenceID); err != nil {
				return nil, nil, err
			}
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
	case sessionresource.MemoryStoreType:
		return h.normalizeMemoryStoreResource(r, principal, fields)
	default:
		return normalizedDeploymentResource{}, errors.New(
			"resource type must be file, github_repository, or memory_store",
		)
	}

	return resource, nil
}

func (h *Handler) normalizeMemoryStoreResource(
	r *http.Request,
	principal auth.Principal,
	fields deploymentResourceRequest,
) (normalizedDeploymentResource, error) {
	if err := sessionresource.RejectClientMemoryIdentityFields(fields.MountPath, fields.Name, fields.Description); err != nil {
		return normalizedDeploymentResource{}, err
	}
	memoryStoreID, err := parseRequiredRawString(fields.MemoryStoreID, "memory_store_id")
	if err != nil {
		return normalizedDeploymentResource{}, err
	}
	access, err := sessionresource.ParseMemoryAccess(fields.Access)
	if err != nil {
		return normalizedDeploymentResource{}, err
	}
	resource := normalizedDeploymentResource{
		resourceType: sessionresource.MemoryStoreType,
		payload: deploymentResourcePayload{
			Type:          sessionresource.MemoryStoreType,
			MemoryStoreID: memoryStoreID,
			Access:        access,
		},
		referenceID: memoryStoreID,
	}
	if len(fields.Instructions) > 0 {
		instructions, err := sessionresource.ParseMemoryInstructions(fields.Instructions)
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload.Instructions = &instructions
	}
	store, err := h.db.GetMemoryStore(r.Context(), principal.WorkspaceUUID, memoryStoreID)
	if err != nil {
		return normalizedDeploymentResource{}, resourceReferenceError{
			ResourceType: sessionresource.MemoryStoreType,
			ResourceID:   memoryStoreID,
			Err:          err,
		}
	}
	if store.ArchivedAt != nil {
		return normalizedDeploymentResource{}, resourceReferenceError{
			ResourceType: sessionresource.MemoryStoreType,
			ResourceID:   memoryStoreID,
			Err:          db.ErrInvalidState,
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
	memoryStores map[string]db.MemoryStore,
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
	attachSet := sessionresource.NewMemoryAttachSet()
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

		payload := config
		var fileMount *db.SessionFileMount
		switch resourceType {
		case sessionresource.FileType:
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
		case sessionresource.MemoryStoreType:
			payload, err = snapshotMemoryStorePayload(config, memoryStores, attachSet, resourceID)
			if err != nil {
				return nil, err
			}
		default:
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

func snapshotMemoryStorePayload(
	config map[string]any,
	stores map[string]db.MemoryStore,
	attachSet *sessionresource.MemoryAttachSet,
	resourceID string,
) (map[string]any, error) {
	memoryStoreID, ok := config["memory_store_id"].(string)
	if !ok || memoryStoreID == "" {
		return nil, errors.New("stored memory_store resource is missing memory_store_id")
	}
	store, ok := stores[memoryStoreID]
	if !ok {
		return nil, fmt.Errorf("memory store not found: %s", memoryStoreID)
	}
	access, err := storedMemoryAccess(config["access"])
	if err != nil {
		return nil, err
	}
	instructions, ok := config["instructions"].(string)
	if !ok && config["instructions"] != nil {
		return nil, errors.New("stored memory_store instructions must be a string")
	}
	slug, err := attachSet.Add(memoryStoreID, store.Name, store.ExternalID)
	if err != nil {
		return nil, err
	}
	snapshot := sessionresource.SnapshotMemoryStore(
		memoryStoreID,
		access,
		instructions,
		store.Name,
		store.Description,
		slug,
	)
	return snapshot.PayloadFields(resourceID), nil
}

// storedMemoryAccess reads the access field of a stored memory_store resource.
// Validation itself lives in sessionresource.NormalizeMemoryAccess; this only
// rejects a value that is present but not a JSON string.
func storedMemoryAccess(raw any) (sessionresource.MemoryAccess, error) {
	if raw == nil {
		return sessionresource.MemoryAccessReadWrite, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", sessionresource.ErrMemoryStoreAccess
	}
	return sessionresource.NormalizeMemoryAccess(value)
}

type deploymentResourceReader interface {
	GetFile(context.Context, string, string) (db.FileRecord, error)
	GetMemoryStoresByExternalIDs(context.Context, string, []string) ([]db.MemoryStore, error)
}

func validateDeploymentResources(ctx context.Context, database deploymentResourceReader, workspaceUUID string, resources []deploymentResourcePayload) (map[string]db.MemoryStore, *deploymentRunError, error) {
	var memoryStores map[string]db.MemoryStore
	for _, resource := range resources {
		switch resource.Type {
		case "file":
			if _, err := database.GetFile(ctx, workspaceUUID, resource.FileID); err != nil {
				failure, err := classifyReferenceFailure("file", err, false)
				return nil, failure, err
			}
		case "memory_store":
			// Load once, but check references in resource order so file and store
			// failures keep their original precedence.
			if memoryStores == nil {
				var err error
				memoryStores, err = loadDeploymentMemoryStores(ctx, database, workspaceUUID, resources)
				if err != nil {
					return nil, nil, err
				}
			}
			store, exists := memoryStores[resource.MemoryStoreID]
			if !exists {
				failure, err := classifyReferenceFailure("memory_store", db.ErrNotFound, false)
				return nil, failure, err
			}
			if store.ArchivedAt != nil {
				failure, err := classifyReferenceFailure("memory_store", nil, true)
				return nil, failure, err
			}
		}
	}
	return memoryStores, nil, nil
}

func loadDeploymentMemoryStores(
	ctx context.Context,
	database deploymentResourceReader,
	workspaceUUID string,
	resources []deploymentResourcePayload,
) (map[string]db.MemoryStore, error) {
	var storeIDs []string
	for _, resource := range resources {
		if resource.Type == sessionresource.MemoryStoreType && !slices.Contains(storeIDs, resource.MemoryStoreID) {
			storeIDs = append(storeIDs, resource.MemoryStoreID)
		}
	}
	stores := make(map[string]db.MemoryStore)
	if len(storeIDs) == 0 {
		return stores, nil
	}
	rows, err := database.GetMemoryStoresByExternalIDs(ctx, workspaceUUID, storeIDs)
	if err != nil {
		return nil, err
	}
	for _, store := range rows {
		stores[store.ExternalID] = store
	}
	return stores, nil
}

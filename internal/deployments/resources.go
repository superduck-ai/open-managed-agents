package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/open-managed-agents/internal/sessioneventfiles"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"

	"github.com/google/uuid"
)

type normalizedDeploymentResource struct {
	resourceType string
	payload      deploymentResourcePayload
	secret       *deploymentResourceSecret
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

type deploymentRunResourceReference struct {
	Type          string `json:"type"`
	FileID        string `json:"file_id"`
	MemoryStoreID string `json:"memory_store_id"`
}

type deploymentResourceSecret struct {
	AuthorizationToken string `json:"authorization_token"`
}

// normalizeResources 将 Deployment 请求中的整组 resources 规范化，并把普通配置与秘密分开。
//
// 未传 resources 或值为 null 时，函数返回规范的空数组和空 secrets 对象。其他输入必须是
// JSON 数组，且总资源数不能超过 sessioncontract.MaxResources。它按原始顺序调用
// normalizeResource 处理每一项，收集可公开保存的 payload、可选 secret 和 File mount_path；
// 任意一项失败都会终止整组处理。
//
// secret 使用资源在原数组中的下标作为 key。这样普通 resources 中不会包含 GitHub
// authorization_token，Deployment 运行时 sessionResourcesFromDeployment 仍可用相同下标
// 将秘密匹配回对应的 Session resource。File mount_path 在全部项目处理完成后统一校验，
// 确保路径没有重复或祖先/后代冲突，且 File 数量不超过 sessionresource.MaxFileResources
// （当前等于 MaxResources）。这样 Create/Update 与 run 时 materialize Session 的限额一致。
//
// 例如：
//   - 输入一个 File 和一个带 Token 的 GitHub resource，resourcesRaw 保存规范化后的两条
//     普通配置，secretsRaw 形如 {"1":{"authorization_token":"github-secret"}}。
//   - 输入 null，返回 resourcesRaw=[]、secretsRaw={}，不会产生错误。
//   - 两个 File 分别挂到 /workspace/data 和 /workspace/data/config.json，聚合路径校验失败，
//     函数返回错误，不生成可保存的结果。
//
// 成功时返回两段 JSON：resourcesRaw 用于普通资源配置，secretsRaw 用于敏感配置。主要错误
// 包括输入不是数组、超过数量限制、单项字段或引用无效、File 路径冲突以及 JSON 编码失败。
// 函数本身不写数据库、不创建 Session、不修改 Filestore，也不执行挂载；normalizeResource
// 只会按 principal 的 Workspace 读取并校验 File 或 Memory Store 引用，因此这里没有事务或锁。
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
	secrets := map[string]deploymentResourceSecret{}
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
		if resource.secret != nil {
			secrets[strconv.Itoa(index)] = *resource.secret
		}
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
	secretsRaw, err := httpapi.MarshalRaw(secrets)
	if err != nil {
		return nil, nil, err
	}
	return resourcesRaw, secretsRaw, nil
}

// normalizeResource 将 Deployment 请求中的一条原始资源配置校验并转换为统一的存储格式。
//
// 函数先读取必填的 type，只接受 file、github_repository 和 memory_store。它只把已知
// 字段写入 payload，并补充各类型的默认值。GitHub authorization_token 会单独放入
// secret，避免进入普通资源配置。File 和 Memory Store 引用都按 principal.WorkspaceUUID
// 查询，防止 Deployment 引用其他 Workspace 的对象；已归档的 Memory Store 也会被拒绝。
//
// File resource 会固定为 source=/uploads，并生成经过校验的 mount_path。当前函数只处理
// 单条资源；File 数量、重复 mount_path 和祖先/后代路径冲突由外层 normalizeResources
// 收集全部路径后统一校验。Deployment 真正运行时，sessionResourcesFromDeployment
// 才会为这些模板生成 sesrsc_ ID，并创建 Session resource 和对应的 File binding。
//
// 例如：
//   - 输入 {"type":"file","file_id":"file_123","mount_path":"/workspace/context.md"}，
//     返回的 payload 会包含固定的 source=/uploads 和相同的 mount_path。
//   - 输入带 authorization_token 的 github_repository，普通 payload 只保存仓库配置，
//     Token 单独返回在 secret 中。
//   - file_id 属于其他 Workspace，或 memory_store 已归档，函数返回引用或状态错误，
//     不生成可保存的资源。
//
// 成功时返回规范化的 payload、可选 secret 和 File mount_path。字段格式错误、未知类型、
// 引用不存在、跨 Workspace 引用或无效状态都会返回错误。函数只执行必要的数据库读取，
// 不开启事务、不加显式锁，也不会写数据库、创建 Session、修改 Filestore 或执行挂载。
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
	case "github_repository":
		repoURL, err := parseRequiredRawString(fields.URL, "url")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		mountPath, err := optionalStringWithDefault(
			fields.MountPath,
			sessionresource.DefaultGitHubRepositoryMountPath(repoURL),
			"mount_path",
		)
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.payload.URL = repoURL
		resource.payload.MountPath = mountPath
		if len(fields.Checkout) > 0 && !httpapi.IsJSONNull(fields.Checkout) {
			if err := validateCheckout(fields.Checkout); err != nil {
				return normalizedDeploymentResource{}, err
			}
			resource.payload.Checkout = append(json.RawMessage(nil), fields.Checkout...)
		}
		token, err := parseRequiredRawString(fields.AuthorizationToken, "authorization_token")
		if err != nil {
			return normalizedDeploymentResource{}, err
		}
		resource.secret = &deploymentResourceSecret{AuthorizationToken: token}
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

func parseDeploymentRunResourceReferences(raw json.RawMessage) ([]deploymentRunResourceReference, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var references []deploymentRunResourceReference
	if err := json.Unmarshal(raw, &references); err != nil {
		return nil, errors.New("stored resources are invalid")
	}
	for _, reference := range references {
		switch reference.Type {
		case sessionresource.FileType:
			fileID := strings.TrimSpace(reference.FileID)
			if fileID == "" || fileID != reference.FileID || reference.MemoryStoreID != "" {
				return nil, errors.New("stored file resource reference is invalid")
			}
		case "memory_store":
			memoryStoreID := strings.TrimSpace(reference.MemoryStoreID)
			if memoryStoreID == "" || memoryStoreID != reference.MemoryStoreID || reference.FileID != "" {
				return nil, errors.New("stored memory store resource reference is invalid")
			}
		case "github_repository":
			if reference.FileID != "" || reference.MemoryStoreID != "" {
				return nil, errors.New("stored GitHub resource reference is invalid")
			}
		default:
			return nil, errors.New("stored resource type is invalid")
		}
	}
	return references, nil
}

func sessionResourcesFromDeployment(
	deployment db.Deployment,
	now time.Time,
) ([]db.CreateSessionResourceInput, error) {
	var configs []json.RawMessage
	if len(deployment.Resources) > 0 && !httpapi.IsJSONNull(deployment.Resources) {
		if err := json.Unmarshal(deployment.Resources, &configs); err != nil {
			return nil, errors.New("stored resources are invalid")
		}
	}
	var secrets map[string]json.RawMessage
	if len(deployment.ResourceSecrets) > 0 && !httpapi.IsJSONNull(deployment.ResourceSecrets) {
		if err := json.Unmarshal(deployment.ResourceSecrets, &secrets); err != nil {
			return nil, errors.New("stored resource secrets are invalid")
		}
	}

	resources := make([]db.CreateSessionResourceInput, 0, len(configs))
	fileSpecs := make([]sessionresource.FileSpec, 0, len(configs))
	for index, configRaw := range configs {
		var config map[string]any
		if err := json.Unmarshal(configRaw, &config); err != nil || config == nil {
			return nil, errors.New("stored resources are invalid")
		}
		resourceType, _ := config["type"].(string)
		resourceID, err := ids.New("sesrsc_")
		if err != nil {
			return nil, err
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
		if secrets != nil {
			secretRaw = secrets[strconv.Itoa(index)]
		}
		resources = append(resources, db.CreateSessionResourceInput{
			Resource: db.SessionResource{
				UUID:             uuid.NewString(),
				ExternalID:       resourceID,
				OrganizationUUID: deployment.OrganizationUUID,
				WorkspaceUUID:    deployment.WorkspaceUUID,
				ResourceType:     resourceType,
				Payload:          payloadRaw,
				SecretPayload:    secretRaw,
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			FileMount: fileMount,
		})
	}
	if err := sessionresource.ValidateFileSpecs(fileSpecs); err != nil {
		return nil, err
	}
	return resources, nil
}

func deploymentEventFileBindings(
	resources []db.CreateSessionResourceInput,
	filesByID map[string]db.FileRecord,
) ([]sessioneventfiles.Binding, error) {
	bindings := make([]sessioneventfiles.Binding, 0, len(resources))
	for _, resource := range resources {
		if resource.FileMount == nil {
			continue
		}
		file, ok := filesByID[resource.FileMount.FileExternalID]
		if !ok {
			return nil, fmt.Errorf("file not found: %s", resource.FileMount.FileExternalID)
		}
		bindings = append(bindings, sessioneventfiles.Binding{
			FileID:   file.ExternalID,
			Path:     resource.FileMount.Path,
			MimeType: file.MimeType,
		})
	}
	return bindings, nil
}

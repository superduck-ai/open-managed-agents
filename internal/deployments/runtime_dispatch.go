package deployments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) WithRuntimeResolver(resolver *agentruntime.Resolver) *Handler {
	h.runtimeResolver = resolver
	return h
}

func (h *RunsHandler) WithRuntimeResolver(resolver *agentruntime.Resolver) *RunsHandler {
	h.runtimeResolver = resolver
	return h
}

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func resolveExternalDeploymentRuntime(
	ctx context.Context,
	resolver *agentruntime.Resolver,
	principal auth.Principal,
) (agentruntime.Runtime, agentruntime.DeploymentRuntime, agentruntime.Scope, error) {
	if resolver == nil {
		return nil, nil, agentruntime.Scope{}, deploymentRuntimeUnavailable(nil)
	}
	rt, profile, err := resolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	depRT, ok := rt.(agentruntime.DeploymentRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, deploymentRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, depRT, scope, nil
}

func (h *Handler) createExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	body, err := httpapi.DecodeObjectBodyAs[deploymentMutationRequest](w, r, maxDeploymentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	for _, field := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "metadata", raw: body.Metadata},
		{name: "resources", raw: body.Resources},
		{name: "vault_ids", raw: body.VaultIDs},
	} {
		if err := rejectNullField(field.raw, field.name); err != nil {
			return invalidRequest(err)
		}
	}
	agentID, agentVersion, err := parseAgentReference(body.Agent)
	if err != nil {
		return invalidRequest(err)
	}
	environmentID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
	if err != nil {
		return invalidRequest(err)
	}
	name, err := parseRequiredRawString(body.Name, "name")
	if err != nil {
		return invalidRequest(err)
	}
	descriptionPtr, err := parseNullableString(body.Description, "description")
	if err != nil {
		return invalidRequest(err)
	}
	description := ""
	if descriptionPtr != nil {
		description = *descriptionPtr
	}
	metadata, err := parseExternalDeploymentMetadata(body.Metadata)
	if err != nil {
		return invalidRequest(err)
	}
	initialEvents, err := parseExternalInitialEvents(body.InitialEvents)
	if err != nil {
		return invalidRequest(err)
	}
	schedule, err := parseExternalSchedule(body.Schedule)
	if err != nil {
		return invalidRequest(err)
	}
	resources, err := parseExternalDeploymentResources(body.Resources)
	if err != nil {
		return invalidRequest(err)
	}
	vaultIDs, err := parseExternalVaultIDs(body.VaultIDs)
	if err != nil {
		return invalidRequest(err)
	}
	budget, _, err := parseExternalDeploymentBudget(body.Budget)
	if err != nil {
		return invalidRequest(err)
	}
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	created, err := depRT.CreateDeployment(r.Context(), scope, agentruntime.CreateDeploymentInput{
		AgentID:       agentID,
		AgentVersion:  agentVersion,
		EnvironmentID: environmentID,
		Name:          name,
		Description:   description,
		Metadata:      metadata,
		InitialEvents: initialEvents,
		Resources:     resources,
		VaultIDs:      vaultIDs,
		Budget:        budget,
		Schedule:      schedule,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalDeployment(created, time.Now().UTC()))
	return nil
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodeExternalOffsetCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	includeArchived, err := parseOptionalBool(r, "include_archived")
	if err != nil {
		return invalidRequest(err)
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "active" && status != "paused" {
		return invalidRequest(errors.New("status must be active or paused"))
	}
	if status != "" && r.URL.Query().Has("include_archived") {
		return invalidRequest(errors.New("status cannot be combined with include_archived"))
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := depRT.ListDeployments(r.Context(), scope, agentruntime.ListDeploymentsQuery{
		IncludeArchived: includeArchived,
		AgentID:         strings.TrimSpace(r.URL.Query().Get("agent_id")),
		Status:          status,
		CreatedAtGTE:    createdAtGTE,
		CreatedAtLTE:    createdAtLTE,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	data := make([]deploymentResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromExternalDeployment(item, now))
	}
	var nextPage *string
	if page.HasMore {
		encoded := encodeExternalOffsetCursor(offset + len(data))
		nextPage = &encoded
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[deploymentResponse]{Data: data, NextPage: nextPage})
	return nil
}

func (h *Handler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, deploymentID string) error {
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	item, err := depRT.GetDeployment(r.Context(), scope, deploymentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalDeployment(item, time.Now().UTC()))
	return nil
}

func (h *Handler) updateExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, deploymentID string) error {
	body, err := httpapi.DecodeObjectBodyAs[deploymentMutationRequest](w, r, maxDeploymentBodySize)
	if err != nil {
		return invalidRequest(err)
	}
	input, err := buildExternalUpdateDeploymentInput(body)
	if err != nil {
		return invalidRequest(err)
	}
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	if len(body.Metadata) > 0 {
		current, err := depRT.GetDeployment(r.Context(), scope, deploymentID)
		if err != nil {
			return err
		}
		currentRaw, _ := json.Marshal(current.Metadata)
		patchedRaw, err := patchDeploymentMetadata(currentRaw, body.Metadata)
		if err != nil {
			return invalidRequest(err)
		}
		var patched map[string]string
		if err := json.Unmarshal(patchedRaw, &patched); err != nil {
			return invalidRequest(err)
		}
		input.Metadata = patched
		input.MetadataSet = true
	}
	updated, err := depRT.UpdateDeployment(r.Context(), scope, deploymentID, input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalDeployment(updated, time.Now().UTC()))
	return nil
}

func (h *Handler) archiveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, deploymentID string) error {
	return h.mutateExternalState(w, r, principal, deploymentID, func(ctx context.Context, depRT agentruntime.DeploymentRuntime, scope agentruntime.Scope, id string) (agentruntime.Deployment, error) {
		return depRT.ArchiveDeployment(ctx, scope, id)
	})
}

func (h *Handler) pauseExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, deploymentID string) error {
	return h.mutateExternalState(w, r, principal, deploymentID, func(ctx context.Context, depRT agentruntime.DeploymentRuntime, scope agentruntime.Scope, id string) (agentruntime.Deployment, error) {
		return depRT.PauseDeployment(ctx, scope, id)
	})
}

func (h *Handler) unpauseExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, deploymentID string) error {
	return h.mutateExternalState(w, r, principal, deploymentID, func(ctx context.Context, depRT agentruntime.DeploymentRuntime, scope agentruntime.Scope, id string) (agentruntime.Deployment, error) {
		return depRT.UnpauseDeployment(ctx, scope, id)
	})
}

func (h *Handler) mutateExternalState(
	w http.ResponseWriter,
	r *http.Request,
	principal auth.Principal,
	deploymentID string,
	action func(context.Context, agentruntime.DeploymentRuntime, agentruntime.Scope, string) (agentruntime.Deployment, error),
) error {
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	item, err := action(r.Context(), depRT, scope, deploymentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalDeployment(item, time.Now().UTC()))
	return nil
}

func (h *Handler) runExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, deploymentID string) error {
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	run, err := depRT.RunDeployment(r.Context(), scope, deploymentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalRun(run))
	return nil
}

func (h *RunsHandler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, runID string) error {
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	run, err := depRT.GetDeploymentRun(r.Context(), scope, runID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromExternalRun(run))
	return nil
}

func (h *RunsHandler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodeExternalOffsetCursor(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	triggerType := strings.TrimSpace(r.URL.Query().Get("trigger_type"))
	if triggerType != "" && triggerType != "manual" && triggerType != "schedule" {
		return invalidRequest(errors.New("trigger_type must be manual or schedule"))
	}
	hasError, err := parseOptionalBoolPointer(r, "has_error")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGT, err := httpapi.ParseOptionalTime(r, "created_at[gt]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtGTE, err := httpapi.ParseOptionalTime(r, "created_at[gte]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLT, err := httpapi.ParseOptionalTime(r, "created_at[lt]")
	if err != nil {
		return invalidRequest(err)
	}
	createdAtLTE, err := httpapi.ParseOptionalTime(r, "created_at[lte]")
	if err != nil {
		return invalidRequest(err)
	}
	rt, depRT, scope, err := resolveExternalDeploymentRuntime(r.Context(), h.runtimeResolver, principal)
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()
	page, err := depRT.ListDeploymentRuns(r.Context(), scope, agentruntime.ListDeploymentRunsQuery{
		DeploymentID: strings.TrimSpace(r.URL.Query().Get("deployment_id")),
		TriggerType:  triggerType,
		HasError:     hasError,
		CreatedAtGT:  createdAtGT,
		CreatedAtGTE: createdAtGTE,
		CreatedAtLT:  createdAtLT,
		CreatedAtLTE: createdAtLTE,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		return err
	}
	data := make([]deploymentRunResponse, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, responseFromExternalRun(item))
	}
	var nextPage *string
	if page.HasMore {
		encoded := encodeExternalOffsetCursor(offset + len(data))
		nextPage = &encoded
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[deploymentRunResponse]{Data: data, NextPage: nextPage})
	return nil
}

func buildExternalUpdateDeploymentInput(body *deploymentMutationRequest) (agentruntime.UpdateDeploymentInput, error) {
	var input agentruntime.UpdateDeploymentInput
	if len(body.Agent) > 0 {
		agentID, agentVersion, err := parseAgentReference(body.Agent)
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.AgentID = &agentID
		input.AgentVersion = agentVersion
	}
	if len(body.EnvironmentID) > 0 {
		envID, err := parseRequiredRawString(body.EnvironmentID, "environment_id")
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.EnvironmentID = &envID
	}
	if len(body.Name) > 0 {
		name, err := parseRequiredRawString(body.Name, "name")
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.Name = &name
	}
	if len(body.Description) > 0 {
		desc, err := nullableStringFromRaw(body.Description, "description")
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		if desc == nil || *desc == "" {
			input.ClearDesc = true
		} else {
			input.Description = desc
		}
	}
	if len(body.InitialEvents) > 0 {
		events, err := parseExternalInitialEvents(body.InitialEvents)
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.InitialEvents = events
		input.EventsSet = true
	}
	if len(body.Resources) > 0 {
		resources, err := parseExternalDeploymentResources(body.Resources)
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.Resources = resources
		input.ResourcesSet = true
	}
	if len(body.VaultIDs) > 0 {
		vaultIDs, err := parseExternalVaultIDs(body.VaultIDs)
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.VaultIDs = vaultIDs
		input.VaultIDsSet = true
	}
	if len(body.Budget) > 0 {
		budget, set, err := parseExternalDeploymentBudget(body.Budget)
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.Budget = budget
		input.BudgetSet = set
	}
	if body.Schedule != nil {
		schedule, err := parseExternalSchedule(body.Schedule)
		if err != nil {
			return agentruntime.UpdateDeploymentInput{}, err
		}
		input.Schedule = schedule
		input.ScheduleSet = true
	}
	return input, nil
}

func parseExternalDeploymentMetadata(raw json.RawMessage) (map[string]string, error) {
	normalized, err := httpapi.NormalizeMetadata(jsonx.Default(raw, `{}`), validateMetadataEntries)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	if err := json.Unmarshal(normalized, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseExternalInitialEvents(raw json.RawMessage) ([]json.RawMessage, error) {
	normalized, err := normalizeInitialEvents(raw)
	if err != nil {
		return nil, err
	}
	var events []json.RawMessage
	if err := json.Unmarshal(normalized, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func parseExternalSchedule(raw json.RawMessage) (*agentruntime.DeploymentSchedule, error) {
	normalized, err := normalizeOptionalSchedule(raw)
	if err != nil || len(normalized) == 0 {
		return nil, err
	}
	var sched deploymentjobs.Schedule
	if err := json.Unmarshal(normalized, &sched); err != nil {
		return nil, err
	}
	return &agentruntime.DeploymentSchedule{
		Type:       sched.Type,
		Expression: sched.Expression,
		Timezone:   sched.Timezone,
	}, nil
}

func parseExternalDeploymentResources(raw json.RawMessage) ([]agentruntime.AddSessionResourceInput, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var items []deploymentResourceRequest
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.New("resources must be an array")
	}
	out := make([]agentruntime.AddSessionResourceInput, 0, len(items))
	for _, item := range items {
		resourceType, err := parseRequiredRawString(item.Type, "type")
		if err != nil {
			return nil, err
		}
		var fileID string
		if len(item.FileID) > 0 && !httpapi.IsJSONNull(item.FileID) {
			if err := json.Unmarshal(item.FileID, &fileID); err != nil {
				return nil, errors.New("file_id must be a string")
			}
		}
		var memoryStoreID string
		if len(item.MemoryStoreID) > 0 && !httpapi.IsJSONNull(item.MemoryStoreID) {
			if err := json.Unmarshal(item.MemoryStoreID, &memoryStoreID); err != nil {
				return nil, errors.New("memory_store_id must be a string")
			}
		}
		var mountPath string
		if len(item.MountPath) > 0 && !httpapi.IsJSONNull(item.MountPath) {
			if err := json.Unmarshal(item.MountPath, &mountPath); err != nil {
				return nil, errors.New("mount_path must be a string")
			}
		}
		out = append(out, agentruntime.AddSessionResourceInput{
			Type:          resourceType,
			FileID:        fileID,
			MemoryStoreID: memoryStoreID,
			MountPath:     mountPath,
			Payload:       marshalExternalDeploymentResource(&item),
		})
	}
	return out, nil
}

func marshalExternalDeploymentResource(body *deploymentResourceRequest) json.RawMessage {
	fields := make(map[string]json.RawMessage)
	setField := func(name string, raw json.RawMessage) {
		if len(raw) > 0 {
			fields[name] = raw
		}
	}
	setField("authorization_token", body.AuthorizationToken)
	setField("access", body.Access)
	setField("checkout", body.Checkout)
	setField("description", body.Description)
	setField("file_id", body.FileID)
	setField("instructions", body.Instructions)
	setField("memory_store_id", body.MemoryStoreID)
	setField("mount_path", body.MountPath)
	setField("name", body.Name)
	setField("source", body.Source)
	setField("type", body.Type)
	setField("url", body.URL)
	encoded, _ := json.Marshal(fields)
	return json.RawMessage(encoded)
}

func parseExternalVaultIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	var idsList []string
	if err := json.Unmarshal(raw, &idsList); err != nil {
		return nil, errors.New("vault_ids must be an array of strings")
	}
	return idsList, nil
}

func parseExternalDeploymentBudget(raw json.RawMessage) (*agentruntime.SessionBudget, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, true, nil
	}
	var payload struct {
		Type          string `json:"type"`
		MaxCreditCost string `json:"max_credit_cost"`
		MaxCostUSD    string `json:"max_cost_usd"`
		MaxListCost   *struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"max_list_cost"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, false, errors.New("budget must be an object")
	}
	if payload.Type != "limit" {
		return nil, false, errors.New("budget.type must be limit")
	}
	if payload.MaxListCost != nil {
		return &agentruntime.SessionBudget{
			Mode:       agentruntime.BudgetModeUSD,
			MaxCostUSD: strings.TrimSpace(payload.MaxListCost.Amount),
		}, true, nil
	}
	if strings.TrimSpace(payload.MaxCostUSD) != "" {
		return &agentruntime.SessionBudget{
			Mode:       agentruntime.BudgetModeUSD,
			MaxCostUSD: strings.TrimSpace(payload.MaxCostUSD),
		}, true, nil
	}
	if strings.TrimSpace(payload.MaxCreditCost) == "" {
		return nil, false, errors.New("budget.max_credit_cost is required")
	}
	return &agentruntime.SessionBudget{
		Mode:          agentruntime.BudgetModeCredits,
		MaxCreditCost: strings.TrimSpace(payload.MaxCreditCost),
	}, true, nil
}

func encodeExternalOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(offset)))
}

func decodeExternalOffsetCursor(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return 0, errors.New("page cursor is invalid")
	}
	value, ok := strings.CutPrefix(string(decoded), "offset:")
	if !ok {
		return 0, errors.New("page cursor is invalid")
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, errors.New("page cursor is invalid")
	}
	return n, nil
}

func responseFromExternalDeployment(item agentruntime.Deployment, now time.Time) deploymentResponse {
	metadata := item.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	vaultIDs := item.VaultIDs
	if vaultIDs == nil {
		vaultIDs = []string{}
	}
	initialEvents := make([]deploymentInitialEvent, 0, len(item.InitialEvents))
	for _, raw := range item.InitialEvents {
		var evt deploymentInitialEvent
		if err := json.Unmarshal(raw, &evt); err == nil {
			initialEvents = append(initialEvents, evt)
		}
	}
	resourcesRaw := json.RawMessage(`[]`)
	if len(item.Resources) > 0 {
		if encoded, err := json.Marshal(item.Resources); err == nil {
			resourcesRaw = encoded
		}
	}
	budgetRaw := json.RawMessage(`null`)
	if item.Budget != nil {
		budgetObj := map[string]any{"type": "limit"}
		if item.Budget.MaxCreditCost != "" {
			budgetObj["max_credit_cost"] = item.Budget.MaxCreditCost
		}
		if encoded, err := json.Marshal(budgetObj); err == nil {
			budgetRaw = encoded
		}
	}
	var pausedReason *deploymentPausedReason
	if item.PausedReason != nil {
		pausedReason = &deploymentPausedReason{Type: item.PausedReason.Type}
		if item.PausedReason.Error != nil {
			pausedReason.Error = &deploymentRunError{
				Type:    item.PausedReason.Error.Type,
				Message: item.PausedReason.Error.Message,
			}
		}
	}
	var schedule *deploymentScheduleResponse
	if item.Schedule != nil {
		schedule = &deploymentScheduleResponse{
			Schedule: deploymentjobs.Schedule{
				Type:       item.Schedule.Type,
				Expression: item.Schedule.Expression,
				Timezone:   item.Schedule.Timezone,
			},
			LastRunAt:      item.Schedule.LastRunAt,
			UpcomingRunsAt: item.Schedule.UpcomingRunsAt,
		}
		if schedule.UpcomingRunsAt == nil {
			if rawSched, err := json.Marshal(schedule.Schedule); err == nil {
				var lastRun *time.Time
				if item.Schedule.LastRunAt != nil && *item.Schedule.LastRunAt != "" {
					if parsed, parseErr := time.Parse(time.RFC3339, *item.Schedule.LastRunAt); parseErr == nil {
						lastRun = &parsed
					}
				}
				if computed := scheduleResponse(rawSched, lastRun, now, item.ArchivedAt != nil || item.Status != "active"); computed != nil {
					schedule.UpcomingRunsAt = computed.UpcomingRunsAt
				}
			}
		}
		if schedule.UpcomingRunsAt == nil {
			schedule.UpcomingRunsAt = []string{}
		}
	}
	return deploymentResponse{
		ID:            item.ID,
		Agent:         agentReference(item.Agent.ID, item.Agent.Version),
		ArchivedAt:    httpapi.OptionalTime(item.ArchivedAt),
		Budget:        budgetRaw,
		CreatedAt:     httpapi.FormatTime(item.CreatedAt),
		Description:   item.Description,
		EnvironmentID: item.EnvironmentID,
		InitialEvents: initialEvents,
		Metadata:      metadata,
		Name:          item.Name,
		PausedReason:  pausedReason,
		Resources:     resourcesRaw,
		Schedule:      schedule,
		Status:        item.Status,
		Type:          "deployment",
		UpdatedAt:     httpapi.FormatTime(item.UpdatedAt),
		VaultIDs:      vaultIDs,
	}
}

func responseFromExternalRun(run agentruntime.DeploymentRun) deploymentRunResponse {
	var runErr *deploymentRunError
	if run.Error != nil {
		runErr = &deploymentRunError{
			Type:    run.Error.Type,
			Message: run.Error.Message,
		}
	}
	return deploymentRunResponse{
		ID:           run.ID,
		Agent:        agentReference(run.Agent.ID, run.Agent.Version),
		CreatedAt:    httpapi.FormatTime(run.CreatedAt),
		DeploymentID: run.DeploymentID,
		Error:        runErr,
		SessionID:    run.SessionID,
		TriggerContext: deploymentRunTriggerContext{
			Type:        run.TriggerContext.Type,
			ScheduledAt: run.TriggerContext.ScheduledAt,
		},
		Type: "deployment_run",
	}
}

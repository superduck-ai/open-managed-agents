package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const deploymentHydrateConcurrency = 5

type qoderDeployment struct {
	ID            string                               `json:"id"`
	Agent         qoderAgentField                      `json:"agent"`
	AgentID       string                               `json:"agent_id"`
	AgentVersion  int64                                `json:"agent_version"`
	EnvironmentID string                               `json:"environment_id"`
	Name          string                               `json:"name"`
	Description   string                               `json:"description"`
	Metadata      map[string]string                    `json:"metadata"`
	InitialEvents []json.RawMessage                    `json:"initial_events"`
	Resources     []json.RawMessage                    `json:"resources"`
	VaultIDs      []string                             `json:"vault_ids"`
	Budget        *qoderSessionBudget                  `json:"budget"`
	Schedule      *agentruntime.DeploymentSchedule     `json:"schedule"`
	Status        string                               `json:"status"`
	PausedReason  *agentruntime.DeploymentPausedReason `json:"paused_reason"`
	ArchivedAt    *time.Time                           `json:"archived_at"`
	CreatedAt     time.Time                            `json:"created_at"`
	UpdatedAt     time.Time                            `json:"updated_at"`
}

type qoderCreateDeploymentRequest struct {
	Agent         any                              `json:"agent"`
	EnvironmentID string                           `json:"environment_id"`
	Name          string                           `json:"name"`
	Description   string                           `json:"description,omitempty"`
	Metadata      map[string]string                `json:"metadata,omitempty"`
	InitialEvents []json.RawMessage                `json:"initial_events,omitempty"`
	Resources     []json.RawMessage                `json:"resources,omitempty"`
	VaultIDs      []string                         `json:"vault_ids,omitempty"`
	Budget        *qoderSessionBudget              `json:"budget,omitempty"`
	Schedule      *agentruntime.DeploymentSchedule `json:"schedule,omitempty"`
}

type qoderDeploymentRun struct {
	ID             string                                   `json:"id"`
	DeploymentID   string                                   `json:"deployment_id"`
	Agent          qoderAgentField                          `json:"agent"`
	AgentID        string                                   `json:"agent_id"`
	AgentVersion   int64                                    `json:"agent_version"`
	SessionID      *string                                  `json:"session_id"`
	Error          *agentruntime.DeploymentRunError         `json:"error"`
	TriggerContext agentruntime.DeploymentRunTriggerContext `json:"trigger_context"`
	CreatedAt      time.Time                                `json:"created_at"`
}

type qoderDeploymentRunsEnvelope struct {
	Data     []qoderDeploymentRun `json:"data"`
	Runs     []qoderDeploymentRun `json:"runs"`
	NextPage *string              `json:"next_page"`
}

type deploymentBindingSummary struct {
	Name          string `json:"name,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	Status        string `json:"status,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
}

func (r *Runtime) CreateDeployment(ctx context.Context, scope agentruntime.Scope, input agentruntime.CreateDeploymentInput) (agentruntime.Deployment, error) {
	agentBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, input.AgentID)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	envBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, input.EnvironmentID)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	budget, err := translateSessionBudget(input.Budget)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	resources, err := r.translateCreateSessionResources(ctx, scope, input.Resources)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	vaultIDs, err := r.translateOwnedVaultIDs(ctx, scope, input.VaultIDs)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	reqBody := qoderCreateDeploymentRequest{
		Agent:         buildQoderAgentField(agentBinding.RemoteID, int64(input.AgentVersion)),
		EnvironmentID: envBinding.RemoteID,
		Name:          input.Name,
		Description:   input.Description,
		Metadata:      injectWatermark(input.Metadata, scope),
		InitialEvents: input.InitialEvents,
		Resources:     resources,
		VaultIDs:      vaultIDs,
		Budget:        budget,
		Schedule:      input.Schedule,
	}
	var remote qoderDeployment
	if err := r.doJSON(ctx, http.MethodPost, "/deployments", nil, nil, reqBody, &remote); err != nil {
		return agentruntime.Deployment{}, err
	}
	omaID, err := ids.New("depl_")
	if err != nil {
		return agentruntime.Deployment{}, agentruntime.InvalidArgumentError("failed to generate deployment id", err)
	}
	summary, _ := json.Marshal(deploymentBindingSummary{
		Name:          remote.Name,
		AgentID:       agentBinding.OMAExternalID,
		EnvironmentID: envBinding.OMAExternalID,
		Status:        remote.Status,
		Archived:      remote.ArchivedAt != nil,
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:                agentruntime.ResourceTypeDeployment,
		OMAExternalID:       omaID,
		RemoteID:            remote.ID,
		ParentOMAExternalID: agentBinding.OMAExternalID,
	}, summary); err != nil {
		return agentruntime.Deployment{}, err
	}
	return r.mapQoderDeployment(ctx, scope, omaID, agentBinding.OMAExternalID, envBinding.OMAExternalID, remote), nil
}

func (r *Runtime) GetDeployment(ctx context.Context, scope agentruntime.Scope, deploymentID string) (agentruntime.Deployment, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeployment, deploymentID)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	var remote qoderDeployment
	if err := r.doJSON(ctx, http.MethodGet, "/deployments/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.Deployment{}, err
	}
	mapped := r.mapQoderDeployment(ctx, scope, binding.OMAExternalID, binding.ParentOMAExternalID, "", remote)
	r.refreshDeploymentBinding(ctx, scope, binding, mapped)
	return mapped, nil
}

func (r *Runtime) ListDeployments(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListDeploymentsQuery) (agentruntime.Page[agentruntime.Deployment], error) {
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeDeployment, query.AgentID, "", query.IncludeArchived, 1000, 0)
	if err != nil {
		return agentruntime.Page[agentruntime.Deployment]{}, err
	}
	hydrated := r.hydrateDeployments(ctx, scope, rows)
	filtered := make([]agentruntime.Deployment, 0, len(hydrated))
	for _, item := range hydrated {
		if !matchesDeploymentFilter(item, query) {
			continue
		}
		filtered = append(filtered, item)
	}
	if offset >= len(filtered) {
		return agentruntime.Page[agentruntime.Deployment]{Items: []agentruntime.Deployment{}}, nil
	}
	end := offset + limit
	hasMore := end < len(filtered)
	if end > len(filtered) {
		end = len(filtered)
	}
	slice := filtered[offset:end]
	page := agentruntime.Page[agentruntime.Deployment]{
		Items:   slice,
		HasMore: hasMore,
	}
	if len(slice) > 0 {
		page.FirstID = slice[0].ID
		page.LastID = slice[len(slice)-1].ID
	}
	return page, nil
}

func (r *Runtime) UpdateDeployment(ctx context.Context, scope agentruntime.Scope, deploymentID string, input agentruntime.UpdateDeploymentInput) (agentruntime.Deployment, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeployment, deploymentID)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	payload := make(map[string]any)
	omaAgentID := binding.ParentOMAExternalID
	omaEnvID := ""
	if input.AgentID != nil {
		agentBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeAgent, *input.AgentID)
		if err != nil {
			return agentruntime.Deployment{}, err
		}
		omaAgentID = agentBinding.OMAExternalID
		payload["agent"] = buildQoderAgentField(agentBinding.RemoteID, int64(input.AgentVersion))
	}
	if input.EnvironmentID != nil {
		envBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeEnvironment, *input.EnvironmentID)
		if err != nil {
			return agentruntime.Deployment{}, err
		}
		omaEnvID = envBinding.OMAExternalID
		payload["environment_id"] = envBinding.RemoteID
	}
	if input.Name != nil {
		payload["name"] = *input.Name
	}
	if input.ClearDesc {
		payload["description"] = nil
	} else if input.Description != nil {
		payload["description"] = *input.Description
	}
	if input.MetadataSet {
		payload["metadata"] = injectWatermark(input.Metadata, scope)
	}
	if input.EventsSet {
		payload["initial_events"] = input.InitialEvents
	}
	if input.ResourcesSet {
		resources, err := r.translateCreateSessionResources(ctx, scope, input.Resources)
		if err != nil {
			return agentruntime.Deployment{}, err
		}
		if resources == nil {
			resources = []json.RawMessage{}
		}
		payload["resources"] = resources
	}
	if input.VaultIDsSet {
		vaultIDs, err := r.translateOwnedVaultIDs(ctx, scope, input.VaultIDs)
		if err != nil {
			return agentruntime.Deployment{}, err
		}
		if vaultIDs == nil {
			vaultIDs = []string{}
		}
		payload["vault_ids"] = vaultIDs
	}
	if input.BudgetSet {
		if input.Budget == nil {
			payload["budget"] = nil
		} else {
			budget, err := translateSessionBudget(input.Budget)
			if err != nil {
				return agentruntime.Deployment{}, err
			}
			payload["budget"] = budget
		}
	}
	if input.ScheduleSet {
		payload["schedule"] = input.Schedule
	}
	var remote qoderDeployment
	if err := r.doJSON(ctx, http.MethodPost, "/deployments/"+url.PathEscape(binding.RemoteID), nil, nil, payload, &remote); err != nil {
		return agentruntime.Deployment{}, err
	}
	mapped := r.mapQoderDeployment(ctx, scope, binding.OMAExternalID, omaAgentID, omaEnvID, remote)
	r.refreshDeploymentBinding(ctx, scope, binding, mapped)
	return mapped, nil
}

func (r *Runtime) ArchiveDeployment(ctx context.Context, scope agentruntime.Scope, deploymentID string) (agentruntime.Deployment, error) {
	return r.mutateDeploymentState(ctx, scope, deploymentID, "/archive", "archived")
}

func (r *Runtime) PauseDeployment(ctx context.Context, scope agentruntime.Scope, deploymentID string) (agentruntime.Deployment, error) {
	return r.mutateDeploymentState(ctx, scope, deploymentID, "/pause", "active")
}

func (r *Runtime) UnpauseDeployment(ctx context.Context, scope agentruntime.Scope, deploymentID string) (agentruntime.Deployment, error) {
	return r.mutateDeploymentState(ctx, scope, deploymentID, "/unpause", "active")
}

func (r *Runtime) mutateDeploymentState(ctx context.Context, scope agentruntime.Scope, deploymentID, suffix, bindingState string) (agentruntime.Deployment, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeployment, deploymentID)
	if err != nil {
		return agentruntime.Deployment{}, err
	}
	var remote qoderDeployment
	if err := r.doJSON(ctx, http.MethodPost, "/deployments/"+url.PathEscape(binding.RemoteID)+suffix, nil, nil, nil, &remote); err != nil {
		return agentruntime.Deployment{}, err
	}
	mapped := r.mapQoderDeployment(ctx, scope, binding.OMAExternalID, binding.ParentOMAExternalID, "", remote)
	summary, _ := json.Marshal(deploymentBindingSummary{
		Name:          mapped.Name,
		AgentID:       mapped.Agent.ID,
		EnvironmentID: mapped.EnvironmentID,
		Status:        mapped.Status,
		Archived:      mapped.ArchivedAt != nil || bindingState == "archived",
	})
	_, _ = r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeDeployment, binding.OMAExternalID, bindingState, summary)
	return mapped, nil
}

func (r *Runtime) RunDeployment(ctx context.Context, scope agentruntime.Scope, deploymentID string) (agentruntime.DeploymentRun, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeployment, deploymentID)
	if err != nil {
		return agentruntime.DeploymentRun{}, err
	}
	var remote qoderDeploymentRun
	if err := r.doJSON(ctx, http.MethodPost, "/deployments/"+url.PathEscape(binding.RemoteID)+"/run", nil, nil, nil, &remote); err != nil {
		return agentruntime.DeploymentRun{}, err
	}
	return r.claimAndMapDeploymentRun(ctx, scope, binding, remote)
}

func (r *Runtime) GetDeploymentRun(ctx context.Context, scope agentruntime.Scope, runID string) (agentruntime.DeploymentRun, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeploymentRun, runID)
	if err != nil {
		return agentruntime.DeploymentRun{}, err
	}
	var remote qoderDeploymentRun
	if err := r.doJSON(ctx, http.MethodGet, "/deployment_runs/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.DeploymentRun{}, err
	}
	parentBinding := db.RuntimeResourceBinding{OMAExternalID: binding.ParentOMAExternalID}
	if binding.ParentOMAExternalID != "" {
		if resolved, resolveErr := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeployment, binding.ParentOMAExternalID); resolveErr == nil {
			parentBinding = resolved
		}
	}
	return r.claimAndMapDeploymentRun(ctx, scope, parentBinding, remote)
}

func (r *Runtime) ListDeploymentRuns(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListDeploymentRunsQuery) (agentruntime.Page[agentruntime.DeploymentRun], error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := normalizeListOffset(query.Offset)
	var targetDeployments []db.RuntimeResourceBinding
	if query.DeploymentID != "" {
		binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeDeployment, query.DeploymentID)
		if err != nil {
			return agentruntime.Page[agentruntime.DeploymentRun]{}, err
		}
		targetDeployments = []db.RuntimeResourceBinding{binding}
	} else {
		rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeDeployment, "", "", true, 1000, 0)
		if err != nil {
			return agentruntime.Page[agentruntime.DeploymentRun]{}, err
		}
		targetDeployments = rows
	}
	if len(targetDeployments) == 0 {
		return agentruntime.Page[agentruntime.DeploymentRun]{Items: []agentruntime.DeploymentRun{}}, nil
	}
	collected, err := r.fetchRunsForDeployments(ctx, scope, targetDeployments, query)
	if err != nil {
		return agentruntime.Page[agentruntime.DeploymentRun]{}, err
	}
	sort.Slice(collected, func(i, j int) bool {
		if collected[i].CreatedAt.Equal(collected[j].CreatedAt) {
			return collected[i].ID > collected[j].ID
		}
		return collected[i].CreatedAt.After(collected[j].CreatedAt)
	})
	if offset >= len(collected) {
		return agentruntime.Page[agentruntime.DeploymentRun]{Items: []agentruntime.DeploymentRun{}}, nil
	}
	end := offset + limit
	hasMore := end < len(collected)
	if end > len(collected) {
		end = len(collected)
	}
	slice := collected[offset:end]
	page := agentruntime.Page[agentruntime.DeploymentRun]{
		Items:   slice,
		HasMore: hasMore,
	}
	if len(slice) > 0 {
		page.FirstID = slice[0].ID
		page.LastID = slice[len(slice)-1].ID
	}
	return page, nil
}

func (r *Runtime) fetchRunsForDeployments(
	ctx context.Context,
	scope agentruntime.Scope,
	deployments []db.RuntimeResourceBinding,
	query agentruntime.ListDeploymentRunsQuery,
) ([]agentruntime.DeploymentRun, error) {
	type result struct {
		runs []agentruntime.DeploymentRun
		err  error
	}
	results := make([]result, len(deployments))
	sem := make(chan struct{}, deploymentHydrateConcurrency)
	var wg sync.WaitGroup
	for idx, dep := range deployments {
		wg.Add(1)
		go func(i int, binding db.RuntimeResourceBinding) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runs, err := r.fetchRunsForSingleDeployment(ctx, scope, binding, query)
			results[i] = result{runs: runs, err: err}
		}(idx, dep)
	}
	wg.Wait()
	all := make([]agentruntime.DeploymentRun, 0)
	for _, res := range results {
		if res.err != nil {
			if len(deployments) == 1 {
				return nil, res.err
			}
			r.logger.WarnContext(ctx, "qoder deployment runs fetch failed", "error", res.err)
			continue
		}
		all = append(all, res.runs...)
	}
	return all, nil
}

func (r *Runtime) fetchRunsForSingleDeployment(
	ctx context.Context,
	scope agentruntime.Scope,
	deploymentBinding db.RuntimeResourceBinding,
	query agentruntime.ListDeploymentRunsQuery,
) ([]agentruntime.DeploymentRun, error) {
	collected := make([]agentruntime.DeploymentRun, 0)
	pageToken := ""
	for pageCount := 0; pageCount < 20; pageCount++ {
		params := buildDeploymentRunsQueryParams(deploymentBinding.RemoteID, query, pageToken)
		var env qoderDeploymentRunsEnvelope
		if err := r.doJSON(ctx, http.MethodGet, "/deployment_runs", params, nil, nil, &env); err != nil {
			return nil, err
		}
		rawRuns := env.Data
		if len(rawRuns) == 0 {
			rawRuns = env.Runs
		}
		for _, raw := range rawRuns {
			if raw.DeploymentID != "" && raw.DeploymentID != deploymentBinding.RemoteID {
				continue
			}
			mapped, err := r.claimAndMapDeploymentRun(ctx, scope, deploymentBinding, raw)
			if err != nil {
				return nil, err
			}
			if matchesDeploymentRunFilter(mapped, query) {
				collected = append(collected, mapped)
			}
		}
		if env.NextPage == nil || *env.NextPage == "" {
			break
		}
		pageToken = *env.NextPage
	}
	return collected, nil
}

func (r *Runtime) claimAndMapDeploymentRun(
	ctx context.Context,
	scope agentruntime.Scope,
	deploymentBinding db.RuntimeResourceBinding,
	remote qoderDeploymentRun,
) (agentruntime.DeploymentRun, error) {
	var omaRunID string
	if existing, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeDeploymentRun, remote.ID); err == nil {
		omaRunID = existing.OMAExternalID
	} else {
		candidateID, idErr := ids.New("drun_")
		if idErr != nil {
			return agentruntime.DeploymentRun{}, agentruntime.InvalidArgumentError("failed to generate deployment run id", idErr)
		}
		claimed, _, claimErr := r.bindings.ClaimOutput(ctx, scope, agentruntime.ResourceRef{
			Type:                agentruntime.ResourceTypeDeploymentRun,
			OMAExternalID:       candidateID,
			RemoteID:            remote.ID,
			ParentOMAExternalID: deploymentBinding.OMAExternalID,
		}, json.RawMessage(`{}`))
		if claimErr != nil {
			return agentruntime.DeploymentRun{}, claimErr
		}
		omaRunID = claimed.OMAExternalID
	}
	agentID, agentVersion := r.resolveMappedAgentRef(ctx, scope, deploymentBinding.ParentOMAExternalID, remote.Agent, remote.AgentID, remote.AgentVersion)
	var omaSessionID *string
	if remote.SessionID != nil && *remote.SessionID != "" {
		resolvedSessionID, err := r.claimRemoteSession(ctx, scope, *remote.SessionID, agentID)
		if err != nil {
			return agentruntime.DeploymentRun{}, err
		}
		omaSessionID = &resolvedSessionID
	}
	omaDeploymentID := deploymentBinding.OMAExternalID
	if omaDeploymentID == "" && remote.DeploymentID != "" {
		if dbRow, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeDeployment, remote.DeploymentID); err == nil {
			omaDeploymentID = dbRow.OMAExternalID
		}
	}
	triggerType := remote.TriggerContext.Type
	if triggerType == "" {
		triggerType = "manual"
	}
	return agentruntime.DeploymentRun{
		ID:           omaRunID,
		DeploymentID: omaDeploymentID,
		Agent: agentruntime.DeploymentAgentRef{
			ID:      agentID,
			Type:    "agent",
			Version: int(agentVersion),
		},
		SessionID: omaSessionID,
		Error:     remote.Error,
		TriggerContext: agentruntime.DeploymentRunTriggerContext{
			Type:        triggerType,
			ScheduledAt: remote.TriggerContext.ScheduledAt,
		},
		CreatedAt: remote.CreatedAt,
	}, nil
}

func (r *Runtime) claimRemoteSession(ctx context.Context, scope agentruntime.Scope, remoteSessionID, omaAgentID string) (string, error) {
	if existing, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeSession, remoteSessionID); err == nil {
		return existing.OMAExternalID, nil
	}
	candidateID, err := ids.New("sesn_")
	if err != nil {
		return "", agentruntime.InvalidArgumentError("failed to generate session id", err)
	}
	summary, _ := json.Marshal(sessionBindingSummary{AgentID: omaAgentID, Status: "running"})
	claimed, _, err := r.bindings.ClaimOutput(ctx, scope, agentruntime.ResourceRef{
		Type:                agentruntime.ResourceTypeSession,
		OMAExternalID:       candidateID,
		RemoteID:            remoteSessionID,
		ParentOMAExternalID: omaAgentID,
	}, summary)
	if err != nil {
		return "", err
	}
	return claimed.OMAExternalID, nil
}

func (r *Runtime) hydrateDeployments(ctx context.Context, scope agentruntime.Scope, rows []db.RuntimeResourceBinding) []agentruntime.Deployment {
	if len(rows) == 0 {
		return nil
	}
	items := make([]agentruntime.Deployment, len(rows))
	sem := make(chan struct{}, deploymentHydrateConcurrency)
	var wg sync.WaitGroup
	for idx, row := range rows {
		wg.Add(1)
		go func(i int, binding db.RuntimeResourceBinding) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var remote qoderDeployment
			if err := r.doJSON(ctx, http.MethodGet, "/deployments/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
				r.logger.WarnContext(ctx, "qoder deployment hydrate failed, falling back to binding summary", "oma_id", binding.OMAExternalID, "error", err)
				items[i] = deploymentFromBindingSummary(binding)
				return
			}
			items[i] = r.mapQoderDeployment(ctx, scope, binding.OMAExternalID, binding.ParentOMAExternalID, "", remote)
		}(idx, row)
	}
	wg.Wait()
	return items
}

func (r *Runtime) mapQoderDeployment(
	ctx context.Context,
	scope agentruntime.Scope,
	omaID string,
	fallbackAgentID string,
	fallbackEnvID string,
	remote qoderDeployment,
) agentruntime.Deployment {
	agentID, agentVersion := r.resolveMappedAgentRef(ctx, scope, fallbackAgentID, remote.Agent, remote.AgentID, remote.AgentVersion)
	envID := fallbackEnvID
	if envID == "" && remote.EnvironmentID != "" {
		if eb, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeEnvironment, remote.EnvironmentID); err == nil {
			envID = eb.OMAExternalID
		} else {
			envID = remote.EnvironmentID
		}
	}
	resources := make([]json.RawMessage, 0, len(remote.Resources))
	for _, raw := range remote.Resources {
		parsed, err := parseQoderSessionResource(raw)
		if err != nil {
			resources = append(resources, raw)
			continue
		}
		mapped := r.mapSessionResource(ctx, scope, "", parsed)
		resources = append(resources, mapped.Payload)
	}
	vaultIDs := make([]string, 0, len(remote.VaultIDs))
	for _, remoteVaultID := range remote.VaultIDs {
		if vb, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeVault, remoteVaultID); err == nil {
			vaultIDs = append(vaultIDs, vb.OMAExternalID)
		} else {
			vaultIDs = append(vaultIDs, remoteVaultID)
		}
	}
	var budget *agentruntime.SessionBudget
	if remote.Budget != nil && remote.Budget.MaxCreditCost != "" {
		budget = &agentruntime.SessionBudget{
			Mode:          agentruntime.BudgetModeCredits,
			MaxCreditCost: remote.Budget.MaxCreditCost,
		}
	}
	status := remote.Status
	if status == "" {
		status = "active"
	}
	return agentruntime.Deployment{
		ID: omaID,
		Agent: agentruntime.DeploymentAgentRef{
			ID:      agentID,
			Type:    "agent",
			Version: int(agentVersion),
		},
		EnvironmentID: envID,
		Name:          remote.Name,
		Description:   remote.Description,
		Metadata:      stripWatermark(remote.Metadata),
		InitialEvents: remote.InitialEvents,
		Resources:     resources,
		VaultIDs:      vaultIDs,
		Budget:        budget,
		Schedule:      remote.Schedule,
		Status:        status,
		PausedReason:  remote.PausedReason,
		ArchivedAt:    remote.ArchivedAt,
		CreatedAt:     remote.CreatedAt,
		UpdatedAt:     remote.UpdatedAt,
	}
}

func (r *Runtime) resolveMappedAgentRef(
	ctx context.Context,
	scope agentruntime.Scope,
	fallbackAgentID string,
	field qoderAgentField,
	legacyAgentID string,
	legacyVersion int64,
) (string, int64) {
	remoteAgentID := field.ID
	if remoteAgentID == "" {
		remoteAgentID = legacyAgentID
	}
	agentID := fallbackAgentID
	if remoteAgentID != "" {
		if ab, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeAgent, remoteAgentID); err == nil {
			agentID = ab.OMAExternalID
		} else if agentID == "" {
			agentID = remoteAgentID
		}
	}
	version := field.Version
	if version == 0 {
		version = legacyVersion
	}
	if version <= 0 {
		version = 1
	}
	return agentID, version
}

func (r *Runtime) translateOwnedVaultIDs(ctx context.Context, scope agentruntime.Scope, omaVaultIDs []string) ([]string, error) {
	if len(omaVaultIDs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(omaVaultIDs))
	for _, id := range omaVaultIDs {
		binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeVault, id)
		if err != nil {
			return nil, err
		}
		out = append(out, binding.RemoteID)
	}
	return out, nil
}

func (r *Runtime) refreshDeploymentBinding(ctx context.Context, scope agentruntime.Scope, binding db.RuntimeResourceBinding, mapped agentruntime.Deployment) {
	state := "active"
	if mapped.ArchivedAt != nil {
		state = "archived"
	}
	summary, _ := json.Marshal(deploymentBindingSummary{
		Name:          mapped.Name,
		AgentID:       mapped.Agent.ID,
		EnvironmentID: mapped.EnvironmentID,
		Status:        mapped.Status,
		Archived:      mapped.ArchivedAt != nil,
	})
	_, _ = r.bindings.UpdateSummary(ctx, scope, agentruntime.ResourceTypeDeployment, binding.OMAExternalID, state, summary)
}

func buildQoderAgentField(remoteAgentID string, version int64) any {
	if version > 0 {
		return qoderAgentVersionRef{Type: "agent", ID: remoteAgentID, Version: version}
	}
	return remoteAgentID
}

func deploymentFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.Deployment {
	var summary deploymentBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	status := summary.Status
	if status == "" {
		status = "active"
	}
	var archivedAt *time.Time
	if row.State == "archived" || summary.Archived {
		t := row.UpdatedAt
		archivedAt = &t
	}
	return agentruntime.Deployment{
		ID: row.OMAExternalID,
		Agent: agentruntime.DeploymentAgentRef{
			ID:      summary.AgentID,
			Type:    "agent",
			Version: 1,
		},
		EnvironmentID: summary.EnvironmentID,
		Name:          summary.Name,
		Status:        status,
		ArchivedAt:    archivedAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func matchesDeploymentFilter(item agentruntime.Deployment, query agentruntime.ListDeploymentsQuery) bool {
	if !query.IncludeArchived && item.ArchivedAt != nil {
		return false
	}
	if query.AgentID != "" && item.Agent.ID != query.AgentID {
		return false
	}
	if query.Status != "" && item.Status != query.Status {
		return false
	}
	if query.CreatedAtGTE != nil && item.CreatedAt.Before(*query.CreatedAtGTE) {
		return false
	}
	if query.CreatedAtLTE != nil && item.CreatedAt.After(*query.CreatedAtLTE) {
		return false
	}
	return true
}

func matchesDeploymentRunFilter(item agentruntime.DeploymentRun, query agentruntime.ListDeploymentRunsQuery) bool {
	if query.TriggerType != "" && item.TriggerContext.Type != query.TriggerType {
		return false
	}
	if query.HasError != nil {
		hasErr := item.Error != nil
		if hasErr != *query.HasError {
			return false
		}
	}
	if query.CreatedAtGT != nil && !item.CreatedAt.After(*query.CreatedAtGT) {
		return false
	}
	if query.CreatedAtGTE != nil && item.CreatedAt.Before(*query.CreatedAtGTE) {
		return false
	}
	if query.CreatedAtLT != nil && !item.CreatedAt.Before(*query.CreatedAtLT) {
		return false
	}
	if query.CreatedAtLTE != nil && item.CreatedAt.After(*query.CreatedAtLTE) {
		return false
	}
	return true
}

func buildDeploymentRunsQueryParams(remoteDeploymentID string, query agentruntime.ListDeploymentRunsQuery, pageToken string) url.Values {
	params := url.Values{}
	if remoteDeploymentID != "" {
		params.Set("deployment_id", remoteDeploymentID)
	}
	if query.TriggerType != "" {
		params.Set("trigger_type", query.TriggerType)
	}
	if query.HasError != nil {
		params.Set("has_error", strconv.FormatBool(*query.HasError))
	}
	if query.CreatedAtGT != nil {
		params.Set("created_at[gt]", query.CreatedAtGT.UTC().Format(time.RFC3339Nano))
	}
	if query.CreatedAtGTE != nil {
		params.Set("created_at[gte]", query.CreatedAtGTE.UTC().Format(time.RFC3339Nano))
	}
	if query.CreatedAtLT != nil {
		params.Set("created_at[lt]", query.CreatedAtLT.UTC().Format(time.RFC3339Nano))
	}
	if query.CreatedAtLTE != nil {
		params.Set("created_at[lte]", query.CreatedAtLTE.UTC().Format(time.RFC3339Nano))
	}
	params.Set("limit", "100")
	if pageToken != "" {
		params.Set("page", pageToken)
	}
	return params
}

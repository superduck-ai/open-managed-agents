package environments

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/yourbatis"
)

func (h *Handler) WithPrebuilds(prebuilds *Prebuilds) *Handler { h.prebuilds = prebuilds; return h }

func (h *Handler) createEnvironment(ctx context.Context, next db.Environment) (db.Environment, error) {
	var result db.Environment
	err := h.db.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		if err := h.prebuilds.reconcilePrebuild(ctx, tx, nil, &next); err != nil {
			return err
		}
		var err error
		result, err = h.db.CreateEnvironmentTx(ctx, tx, next)
		return err
	})
	return result, err
}

func (h *Handler) updateEnvironment(ctx context.Context, workspace, id string, body environmentMutationRequest) (db.Environment, error) {
	var result db.Environment
	err := h.db.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		current, err := h.db.LockEnvironmentTx(ctx, tx, workspace, id)
		if err != nil {
			return err
		}
		next, err := h.applyEnvironmentMutation(current, body)
		if err != nil {
			return err
		}
		if err := h.prebuilds.reconcilePrebuild(ctx, tx, &current, &next); err != nil {
			return err
		}
		result, err = h.db.UpdateEnvironmentTx(ctx, tx, next)
		return err
	})
	return result, err
}

func (h *Handler) applyEnvironmentMutation(current db.Environment, body environmentMutationRequest) (db.Environment, error) {
	next := current
	var err error
	if len(body.Name) > 0 {
		next.Name, err = parseRequiredRawString(body.Name, "name")
		if err != nil {
			return db.Environment{}, invalidRequest(err)
		}
	}
	if len(body.Description) > 0 {
		next.Description, err = descriptionFromRaw(body.Description)
		if err != nil {
			return db.Environment{}, invalidRequest(err)
		}
	}
	if len(body.Metadata) > 0 {
		next.Metadata, err = patchMetadata(current.Metadata, body.Metadata)
		if err != nil {
			return db.Environment{}, invalidRequest(err)
		}
	}
	if len(body.Scope) > 0 {
		next.Scope, err = parseScope(body.Scope)
		if err != nil {
			return db.Environment{}, invalidRequest(err)
		}
	}
	if len(body.Config) > 0 {
		next.Config, err = normalizeConfigForUpdate(current.Config, body.Config)
		if err != nil {
			return db.Environment{}, invalidRequest(err)
		}
		next.ResolvedTemplate = h.resolvedTemplate(next.Config)
	}
	next.UpdatedAt = time.Now().UTC()
	return next, nil
}

type prebuildResponse struct {
	CreatedAt       *time.Time `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	JobID           string     `json:"job_id"`
	Stage           string     `json:"stage"`
	State           string     `json:"state"`
	Message         string     `json:"message"`
	CanStart        bool       `json:"can_start"`
	CanCancel       bool       `json:"can_cancel"`
	HasImageLogs    bool       `json:"image_logs"`
	HasTemplateLogs bool       `json:"template_logs"`
}

func (h *Handler) requestPrebuildEnvironment(r *http.Request) (db.Environment, error) {
	principal, err := requireWorkspaceCredential(r)
	if err != nil {
		return db.Environment{}, err
	}
	id := chi.URLParam(r, "environment_id")
	env, err := h.db.GetEnvironment(r.Context(), principal.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) {
		return env, environmentNotFound(id, err)
	}
	if err != nil {
		return env, prebuildError(err)
	}
	return env, nil
}

func (h *Handler) requestPrebuild(r *http.Request) (db.Environment, prebuildTask, error) {
	env, err := h.requestPrebuildEnvironment(r)
	if err != nil || env.BuildJobID == nil {
		return env, prebuildTask{}, err
	}
	task, err := h.prebuilds.loadCurrentPrebuild(r.Context(), nil, env)
	if err != nil {
		return env, task, prebuildError(err)
	}
	return env, task, nil
}

func (svc *Prebuilds) currentPrebuildResponse(env db.Environment, task prebuildTask) *prebuildResponse {
	result := &prebuildResponse{Stage: task.stage(), State: task.state(), Message: task.output.Message}
	mutable := svc != nil && svc.cfg.Enabled && env.ArchivedAt == nil
	providerMatches := svc != nil && task.job != nil && task.job.Args.ProviderKey == svc.providerKey
	if task.job == nil {
		result.Message = "Build task details have expired."
	} else {
		result.JobID = strconv.FormatInt(task.job.ID, 10)
		result.CreatedAt = &task.job.CreatedAt
		result.FinishedAt = task.job.FinalizedAt
		if result.Message == "" && len(task.job.Errors) > 0 {
			result.Message = task.job.Errors[len(task.job.Errors)-1].Error
		}
		available := providerMatches && svc.cfg.Enabled
		result.HasImageLogs = available && svc.images.Capabilities().Logs && task.output.ImageJobRef != ""
		result.HasTemplateLogs = available && svc.templates.Capabilities().Logs && task.output.TemplateJobRef != ""
	}
	result.CanCancel = mutable && providerMatches && task.active() && !task.output.Submitting && !task.output.CancelRequested && (task.ref() == "" || svc.capabilities(task.stage()).Cancel)
	if env.ResolvedTemplate != "" {
		result.State, result.Stage = "ready", "template"
		result.CanCancel = false
	}
	result.CanStart = mutable && !task.active() && result.State != "ready"
	return result
}

func (h *Handler) getPrebuild(w http.ResponseWriter, r *http.Request) error {
	env, task, err := h.requestPrebuild(r)
	if err != nil {
		return err
	}
	var result *prebuildResponse
	if env.BuildJobID == nil {
		packages, err := decodeEnvironmentPackages(env.Config)
		if err != nil {
			return prebuildError(err)
		}
		if packages != nil {
			result = &prebuildResponse{State: "idle", CanStart: h.prebuilds != nil && h.prebuilds.cfg.Enabled && env.ArchivedAt == nil}
		}
	} else {
		result = h.prebuilds.currentPrebuildResponse(env, task)
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Build *prebuildResponse `json:"build"`
	}{result})
	return nil
}

func (h *Handler) getPrebuildLogs(w http.ResponseWriter, r *http.Request) error {
	_, task, err := h.requestPrebuild(r)
	if err != nil {
		return err
	}
	if task.job == nil || r.URL.Query().Get("job_id") != strconv.FormatInt(task.job.ID, 10) {
		return prebuildError(errPrebuildConflict)
	}
	stage := r.URL.Query().Get("stage")
	if stage != "image" && stage != "template" {
		return invalidRequest(errPrebuildStage)
	}
	chunk, err := h.prebuilds.readLogs(r.Context(), task, stage, r.URL.Query().Get("cursor"))
	if err != nil {
		return prebuildError(err)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, chunk)
	return nil
}

func (h *Handler) startPrebuild(w http.ResponseWriter, r *http.Request) error {
	env, err := h.requestPrebuildEnvironment(r)
	if err != nil {
		return err
	}
	if err := h.prebuilds.StartOrRetryPrebuild(r.Context(), env); err != nil {
		return prebuildError(err)
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (h *Handler) cancelPrebuild(w http.ResponseWriter, r *http.Request) error {
	env, err := h.requestPrebuildEnvironment(r)
	if err != nil {
		return err
	}
	if h.prebuilds == nil {
		return prebuildError(errPrebuildUnavailable)
	}
	body, err := httpapi.DecodeObjectBodyAs[prebuildCancelRequest](w, r, 4096)
	if err != nil {
		return invalidRequest(err)
	}
	if err := h.prebuilds.CancelPrebuild(r.Context(), env, body.JobID); err != nil {
		return prebuildError(err)
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

type prebuildCancelRequest struct {
	JobID int64 `json:"job_id,string"`
}

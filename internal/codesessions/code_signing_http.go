package codesessions

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

type signCommitRequest struct {
	Contents        string            `json:"contents"`
	Source          *signCommitSource `json:"source,omitempty"`
	GitObjectFormat string            `json:"git_object_format,omitempty"`
}

type signCommitSource struct {
	Type    string             `json:"type"`
	GitInfo *signCommitGitInfo `json:"git_info"`
}

type signCommitGitInfo struct {
	Type string `json:"type"`
	Repo string `json:"repo"`
	Ref  string `json:"ref,omitempty"`
}

type signCommitResponse struct {
	Signature string `json:"signature"`
}

func (h *Handler) handleSignCommit(w http.ResponseWriter, r *http.Request) error {
	codeSessionID := chi.URLParam(r, "code_session_id")
	claims, err := h.sessionIngressClaims(r, codeSessionID)
	if err != nil {
		return err
	}
	if claims.WorkerEpoch <= 0 {
		return sessionIngressTokenInvalid(errors.New("commit signing requires a managed code-session credential"))
	}

	request, err := httpapi.DecodeObjectBodyAs[signCommitRequest](w, r, maxIngressBodySize)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return signCommitRequestTooLarge(err)
		}
		return invalidSignCommitRequest("Invalid JSON body", err)
	}
	objectFormat, err := validateSignCommitRequest(request)
	if err != nil {
		return err
	}
	signature, err := h.service.SignGitCommit([]byte(request.Contents))
	if err != nil {
		return signCommitFailure(err)
	}
	h.logger.InfoContext(
		r.Context(),
		"git commit signed",
		"session_id", claims.SessionID,
		"content_length", len(request.Contents),
		"git_object_format", objectFormat,
	)
	httpapi.WriteJSON(w, http.StatusOK, signCommitResponse{Signature: signature})
	return nil
}

func validateSignCommitRequest(request *signCommitRequest) (string, error) {
	if request.Contents == "" {
		return "", invalidSignCommitRequest("contents must not be empty", nil)
	}
	objectFormat := strings.TrimSpace(request.GitObjectFormat)
	if objectFormat == "" {
		objectFormat = "sha1"
	}
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return "", invalidSignCommitRequest("git_object_format must be sha1 or sha256", nil)
	}
	if request.Source == nil {
		return objectFormat, nil
	}
	if strings.TrimSpace(request.Source.Type) != "git_repository" {
		return "", invalidSignCommitRequest("source.type must be git_repository", nil)
	}
	if request.Source.GitInfo == nil || strings.TrimSpace(request.Source.GitInfo.Type) == "" || strings.TrimSpace(request.Source.GitInfo.Repo) == "" {
		return "", invalidSignCommitRequest("source.git_info must include type and repo", nil)
	}
	return objectFormat, nil
}

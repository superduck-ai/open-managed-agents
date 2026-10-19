package codesessions

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const maxBrainClaimBodySize = 4 << 10

type brainClaimResponse struct {
	SessionID      string `json:"session_id"`
	WorkerEpoch    int64  `json:"worker_epoch"`
	IngressToken   string `json:"ingress_token"`
	ModelToken     string `json:"model_token"`
	ClaimExpiresAt string `json:"claim_expires_at"`
}

type brainClaimReleaseRequest struct {
	WorkerEpoch int64  `json:"worker_epoch"`
	Reason      string `json:"reason"`
}

type BrainClaimAuthenticator struct {
	tokenHash [sha256.Size]byte
	enabled   bool
}

func LoadBrainClaimAuthenticator(path string) (BrainClaimAuthenticator, error) {
	if path == "" {
		return BrainClaimAuthenticator{}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return BrainClaimAuthenticator{}, fmt.Errorf("read brain claim token file: %w", err)
	}
	token := strings.TrimSpace(string(content))
	if len(token) < 32 {
		return BrainClaimAuthenticator{}, errors.New("brain claim token must contain at least 32 characters")
	}
	return BrainClaimAuthenticator{tokenHash: sha256.Sum256([]byte(token)), enabled: true}, nil
}

func (a BrainClaimAuthenticator) authorize(r *http.Request) error {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !a.enabled || !ok {
		return brainClaimUnauthenticated()
	}
	provided := sha256.Sum256([]byte(strings.TrimSpace(token)))
	if subtle.ConstantTimeCompare(provided[:], a.tokenHash[:]) != 1 {
		return brainClaimUnauthenticated()
	}
	return nil
}

func (h *Handler) WithBrainClaims(authenticator BrainClaimAuthenticator) *Handler {
	h.brainClaims = authenticator
	return h
}

func (h *Handler) registerBrainClaimRoutes(router chi.Router) {
	router.Route("/code/brain/claims", func(claimRouter chi.Router) {
		claimRouter.Post("/", h.errorAdapter.Wrap(h.handleBrainClaim))
		claimRouter.Post("/{code_session_id}/release", h.errorAdapter.Wrap(h.handleBrainClaimRelease))
	})
}

func brainClaimWorkerID(r *http.Request) (string, error) {
	workerID := strings.TrimSpace(r.Header.Get("X-Worker-ID"))
	if workerID == "" || len(workerID) > 128 {
		return "", brainClaimInvalidRequest("X-Worker-ID header is required", nil)
	}
	return workerID, nil
}

func parseBrainClaimBlock(r *http.Request) (time.Duration, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("block_ms"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || time.Duration(value)*time.Millisecond > brainClaimMaxBlock {
		return 0, brainClaimInvalidRequest("block_ms must be between 0 and 30000", err)
	}
	return time.Duration(value) * time.Millisecond, nil
}

func (h *Handler) handleBrainClaim(w http.ResponseWriter, r *http.Request) error {
	if err := h.brainClaims.authorize(r); err != nil {
		return err
	}
	workerID, err := brainClaimWorkerID(r)
	if err != nil {
		return err
	}
	block, err := parseBrainClaimBlock(r)
	if err != nil {
		return err
	}
	claim, found, err := h.service.ClaimBrainWorker(r.Context(), workerID, block)
	if err != nil {
		if r.Context().Err() != nil {
			return nil
		}
		return brainClaimFailure(err)
	}
	w.Header().Set("Cache-Control", "no-store")
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	httpapi.WriteJSON(w, http.StatusOK, brainClaimResponse{
		SessionID:      claim.CodeSessionID,
		WorkerEpoch:    claim.WorkerEpoch,
		IngressToken:   claim.IngressToken,
		ModelToken:     claim.ModelToken,
		ClaimExpiresAt: claim.ClaimExpiresAt.Format(time.RFC3339Nano),
	})
	return nil
}

func (h *Handler) handleBrainClaimRelease(w http.ResponseWriter, r *http.Request) error {
	if err := h.brainClaims.authorize(r); err != nil {
		return err
	}
	workerID, err := brainClaimWorkerID(r)
	if err != nil {
		return err
	}
	request, err := httpapi.DecodeObjectBodyAs[brainClaimReleaseRequest](w, r, maxBrainClaimBodySize)
	if err != nil {
		return brainClaimInvalidRequest("Invalid JSON body", err)
	}
	reason := BrainClaimReleaseReason(request.Reason)
	if request.WorkerEpoch <= 0 || !validBrainClaimReleaseReason(reason) {
		return brainClaimInvalidRequest("worker_epoch must be positive and reason must be admission_failed, shutdown, idle or permanent", nil)
	}
	codeSessionID := chi.URLParam(r, "code_session_id")
	if err := h.service.ReleaseBrainClaim(r.Context(), codeSessionID, workerID, request.WorkerEpoch, reason); err != nil {
		return mapBrainClaimReleaseError(err)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func validBrainClaimReleaseReason(reason BrainClaimReleaseReason) bool {
	_, ok := brainClaimReleaseKinds[reason]
	return ok || reason == BrainClaimPermanent
}

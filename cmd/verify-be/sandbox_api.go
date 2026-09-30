package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func newLocalSandboxAPI(runID string) *httptest.Server {
	router := chi.NewRouter()
	router.Route("/sandboxes/{sandbox_id}", func(router chi.Router) {
		router.Post("/connect", localSandboxHandler(runID, "connect"))
		router.Post("/timeout", localSandboxHandler(runID, "timeout"))
		router.Delete("/", localSandboxHandler(runID, "delete"))
	})
	return httptest.NewServer(router)
}

func localSandboxHandler(runID, operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "sandbox_id")
		if !strings.HasPrefix(id, "oma-public-"+runID+"-") {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		label, err := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{index .Config.Labels "oma.verify-be.run"}}`, id).Output()
		if err != nil || string(label) != runID+"\n" {
			http.NotFound(w, r)
			return
		}
		if operation == "delete" {
			if err := exec.CommandContext(ctx, "docker", "rm", "-f", id).Run(); err != nil {
				http.Error(w, "local sandbox deletion failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var request struct {
			Timeout int `json:"timeout"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Timeout <= 0 {
			http.Error(w, "positive timeout required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if operation == "connect" {
			_ = json.NewEncoder(w).Encode(struct {
				SandboxID string `json:"sandboxID"`
				EnvdURL   string `json:"envdURL"`
			}{SandboxID: id, EnvdURL: "http://127.0.0.1:1"})
			return
		}
		_, _ = w.Write([]byte("{}"))
	}
}

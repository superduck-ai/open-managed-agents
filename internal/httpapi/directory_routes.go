package httpapi

import (
	_ "embed"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed directory_servers.json
var directoryServersData []byte

func RegisterDirectoryRoutes(r chi.Router) {
	r.Get("/api/directory/servers", handleDirectoryServers)
	r.Options("/api/directory/servers", handleDirectoryServersPreflight)
}

func handleDirectoryServers(w http.ResponseWriter, r *http.Request) {
	writeDirectoryCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(directoryServersData)
}

func handleDirectoryServersPreflight(w http.ResponseWriter, r *http.Request) {
	writeDirectoryCORSHeaders(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func writeDirectoryCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	allowHeaders := r.Header.Get("Access-Control-Request-Headers")
	if allowHeaders == "" {
		allowHeaders = "Accept, Authorization, Baggage, Content-Type, Origin, Sentry-Trace, X-Requested-With, anthropic-client-sha, anthropic-client-version, anthropic-device-id, x-stainless-arch, x-stainless-lang, x-stainless-os, x-stainless-package-version, x-stainless-runtime, x-stainless-runtime-version"
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
	w.Header().Set("Access-Control-Max-Age", "86400")
	if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
}

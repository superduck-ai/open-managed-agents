package dsh

import "net/http"

func RequireVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("DSR-Protocol") != "1" {
			writeError(w, r, unsupportedProtocol(), nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

package channels

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func requiresRuntime(w http.ResponseWriter, r *http.Request, settings bool) (bool, error) {
	if r.Method != http.MethodPost || settings || chi.URLParam(r, "pairing_id") != "" {
		return false, nil
	}
	if chi.URLParam(r, "channel_id") == "" || strings.HasSuffix(chi.RouteContext(r.Context()).RoutePattern(), "/qr") {
		return true, nil
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		return false, invalidChannelUpdate(err)
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return false, invalidChannelUpdate(err)
	}
	return !(len(body) == 1 && body["enabled"] == false), nil
}

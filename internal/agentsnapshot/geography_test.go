package agentsnapshot

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestExpandedRosterGeographyRejectsModelOverrideMismatch(t *testing.T) {
	for _, geo := range []string{"", "us", "global"} {
		model := json.RawMessage(`{"id":"fixture"}`)
		if geo != "" {
			model = json.RawMessage(`{"id":"fixture","inference_geo":"` + geo + `"}`)
		}
		raw := append(append([]byte(`{"model":`), model...), '}')
		if err := validateRosterGeography(model, []json.RawMessage{raw}); err != nil {
			t.Fatal("same pin rejected", err)
		}
		other := json.RawMessage(`{"id":"fixture","inference_geo":"us"}`)
		if geo == "us" {
			other = json.RawMessage(`{"id":"fixture","inference_geo":"global"}`)
		}
		if err := validateRosterGeography(other, []json.RawMessage{raw}); !errors.Is(err, errRosterInferenceGeoMismatch) {
			t.Fatal("mixed model override accepted", geo, err)
		}
	}
}

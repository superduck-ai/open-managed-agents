package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestPlatformPrivacyConsentsRoutes(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("platform-privacy-consents-bucket"))
	defer app.close()

	emptyResp := app.platformRequest(t, http.MethodGet, "/v1/privacy-consents?prefix=cookies.codex_test.", nil, nil)
	defer emptyResp.Body.Close()
	if emptyResp.StatusCode != http.StatusOK {
		t.Fatalf("empty privacy consents status = %d, want 200: %s", emptyResp.StatusCode, readAll(t, emptyResp.Body))
	}
	var empty map[string]any
	decodeJSON(t, emptyResp.Body, &empty)
	if consents, ok := empty["consents"].([]any); !ok || len(consents) != 0 {
		t.Fatalf("empty consents = %#v, want empty array", empty["consents"])
	}

	putResp := app.platformRequest(t, http.MethodPut, "/v1/privacy-consents", strings.NewReader(`{"consent_type":"cookies.codex_test.analytics","accepted":true}`), nil)
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("put privacy consent status = %d, want 200: %s", putResp.StatusCode, readAll(t, putResp.Body))
	}
	var put map[string]any
	decodeJSON(t, putResp.Body, &put)
	if len(put) != 0 {
		t.Fatalf("put response = %#v, want empty object", put)
	}

	listResp := app.platformRequest(t, http.MethodGet, "/v1/privacy-consents?prefix=cookies.", nil, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list privacy consents status = %d, want 200: %s", listResp.StatusCode, readAll(t, listResp.Body))
	}
	var list map[string]any
	decodeJSON(t, listResp.Body, &list)
	consents, ok := list["consents"].([]any)
	if !ok {
		t.Fatalf("consents = %#v, want array", list["consents"])
	}
	for _, value := range consents {
		consent, _ := value.(map[string]any)
		if consent["consent_type"] == "cookies.codex_test.analytics" && consent["accepted"] == true {
			return
		}
	}
	t.Fatalf("consents = %#v, want stored cookie consent", consents)
}

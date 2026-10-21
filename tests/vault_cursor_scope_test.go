//go:build integration || e2e || e2b_integration

package tests

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestVaultCursorCannotSelectAnotherWorkspacePosition(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vault-cursor-scope"))
	defer app.close()
	suffix := uniqueAdminSuffix()
	otherKey := "sk-vault-cursor-other-" + suffix
	seedWorkspaceKey(t, app.pool, "cursor-org-"+suffix, "cursor-workspace-"+suffix, "cursor-key-"+suffix, otherKey)
	keys := []string{defaultTestKey, otherKey}
	owned := make([]map[string]bool, len(keys))
	for i, key := range keys {
		owned[i] = map[string]bool{}
		for range 2 {
			resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults?beta=true", strings.NewReader(`{"display_name":"cursor scope"}`), key, true)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("create status = %d", resp.StatusCode)
			}
			var vault vaultAPIResponse
			decodeJSON(t, resp.Body, &vault)
			resp.Body.Close()
			defer cleanupVaultRows(t, app, vault.ID)
			owned[i][vault.ID] = true
		}
	}
	list := func(key, cursor string) vaultPageAPIResponse {
		t.Helper()
		resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults?beta=true&limit=1&page="+url.QueryEscape(cursor), nil, key, true)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d", resp.StatusCode)
		}
		var page vaultPageAPIResponse
		decodeJSON(t, resp.Body, &page)
		return page
	}
	first := []vaultPageAPIResponse{list(keys[0], ""), list(keys[1], "")}
	for _, page := range first {
		if len(page.Data) != 1 || page.NextPage == nil || *page.NextPage == "" {
			t.Fatalf("positive cursor control = %+v", page)
		}
	}
	for i, key := range keys {
		page := list(key, *first[1-i].NextPage)
		if len(page.Data) != 0 || page.NextPage != nil {
			t.Fatalf("foreign cursor returned a nonempty or continuing page: %+v", page)
		}
	}
	for i, key := range keys {
		page := list(key, *first[i].NextPage)
		if len(page.Data) != 1 || !owned[i][page.Data[0].ID] || page.Data[0].ID == first[i].Data[0].ID {
			t.Fatalf("same-workspace next page = %+v", page)
		}
	}
}

//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"uuid"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestFilesPaginationCompatibility(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("files-pagination-bucket"))
	defer app.close()
	scopeID := "pagination_" + uuid.NewV4().String()
	fileIDs := make([]string, 6)
	for index := range fileIDs {
		fileIDs[index] = createMetadataOnlyFile(t, app, scopeID)
		defer softDeleteFile(t, app, fileIDs[index])
	}
	query := "limit=2&scope_id=" + scopeID
	first := listFiles(t, app, query)
	if first.NextPage == nil || !first.HasMore || first.LastID == nil {
		t.Fatalf("first page missing continuation: %+v", first)
	}

	t.Run("failure invalid and conflicting cursors", func(t *testing.T) {
		for _, invalid := range []string{
			"page=bad", "page=" + url.QueryEscape(*first.NextPage),
			"page=" + url.QueryEscape(*first.NextPage) + "&scope_id=other",
			query + "&page=" + url.QueryEscape(*first.NextPage) + "&after_id=" + fileIDs[0],
			query + "&page=" + url.QueryEscape(*first.NextPage) + "&before_id=" + fileIDs[0],
		} {
			resp := app.do(t, http.MethodGet, "/v1/files?"+invalid, nil, defaultTestKey, false, "")
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
	})

	t.Run("failure cursor cannot expose another workspace", func(t *testing.T) {
		suffix := uuid.NewV4().String()
		key := "sk-file-page-" + suffix
		seedWorkspaceKey(t, app.pool, "file-page-"+suffix, "wrkspc_"+suffix, "key_"+suffix, key)
		resp := app.do(t, http.MethodGet, "/v1/files?"+query+"&page="+url.QueryEscape(*first.NextPage), nil, key, false, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("foreign cursor status=%d: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var page pageResponse
		decodeJSON(t, resp.Body, &page)
		if len(page.Data) != 0 || page.HasMore || page.NextPage != nil {
			t.Fatalf("foreign cursor exposed files: %+v", page)
		}
	})

	t.Run("success formal SDK automatic pagination", func(t *testing.T) {
		client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
		pager := client.Files.ListAutoPaging(context.Background(), anthropic.FileListParams{Limit: anthropic.Int(2)}, option.WithQuery("scope_id", scopeID))
		var got []string
		for pager.Next() {
			got = append(got, pager.Current().ID)
			if len(got) > len(fileIDs) {
				t.Fatal("SDK pagination repeated files")
			}
		}
		want := slices.Clone(fileIDs)
		slices.Reverse(want)
		if pager.Err() != nil || !slices.Equal(got, want) {
			t.Fatalf("SDK got %v, error %v, want %v", got, pager.Err(), want)
		}
	})

	t.Run("success legacy and formal forward cursors agree", func(t *testing.T) {
		legacy := listFiles(t, app, query+"&after_id="+*first.LastID)
		formal := listFiles(t, app, query+"&page="+url.QueryEscape(*first.NextPage))
		if len(legacy.Data) != 2 || formal.NextPage == nil || !slices.EqualFunc(legacy.Data, formal.Data, func(a, b metadataResponse) bool { return a.ID == b.ID }) {
			t.Fatalf("legacy=%+v formal=%+v", legacy, formal)
		}
		last := listFiles(t, app, query+"&page="+url.QueryEscape(*formal.NextPage))
		if last.HasMore || last.NextPage != nil || len(last.Data) != 2 || last.LastID == nil {
			t.Fatalf("unexpected terminal page: %+v", last)
		}
		empty := listFiles(t, app, query+"&after_id="+*last.LastID)
		if empty.HasMore || empty.NextPage != nil || empty.FirstID != nil || empty.LastID != nil || len(empty.Data) != 0 {
			t.Fatalf("unexpected empty page: %+v", empty)
		}
	})

	t.Run("success backward continuation preserves direction", func(t *testing.T) {
		page := listFiles(t, app, query+"&before_id="+fileIDs[0])
		for _, want := range [][]string{{fileIDs[2], fileIDs[1]}, {fileIDs[4], fileIDs[3]}, {fileIDs[5]}} {
			var got []string
			for _, file := range page.Data {
				got = append(got, file.ID)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("backward got %v, want %v", got, want)
			}
			if page.NextPage != nil {
				page = listFiles(t, app, query+"&page="+url.QueryEscape(*page.NextPage))
			}
		}
		if page.HasMore || page.NextPage != nil {
			t.Fatalf("backward pagination did not terminate: %+v", page)
		}
	})
}

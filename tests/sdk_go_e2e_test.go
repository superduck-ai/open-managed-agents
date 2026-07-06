package tests

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestGoSDKFilesE2E(t *testing.T) {
	baseURL := os.Getenv("TEST_API_BASE_URL")
	if baseURL == "" {
		app := newTestApp(t, nil)
		defer app.close()
		baseURL = app.baseURL
	}

	client := anthropic.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx := context.Background()

	t.Run("failure missing file id", func(t *testing.T) {
		_, err := client.Beta.Files.GetMetadata(ctx, "file_missing_go_sdk", anthropic.BetaFileGetMetadataParams{})
		if err == nil {
			t.Fatal("expected missing file metadata request to fail")
		}
	})

	t.Run("failure uploaded file is not downloadable", func(t *testing.T) {
		uploaded, err := client.Beta.Files.Upload(ctx, anthropic.BetaFileUploadParams{
			File: anthropic.File(bytes.NewReader([]byte("go sdk no download")), "go-sdk-no-download.txt", "text/plain"),
		})
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		defer client.Beta.Files.Delete(ctx, uploaded.ID, anthropic.BetaFileDeleteParams{})

		resp, err := client.Beta.Files.Download(ctx, uploaded.ID, anthropic.BetaFileDownloadParams{})
		if err == nil {
			resp.Body.Close()
			t.Fatal("expected uploaded file download to fail")
		}
	})

	t.Run("success upload list retrieve delete", func(t *testing.T) {
		uploaded, err := client.Beta.Files.Upload(ctx, anthropic.BetaFileUploadParams{
			File: anthropic.File(bytes.NewReader([]byte("hello from go sdk")), "go-sdk.txt", "text/plain"),
		})
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		if uploaded.ID == "" || uploaded.Filename != "go-sdk.txt" || uploaded.Type != "file" {
			t.Fatalf("unexpected upload response: %+v", uploaded)
		}

		page, err := client.Beta.Files.List(ctx, anthropic.BetaFileListParams{Limit: anthropic.Int(20)})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, file := range page.Data {
			if file.ID == uploaded.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("uploaded file %s not found in list", uploaded.ID)
		}

		retrieved, err := client.Beta.Files.GetMetadata(ctx, uploaded.ID, anthropic.BetaFileGetMetadataParams{})
		if err != nil {
			t.Fatalf("retrieve metadata: %v", err)
		}
		if retrieved.ID != uploaded.ID {
			t.Fatalf("retrieved id = %s, want %s", retrieved.ID, uploaded.ID)
		}

		deleted, err := client.Beta.Files.Delete(ctx, uploaded.ID, anthropic.BetaFileDeleteParams{})
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if deleted.ID != uploaded.ID || deleted.Type != "file_deleted" {
			t.Fatalf("unexpected delete response: %+v", deleted)
		}
	})
}

func TestGoSDKMemoryStoresE2E(t *testing.T) {
	baseURL := os.Getenv("TEST_API_BASE_URL")
	if baseURL == "" {
		app := newTestApp(t, nil)
		defer app.close()
		baseURL = app.baseURL
	}

	client := anthropic.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx := context.Background()

	store, err := client.Beta.MemoryStores.New(ctx, anthropic.BetaMemoryStoreNewParams{
		Name:        "go-sdk-memory-store",
		Description: anthropic.String("go sdk memory e2e"),
		Metadata:    map[string]string{"sdk": "go"},
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer client.Beta.MemoryStores.Delete(ctx, store.ID, anthropic.BetaMemoryStoreDeleteParams{})
	if store.ID == "" || store.Type != anthropic.BetaManagedAgentsMemoryStoreTypeMemoryStore || store.Metadata["sdk"] != "go" {
		t.Fatalf("unexpected store response: %+v", store)
	}

	storePage, err := client.Beta.MemoryStores.List(ctx, anthropic.BetaMemoryStoreListParams{Limit: anthropic.Int(20)})
	if err != nil {
		t.Fatalf("list stores: %v", err)
	}
	if !goSDKStorePageContains(storePage.Data, store.ID) {
		t.Fatalf("store %s not found in list", store.ID)
	}

	retrievedStore, err := client.Beta.MemoryStores.Get(ctx, store.ID, anthropic.BetaMemoryStoreGetParams{})
	if err != nil {
		t.Fatalf("retrieve store: %v", err)
	}
	if retrievedStore.ID != store.ID {
		t.Fatalf("retrieved store id = %s, want %s", retrievedStore.ID, store.ID)
	}

	updatedStore, err := client.Beta.MemoryStores.Update(ctx, store.ID, anthropic.BetaMemoryStoreUpdateParams{
		Name:        anthropic.String("go-sdk-memory-store-updated"),
		Description: anthropic.String(""),
		Metadata:    map[string]string{"phase": "updated"},
	})
	if err != nil {
		t.Fatalf("update store: %v", err)
	}
	if updatedStore.Name != "go-sdk-memory-store-updated" || updatedStore.Metadata["phase"] != "updated" {
		t.Fatalf("unexpected updated store: %+v", updatedStore)
	}

	memory, err := client.Beta.MemoryStores.Memories.New(ctx, store.ID, anthropic.BetaMemoryStoreMemoryNewParams{
		Path:    "/go-sdk/notes.md",
		Content: anthropic.String("hello from go sdk memory"),
		View:    anthropic.BetaManagedAgentsMemoryViewFull,
	})
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	if memory.Type != anthropic.BetaManagedAgentsMemoryTypeMemory || memory.Content != "hello from go sdk memory" || memory.MemoryStoreID != store.ID {
		t.Fatalf("unexpected memory response: %+v", memory)
	}
	firstVersionID := memory.MemoryVersionID

	if _, err := client.Beta.MemoryStores.Memories.New(ctx, store.ID, anthropic.BetaMemoryStoreMemoryNewParams{
		Path:    "/go-sdk/notes.md",
		Content: anthropic.String("duplicate"),
	}); err == nil {
		t.Fatal("expected duplicate memory path to fail")
	}

	updatedMemory, err := client.Beta.MemoryStores.Memories.Update(ctx, memory.ID, anthropic.BetaMemoryStoreMemoryUpdateParams{
		MemoryStoreID: store.ID,
		Path:          anthropic.String("/go-sdk/renamed.md"),
		Content:       anthropic.String("updated from go sdk memory"),
		View:          anthropic.BetaManagedAgentsMemoryViewFull,
		Precondition: anthropic.BetaManagedAgentsPreconditionParam{
			Type:          anthropic.BetaManagedAgentsPreconditionTypeContentSha256,
			ContentSha256: anthropic.String(memory.ContentSha256),
		},
	})
	if err != nil {
		t.Fatalf("update memory: %v", err)
	}
	if updatedMemory.ID != memory.ID || updatedMemory.Path != "/go-sdk/renamed.md" || updatedMemory.Content != "updated from go sdk memory" || updatedMemory.MemoryVersionID == firstVersionID {
		t.Fatalf("unexpected updated memory: %+v", updatedMemory)
	}

	retrievedMemory, err := client.Beta.MemoryStores.Memories.Get(ctx, memory.ID, anthropic.BetaMemoryStoreMemoryGetParams{MemoryStoreID: store.ID})
	if err != nil {
		t.Fatalf("retrieve memory: %v", err)
	}
	if retrievedMemory.Content != updatedMemory.Content || retrievedMemory.ContentSha256 != updatedMemory.ContentSha256 {
		t.Fatalf("unexpected retrieved memory: %+v", retrievedMemory)
	}

	memories, err := client.Beta.MemoryStores.Memories.List(ctx, store.ID, anthropic.BetaMemoryStoreMemoryListParams{
		PathPrefix: anthropic.String("/go-sdk/"),
		Limit:      anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	if !goSDKMemoryPageContains(memories.Data, memory.ID) {
		t.Fatalf("memory %s not found in list", memory.ID)
	}

	versions, err := client.Beta.MemoryStores.MemoryVersions.List(ctx, store.ID, anthropic.BetaMemoryStoreMemoryVersionListParams{
		MemoryID: anthropic.String(memory.ID),
		Limit:    anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if !goSDKVersionPageContains(versions.Data, anthropic.BetaManagedAgentsMemoryVersionOperationCreated) ||
		!goSDKVersionPageContains(versions.Data, anthropic.BetaManagedAgentsMemoryVersionOperationModified) {
		t.Fatalf("version list missing created/modified: %+v", versions.Data)
	}

	firstVersion, err := client.Beta.MemoryStores.MemoryVersions.Get(ctx, firstVersionID, anthropic.BetaMemoryStoreMemoryVersionGetParams{
		MemoryStoreID: store.ID,
		View:          anthropic.BetaManagedAgentsMemoryViewFull,
	})
	if err != nil {
		t.Fatalf("retrieve version: %v", err)
	}
	if firstVersion.Content != memory.Content {
		t.Fatalf("first version content = %q, want %q", firstVersion.Content, memory.Content)
	}

	redacted, err := client.Beta.MemoryStores.MemoryVersions.Redact(ctx, firstVersionID, anthropic.BetaMemoryStoreMemoryVersionRedactParams{MemoryStoreID: store.ID})
	if err != nil {
		t.Fatalf("redact version: %v", err)
	}
	if redacted.RedactedAt.IsZero() || redacted.ContentSha256 != "" || redacted.Path != "" {
		t.Fatalf("unexpected redacted version: %+v", redacted)
	}

	deletedMemory, err := client.Beta.MemoryStores.Memories.Delete(ctx, memory.ID, anthropic.BetaMemoryStoreMemoryDeleteParams{
		MemoryStoreID:         store.ID,
		ExpectedContentSha256: anthropic.String(updatedMemory.ContentSha256),
	})
	if err != nil {
		t.Fatalf("delete memory: %v", err)
	}
	if deletedMemory.ID != memory.ID || deletedMemory.Type != anthropic.BetaManagedAgentsDeletedMemoryTypeMemoryDeleted {
		t.Fatalf("unexpected deleted memory: %+v", deletedMemory)
	}

	deletedStore, err := client.Beta.MemoryStores.Delete(ctx, store.ID, anthropic.BetaMemoryStoreDeleteParams{})
	if err != nil {
		t.Fatalf("delete store: %v", err)
	}
	if deletedStore.ID != store.ID || deletedStore.Type != anthropic.BetaManagedAgentsDeletedMemoryStoreTypeMemoryStoreDeleted {
		t.Fatalf("unexpected deleted store: %+v", deletedStore)
	}
}

func goSDKStorePageContains(stores []anthropic.BetaManagedAgentsMemoryStore, id string) bool {
	for _, store := range stores {
		if store.ID == id {
			return true
		}
	}
	return false
}

func goSDKMemoryPageContains(memories []anthropic.BetaManagedAgentsMemoryListItemUnion, id string) bool {
	for _, memory := range memories {
		if memory.Type == "memory" && memory.ID == id {
			return true
		}
	}
	return false
}

func goSDKVersionPageContains(versions []anthropic.BetaManagedAgentsMemoryVersion, operation anthropic.BetaManagedAgentsMemoryVersionOperation) bool {
	for _, version := range versions {
		if version.Operation == operation {
			return true
		}
	}
	return false
}

package sessionresource

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRejectClientMemoryIdentityFields(t *testing.T) {
	t.Parallel()

	if err := RejectClientMemoryIdentityFields(nil, nil, nil); err != nil {
		t.Fatalf("omitted fields: %v", err)
	}

	for _, test := range []struct {
		name        string
		mountPath   json.RawMessage
		storeName   json.RawMessage
		description json.RawMessage
	}{
		{name: "mount_path", mountPath: json.RawMessage(`"/mnt/memory/custom"`)},
		{name: "name", storeName: json.RawMessage(`"memory"`)},
		{name: "description", description: json.RawMessage(`"copied"`)},
		{name: "null name", storeName: json.RawMessage(`null`)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := RejectClientMemoryIdentityFields(test.mountPath, test.storeName, test.description)
			if !errors.Is(err, ErrMemoryStoreClientIdentity) {
				t.Fatalf("error = %v, want ErrMemoryStoreClientIdentity", err)
			}
		})
	}
}

func TestParseMemoryAccess(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "omitted", want: MemoryAccessReadWrite},
		{name: "null", raw: json.RawMessage(`null`), want: MemoryAccessReadWrite},
		{name: "read_only", raw: json.RawMessage(`"read_only"`), want: MemoryAccessReadOnly},
		{name: "read_write", raw: json.RawMessage(`"read_write"`), want: MemoryAccessReadWrite},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMemoryAccess(test.raw)
			if err != nil {
				t.Fatalf("ParseMemoryAccess() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ParseMemoryAccess() = %q, want %q", got, test.want)
			}
		})
	}

	t.Run("rejects rw", func(t *testing.T) {
		t.Parallel()
		_, err := ParseMemoryAccess(json.RawMessage(`"rw"`))
		if !errors.Is(err, ErrMemoryStoreAccess) {
			t.Fatalf("error = %v, want ErrMemoryStoreAccess", err)
		}
	})
}

func TestParseMemoryInstructions(t *testing.T) {
	t.Parallel()

	got, err := ParseMemoryInstructions(nil)
	if err != nil || got != "" {
		t.Fatalf("omitted = (%q, %v), want empty", got, err)
	}

	got, err = ParseMemoryInstructions(json.RawMessage(`""`))
	if err != nil || got != "" {
		t.Fatalf("empty = (%q, %v), want empty", got, err)
	}

	limit := strings.Repeat("i", MaxMemoryInstructionsRunes)
	got, err = ParseMemoryInstructions(json.RawMessage(`"` + limit + `"`))
	if err != nil || got != limit {
		t.Fatalf("500 runes = (%d, %v)", utf8.RuneCountInString(got), err)
	}

	emoji := strings.Repeat("😀", MaxMemoryInstructionsRunes)
	got, err = ParseMemoryInstructions(mustJSONString(t, emoji))
	if err != nil || got != emoji {
		t.Fatalf("500 emoji = (%d, %v)", utf8.RuneCountInString(got), err)
	}

	_, err = ParseMemoryInstructions(json.RawMessage(`"` + strings.Repeat("i", MaxMemoryInstructionsRunes+1) + `"`))
	if !errors.Is(err, ErrMemoryStoreInstructionsTooLong) {
		t.Fatalf("501 runes error = %v, want ErrMemoryStoreInstructionsTooLong", err)
	}

	_, err = ParseMemoryInstructions(mustJSONString(t, strings.Repeat("😀", MaxMemoryInstructionsRunes+1)))
	if !errors.Is(err, ErrMemoryStoreInstructionsTooLong) {
		t.Fatalf("501 emoji error = %v, want ErrMemoryStoreInstructionsTooLong", err)
	}

	_, err = ParseMemoryInstructions(json.RawMessage(`42`))
	if err == nil {
		t.Fatal("non-string instructions succeeded")
	}
}

func TestSlugifyMemoryName(t *testing.T) {
	t.Parallel()

	if got := SlugifyMemoryName("Product Docs-Draft!!", "memstore_fallback"); got != "product-docs-draft" {
		t.Fatalf("display name slug = %q, want product-docs-draft", got)
	}

	fallback := "memstore_abcXYZ"
	got := SlugifyMemoryName("!!!", fallback)
	if got != "memstore-abcxyz" {
		t.Fatalf("all-symbol name slug = %q, want memstore-abcxyz", got)
	}
}

func TestMemoryAttachSetAssignsUniqueSlugsAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	set := NewMemoryAttachSet()
	first, err := set.Add("memstore_one", "Product Docs-Draft!!", "memstore_one")
	if err != nil || first != "product-docs-draft" {
		t.Fatalf("first slug = (%q, %v), want product-docs-draft", first, err)
	}
	second, err := set.Add("memstore_two", "Product Docs-Draft", "memstore_two")
	if err != nil || second != "product-docs-draft-2" {
		t.Fatalf("second slug = (%q, %v), want product-docs-draft-2", second, err)
	}

	if _, err := set.Add("memstore_one", "other", "memstore_one"); !errors.Is(err, ErrMemoryStoreDuplicate) {
		t.Fatalf("duplicate id error = %v, want ErrMemoryStoreDuplicate", err)
	}

	limit := NewMemoryAttachSet()
	for i := 0; i < MaxMemoryStores; i++ {
		id := "memstore_" + strings.Repeat("a", i+1)
		if _, err := limit.Add(id, id, id); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}
	if _, err := limit.Add("memstore_overflow", "overflow", "memstore_overflow"); !errors.Is(err, ErrMemoryStoreLimit) {
		t.Fatalf("ninth store error = %v, want ErrMemoryStoreLimit", err)
	}

	claimed := NewMemoryAttachSet()
	if err := claimed.Claim("memstore_one"); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := claimed.Claim("memstore_one"); !errors.Is(err, ErrMemoryStoreDuplicate) {
		t.Fatalf("Claim() duplicate error = %v, want ErrMemoryStoreDuplicate", err)
	}
}

func TestObserveStoredMemoryResourceReservesSlug(t *testing.T) {
	t.Parallel()

	set := NewMemoryAttachSet()
	ObserveStoredMemoryResource(set, FileType, json.RawMessage(`{"memory_store_id":"memstore_ignored"}`))
	ObserveStoredMemoryResource(set, MemoryStoreType, json.RawMessage(
		`{"memory_store_id":"memstore_one","mount_path":"/mnt/memory/product-docs-draft"}`,
	))

	slug, err := set.Add("memstore_two", "Product Docs-Draft!!", "memstore_two")
	if err != nil || slug != "product-docs-draft-2" {
		t.Fatalf("slug after observe = (%q, %v), want product-docs-draft-2", slug, err)
	}
	if _, err := set.Add("memstore_one", "other", "memstore_one"); !errors.Is(err, ErrMemoryStoreDuplicate) {
		t.Fatalf("observed id error = %v, want ErrMemoryStoreDuplicate", err)
	}
}

func TestMemorySnapshotPayloadFields(t *testing.T) {
	t.Parallel()

	snapshot := MemorySnapshot{
		MemoryStoreID: "memstore_one",
		Access:        MemoryAccessReadWrite,
		Instructions:  "remember this",
		Name:          "Product Docs-Draft!!",
		Description:   "personal taste",
		MountPath:     MemoryMountPath("product-docs-draft"),
	}
	fields := snapshot.PayloadFields("sesrsc_one")
	if fields["type"] != MemoryStoreType ||
		fields["memory_store_id"] != "memstore_one" ||
		fields["access"] != MemoryAccessReadWrite ||
		fields["instructions"] != "remember this" ||
		fields["name"] != "Product Docs-Draft!!" ||
		fields["description"] != "personal taste" ||
		fields["mount_path"] != "/mnt/memory/product-docs-draft" ||
		fields["id"] != "sesrsc_one" {
		t.Fatalf("payload fields = %#v", fields)
	}
}

func mustJSONString(t *testing.T, value string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal string: %v", err)
	}
	return raw
}

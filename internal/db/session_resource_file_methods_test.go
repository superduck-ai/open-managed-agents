package db

import "testing"

func TestSessionResourceFileOwnershipIsExplicit(t *testing.T) {
	t.Parallel()

	size := int64(17)
	referenced := SessionResourceFile{
		Kind:          SessionResourceFileKindFile,
		SizeBytes:     &size,
		FileOwnership: SessionResourceFileOwnershipReferenced,
	}
	if !referenced.ReferencesSourceFile() || referenced.OwnsFile() || referenced.OwnedBytes() != 0 {
		t.Fatalf("referenced File ownership = references %t owns %t bytes %d", referenced.ReferencesSourceFile(), referenced.OwnsFile(), referenced.OwnedBytes())
	}

	owned := SessionResourceFile{
		Kind:          SessionResourceFileKindFile,
		SizeBytes:     &size,
		FileOwnership: SessionResourceFileOwnershipOwned,
	}
	if owned.ReferencesSourceFile() || !owned.OwnsFile() || owned.OwnedBytes() != size {
		t.Fatalf("owned File ownership = references %t owns %t bytes %d", owned.ReferencesSourceFile(), owned.OwnsFile(), owned.OwnedBytes())
	}

	for name, entry := range map[string]SessionResourceFile{
		"missing ownership":  {Kind: SessionResourceFileKindFile, SizeBytes: &size},
		"archive cannot own": {Kind: SessionResourceFileKindArchive, SizeBytes: &size, FileOwnership: SessionResourceFileOwnershipOwned},
	} {
		t.Run(name, func(t *testing.T) {
			if entry.ReferencesSourceFile() || entry.OwnsFile() || entry.OwnedBytes() != 0 {
				t.Fatalf("invalid ownership was treated as actionable: %#v", entry)
			}
		})
	}
}

package filestore

import "testing"

func TestDirectoryCursorRoundTripAndScope(t *testing.T) {
	raw, err := encodeDirectoryCursor(directoryCursor{
		FilesystemID: "fs_test",
		Path:         "/reports",
		LastPath:     "/reports/z.txt",
		LastID:       44,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	cursor, err := decodeDirectoryCursor(raw, "fs_test", "/reports", false)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cursor.LastPath != "/reports/z.txt" || cursor.LastID != 44 {
		t.Fatalf("cursor = %#v", cursor)
	}
	if _, err := decodeDirectoryCursor(raw, "fs_other", "/reports", false); err == nil {
		t.Fatal("cursor was accepted for another filesystem")
	}
}

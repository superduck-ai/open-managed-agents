package db

// BorrowsSourceObject reports whether the entry references an object owned and
// accounted for by the Files API. The entry remains a mutable node in the
// Filestore logical view, but deleting or replacing it must not delete or
// decrement usage for the borrowed object.
func (entry *FilestoreEntry) BorrowsSourceObject() bool {
	return entry != nil && entry.SourceFileUUID != nil
}

// OwnedBytes returns the bytes that this entry contributes to Filestore-owned
// storage accounting. Borrowed Files API objects remain visible in the
// namespace, but they do not consume Filestore-owned bytes.
func (entry *FilestoreEntry) OwnedBytes() int64 {
	if entry == nil || entry.BorrowsSourceObject() {
		return 0
	}
	return filestoreInt64(entry.SizeBytes)
}

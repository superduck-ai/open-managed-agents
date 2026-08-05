package db

// ReferencesSourceFile 表示节点是 Input Resource，它引用由 Files API
// 拥有和计费的 Source File。这类节点不能通过通用 Filestore mutation
// 删除或替换，也不能删除 Source File 对象或扣减其用量。
func (entry *SessionResourceFile) ReferencesSourceFile() bool {
	return entry != nil && entry.SourceFileUUID != nil
}

// OwnedBytes 返回该节点计入 Session Owned File 的字节数。
// 只有普通 File 节点拥有对象；Input Resource 和 Skill Archive Resource
// 都只引用其他资源拥有的对象。
func (entry *SessionResourceFile) OwnedBytes() int64 {
	if entry == nil ||
		entry.Kind != SessionResourceFileKindFile ||
		entry.ReferencesSourceFile() {
		return 0
	}
	return filestoreInt64(entry.SizeBytes)
}

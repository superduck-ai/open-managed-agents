package db

var filestoreFixedRootPaths = [...]string{
	"/outputs",
	"/skills",
	"/uploads",
	"/transcripts",
	"/tool_results",
}

// filestoreFixedRootForPath 返回路径所属的固定顶层命名空间。固定根本身也属于该命名空间。
func filestoreFixedRootForPath(entryPath string) (string, bool) {
	for _, rootPath := range filestoreFixedRootPaths {
		if entryPath == rootPath || filestorePathIsDescendant(rootPath, entryPath) {
			return rootPath, true
		}
	}
	return "", false
}

// validateFilestoreDirectoryMoveRoots 拒绝会破坏固定顶层命名空间语义的目录移动。
//
// 它先判断源路径和目标路径是否位于 `/outputs`、`/skills`、`/uploads`、`/transcripts`、`/tool_results`
// 这些固定根之一。
// 它不允许把固定根目录本身改名或搬到别处。
// 它也不允许目录跨固定根移动、离开固定根，或从普通路径移入固定根。
// 这样可以保证这些系统保留命名空间始终稳定，避免调用方把受约束目录树伪装成普通目录，
// 或把普通目录塞进受特殊语义保护的区域。
//
// 成功时返回 nil，表示这次移动在固定根边界上是允许的。
// 失败时返回 ErrFilestoreInvalidMove。
// 这个函数本身不访问数据库、不开事务、也不加锁；它只是事务开始前的纯路径校验。
// 真正的原子移动、冲突检查和并发控制由 MoveFilestoreDirectory 在后续事务里完成。
//
// 例子：
//   - 成功：`/outputs/drafts` → `/outputs/published`。目录仍留在同一个固定根内。
//   - 失败：`/outputs` → `/outputs-renamed`。固定根本身不能被改名。
//   - 失败：`/archive` → `/uploads/archive`。普通目录不能移入固定根。
func validateFilestoreDirectoryMoveRoots(sourcePath, destinationPath string) error {
	sourceRoot, sourceIsScoped := filestoreFixedRootForPath(sourcePath)
	destinationRoot, destinationIsScoped := filestoreFixedRootForPath(destinationPath)
	// 不能直接移动固定根本身, 也就是下面这种不允许：
	//
	//• /outputs → /outputs-renamed
	//• /uploads → /archive/uploads
	if sourcePath == sourceRoot || destinationPath == destinationRoot {
		return ErrFilestoreInvalidMove
	}
	// 不能跨固定根命名空间移动
	// • 要么 source 和 destination 都不在任何 fixed root 里
	// • 要么它们都在同一个 fixed root 里
	// 否则就拒绝。
	if sourceIsScoped != destinationIsScoped || sourceRoot != destinationRoot {
		return ErrFilestoreInvalidMove
	}
	return nil
}

func validateFilestoreDirectoryRemovalRoot(entryPath string) error {
	rootPath, scoped := filestoreFixedRootForPath(entryPath)
	if scoped && entryPath == rootPath {
		return ErrFilestoreInvalidMove
	}
	return nil
}

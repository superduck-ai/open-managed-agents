package db

var filestoreFixedRootPaths = [...]string{
	"/outputs",
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

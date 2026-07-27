package filestore

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type readOperation uint8

const (
	readOperationListDirectory readOperation = iota
	readOperationFile
	readOperationMetadata
)

// pathBackend 只抽象不同命名空间共享的读取能力。
// 普通 Filestore 的写入仍由 Service 编排，避免把对象存储和数据库事务塞进虚拟读取接口。
type pathBackend interface {
	listDirectory(
		context.Context,
		Principal,
		db.FilestoreFilesystem,
		listDirectoryRequest,
		directoryCursor,
		int,
	) (listDirectoryResponse, *apiError)
	readFile(
		context.Context,
		Principal,
		db.FilestoreFilesystem,
		readFileRequest,
	) (readFileResult, *apiError)
	readMetadata(
		context.Context,
		Principal,
		db.FilestoreFilesystem,
		string,
	) (entryPayload, *apiError)
}

// readOnlyPathBackend 表示覆盖普通持久化视图的只读虚拟命名空间。
// backend 自己决定哪些读取由它处理；router 统一阻止对整棵命名空间的修改。
type readOnlyPathBackend interface {
	pathBackend
	namespaceRoot() string
	matchesRead(readOperation, string) bool
	containsPath(string) bool
}

type pathRouter struct {
	persistent pathBackend
	readOnly   []readOnlyPathBackend
}

func (r pathRouter) backendFor(operation readOperation, value string) pathBackend {
	for _, backend := range r.readOnly {
		if backend.matchesRead(operation, value) {
			return backend
		}
	}
	return r.persistent
}

func (r pathRouter) authorizeMutation(paths ...string) *apiError {
	for _, value := range paths {
		for _, backend := range r.readOnly {
			if backend.containsPath(value) {
				return permissionDenied("the " + backend.namespaceRoot() + " namespace is read-only")
			}
		}
	}
	return nil
}

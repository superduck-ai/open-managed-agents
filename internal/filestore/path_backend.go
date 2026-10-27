package filestore

import (
	"context"
	"io"

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

// writablePathBackend 接管可写虚拟命名空间的操作；普通写入仍留在 Service。
type writablePathBackend interface {
	pathBackend
	makeDirectory(context.Context, Principal, db.FilestoreFilesystem, makeDirectoryRequest) (directoryResponse, *apiError)
	removeDirectory(context.Context, Principal, db.FilestoreFilesystem, removeDirectoryRequest) *apiError
	createFile(context.Context, Principal, db.FilestoreFilesystem, createFileParams, io.Reader) (fileResponse, *apiError)
	removeFile(context.Context, Principal, db.FilestoreFilesystem, pathRequest) *apiError
	copyFile(context.Context, Principal, db.FilestoreFilesystem, copyMoveFileRequest) (fileResponse, *apiError)
	moveFile(context.Context, Principal, db.FilestoreFilesystem, copyMoveFileRequest) (fileResponse, *apiError)
}

type mutationOperation uint8

const (
	mutationSinglePath mutationOperation = iota
	mutationFileTransfer
	mutationDirectoryTransfer
)

type pathRouter struct {
	persistent pathBackend
	memory     writablePathBackend
	readOnly   []readOnlyPathBackend
}

func (r pathRouter) backendFor(operation readOperation, value string) pathBackend {
	for _, backend := range r.readOnly {
		if backend.matchesRead(operation, value) {
			return backend
		}
	}
	if r.memory != nil {
		if _, claimed := parseMemoryFilestorePath(value); claimed {
			return r.memory
		}
	}
	return r.persistent
}

// mutationBackendFor 先保护只读命名空间，再解析写入归属和传输边界。
// nil backend 表示由 Service 继续普通持久化写入；所有错误都禁止继续写入。
func (r pathRouter) mutationBackendFor(operation mutationOperation, paths ...string) (writablePathBackend, *apiError) {
	if apiErr := r.authorizeMutation(paths...); apiErr != nil {
		return nil, apiErr
	}
	if operation == mutationSinglePath {
		if _, claimed := parseMemoryFilestorePath(paths[0]); claimed {
			return r.memory, nil
		}
		return nil, nil
	}
	_, _, sameStore, claimed := classifyMemoryTransfer(paths[0], paths[1])
	if !claimed {
		return nil, nil
	}
	if operation == mutationDirectoryTransfer {
		return nil, memoryDirectoryMoveError()
	}
	if !sameStore {
		return nil, memoryTransferBoundaryError()
	}
	return r.memory, nil
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

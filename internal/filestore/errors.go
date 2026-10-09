package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var (
	errNotFound           = errors.New("filestore resource not found")
	errFailedPrecondition = errors.New("filestore precondition failed")
)

type apiError struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *apiError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *apiError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func invalidArgument(message string) *apiError {
	return &apiError{Status: http.StatusBadRequest, Code: "invalid_argument", Message: message}
}

func permissionDenied(message string) *apiError {
	return &apiError{Status: http.StatusForbidden, Code: "permission_denied", Message: message}
}

func memoryTransferBoundaryError() *apiError {
	return invalidArgument("cannot copy or move across the memory namespace boundary")
}

func memoryDirectoryMoveError() *apiError {
	return failedPrecondition("memory namespace directories are virtual")
}

func duplicateMemoryMountError() *apiError {
	return failedPrecondition("multiple memory mounts match the requested slug")
}

func writeFilestoreError(w http.ResponseWriter, err *apiError) {
	if err == nil {
		err = &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "Internal server error"}
	}
	WriteProtocolError(w, err.Status, err.Code, err.Message)
}

// WriteProtocolError 写出 rclone-filestore 线协议约定的扁平错误结构。
// API 层鉴权可能先于 Filestore Handler 失败，因此将此函数公开给路由中间件复用，
// 保证所有失败路径都呈现同一种协议外观。
func WriteProtocolError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}

func mapMemoryMutationError(operation string, err error) *apiError {
	var conflict *db.MemoryPathConflictError
	if errors.As(err, &conflict) {
		return &apiError{Status: http.StatusConflict, Code: "already_exists", Message: "memory path conflicts with an existing file or directory", Cause: err}
	}
	switch {
	case errors.Is(err, db.ErrInvalidState):
		return permissionDenied("memory store is archived")
	case errors.Is(err, db.ErrLimitExceeded):
		return &apiError{Status: http.StatusForbidden, Code: "resource_exhausted", Message: "Memory store item limit exceeded"}
	default:
		return mapDatabaseError(operation, err)
	}
}

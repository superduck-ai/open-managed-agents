package storage

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/smithy-go"
)

var (
	ErrNotFound     = errors.New("object not found")
	ErrAccessDenied = errors.New("object storage access denied")
	ErrConflict     = errors.New("object storage conflict")
)

// StoreError 保留 S3 的原始错误，并通过 errors.Is 暴露稳定的存储错误分类。
type StoreError struct {
	Operation string
	Bucket    string
	Key       string
	Code      string
	Kind      error
	Cause     error
}

func (e *StoreError) Error() string {
	target := e.Bucket
	if e.Key != "" {
		target += "/" + e.Key
	}
	if e.Code != "" {
		return fmt.Sprintf("object storage %s %s (%s): %v", e.Operation, target, e.Code, e.Cause)
	}
	return fmt.Sprintf("object storage %s %s: %v", e.Operation, target, e.Cause)
}

func (e *StoreError) Unwrap() error {
	return e.Cause
}

func (e *StoreError) Is(target error) bool {
	return (e.Kind != nil && target == e.Kind) || errors.Is(e.Cause, target)
}

func normalizeOperationError(operation, bucket, key string, err error) error {
	if err == nil || errors.Is(err, ErrInvalidKey) || errors.Is(err, ErrInvalidRange) || errors.Is(err, ErrInvalidDeleteOptions) {
		return err
	}
	kind, code := classifyStoreError(err)
	return &StoreError{Operation: operation, Bucket: bucket, Key: key, Code: code, Kind: kind, Cause: err}
}

func classifyStoreError(err error) (error, string) {
	status, _ := httpStatusCode(err)
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		// S3 兼容实现的 HTTP 状态较一致，错误码可能不同；先识别错误码，再以状态码兜底。
		code := apiError.ErrorCode()
		if kind := storeErrorKindForCode(code); kind != nil {
			return kind, code
		}
		return storeErrorKindForHTTPStatus(status), code
	}
	return storeErrorKindForHTTPStatus(status), ""
}

func storeErrorKindForCode(code string) error {
	switch strings.ToLower(code) {
	case "nosuchbucket", "nosuchkey", "nosuchversion", "notfound", "404":
		return ErrNotFound
	case "accessdenied", "forbidden", "invalidaccesskeyid", "signaturedoesnotmatch", "unauthorized", "401", "403":
		return ErrAccessDenied
	case "bucketalreadyexists", "bucketalreadyownedbyyou", "conflict", "operationaborted", "preconditionfailed", "409", "412":
		return ErrConflict
	case "invalidrange", "requestedrangenotsatisfiable", "416":
		return ErrInvalidRange
	default:
		return nil
	}
}

func storeErrorKindForHTTPStatus(status int) error {
	switch status {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAccessDenied
	case http.StatusConflict, http.StatusPreconditionFailed:
		return ErrConflict
	case http.StatusRequestedRangeNotSatisfiable:
		return ErrInvalidRange
	default:
		return nil
	}
}

package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/go-chi/chi/v5"
)

const (
	maxFilestoreJSONBody = 1 << 20
	filestoreTransferTTL = 31 * time.Minute
)

// Handler 将 rclone-filestore 协议映射到业务服务，并统一请求上限、传输超时与错误外观。
type Handler struct {
	cfg     config.Config
	service serviceAPI
	router  chi.Router
}

type serviceAPI interface {
	ListDirectory(context.Context, Principal, listDirectoryRequest) (listDirectoryResponse, *apiError)
	MakeDirectory(context.Context, Principal, makeDirectoryRequest) (directoryResponse, *apiError)
	RemoveDirectory(context.Context, Principal, removeDirectoryRequest) *apiError
	CreateFile(context.Context, Principal, createFileParams, io.Reader) (fileResponse, *apiError)
	CopyFile(context.Context, Principal, copyMoveFileRequest) (fileResponse, *apiError)
	MoveFile(context.Context, Principal, copyMoveFileRequest) (fileResponse, *apiError)
	MoveDirectory(context.Context, Principal, moveDirectoryRequest) (directoryResponse, *apiError)
	ReadFile(context.Context, Principal, readFileRequest) (readFileResult, *apiError)
	RemoveFile(context.Context, Principal, pathRequest) *apiError
	ReadMetadata(context.Context, Principal, pathRequest) (entryPayload, *apiError)
}

// NewHandler 构造 Filestore HTTP 边界，并只注册协议明确支持的操作。
// 未知路径与错误方法统一返回 Filestore 错误结构，不泄漏 chi 的默认响应。
func NewHandler(cfg config.Config, service serviceAPI) *Handler {
	h := &Handler{cfg: cfg, service: service}
	router := chi.NewRouter()
	router.NotFound(h.notFound)
	router.MethodNotAllowed(h.notFound)
	router.Post("/listDirectory", h.listDirectory)
	router.Post("/readFile", h.readFile)
	router.Post("/readMetadata", h.readMetadata)
	router.Group(func(r chi.Router) {
		// 只把写保护挂到已知的变更操作；未知路径仍稳定返回 not_found。
		r.Use(h.requireWritableToken)
		r.Post("/makeDirectory", h.makeDirectory)
		r.Post("/removeDirectory", h.removeDirectory)
		r.Post("/createFile", h.createFile)
		r.Post("/copyFile", h.copyFile)
		r.Post("/moveFile", h.moveFile)
		r.Post("/moveDirectory", h.moveDirectory)
		r.Post("/removeFile", h.removeFile)
	})
	h.router = router
	return h
}

func (h *Handler) requireWritableToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, apiErr := authenticatedPrincipal(r)
		if apiErr != nil {
			writeFilestoreError(w, apiErr)
			return
		}
		if principal.Readonly {
			writeFilestoreError(w, permissionDenied("Filestore token is read-only"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ServeHTTP 实现 http.Handler，并将未预期的 panic 收敛为稳定的协议错误。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("filestore panic: %v", recovered)
			writeFilestoreError(w, &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "Internal server error"})
		}
	}()
	h.router.ServeHTTP(w, r)
}

func (h *Handler) listDirectory(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[listDirectoryRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	response, apiErr := h.service.ListDirectory(r.Context(), principal, request)
	writeFilestoreResult(w, response, apiErr)
}

func (h *Handler) makeDirectory(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[makeDirectoryRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	response, apiErr := h.service.MakeDirectory(r.Context(), principal, request)
	writeFilestoreResult(w, response, apiErr)
}

func (h *Handler) removeDirectory(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[removeDirectoryRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	apiErr = h.service.RemoveDirectory(r.Context(), principal, request)
	writeFilestoreResult(w, struct{}{}, apiErr)
}

func (h *Handler) createFile(w http.ResponseWriter, r *http.Request) {
	principal, apiErr := authenticatedPrincipal(r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	extendFilestoreDeadlines(w)
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.Storage.MaxFileBytes+maxFilestoreJSONBody)
	reader, err := r.MultipartReader()
	if err != nil {
		writeFilestoreError(w, invalidArgument("Expected multipart form data"))
		return
	}
	paramsPart, err := reader.NextPart()
	if err != nil || paramsPart.FormName() != "params" {
		writeFilestoreError(w, invalidArgument("Missing required multipart field: params"))
		return
	}
	paramsData, err := io.ReadAll(io.LimitReader(paramsPart, maxFilestoreJSONBody+1))
	_ = paramsPart.Close()
	if err != nil || len(paramsData) > maxFilestoreJSONBody {
		writeFilestoreError(w, invalidArgument("Invalid multipart params"))
		return
	}
	var params createFileParams
	if err := decodeStrictJSON(paramsData, &params); err != nil {
		writeFilestoreError(w, invalidArgument("Invalid multipart params: "+err.Error()))
		return
	}
	filePart, err := reader.NextPart()
	if err != nil || filePart.FormName() != "file" {
		writeFilestoreError(w, invalidArgument("Missing required multipart field: file"))
		return
	}
	defer filePart.Close()
	// params 必须先于 file，服务层才能在读取大文件前完成路径、配额上下文等校验。
	response, apiErr := h.service.CreateFile(r.Context(), principal, params, filePart)
	writeFilestoreResult(w, response, apiErr)
}

func (h *Handler) copyFile(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[copyMoveFileRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	response, apiErr := h.service.CopyFile(r.Context(), principal, request)
	writeFilestoreResult(w, response, apiErr)
}

func (h *Handler) moveFile(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[copyMoveFileRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	response, apiErr := h.service.MoveFile(r.Context(), principal, request)
	writeFilestoreResult(w, response, apiErr)
}

func (h *Handler) moveDirectory(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[moveDirectoryRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	response, apiErr := h.service.MoveDirectory(r.Context(), principal, request)
	writeFilestoreResult(w, response, apiErr)
}

func (h *Handler) readFile(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[readFileRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	extendFilestoreDeadlines(w)
	result, apiErr := h.service.ReadFile(r.Context(), principal, request)
	if apiErr != nil {
		logFilestoreRequestError(apiErr)
		writeFilestoreError(w, apiErr)
		return
	}
	if result.Body != nil {
		defer result.Body.Close()
	}
	if result.MediaType != "" {
		w.Header().Set("Content-Type", result.MediaType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if result.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(result.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if result.Body == nil {
		return
	}
	var err error
	if result.Size >= 0 {
		_, err = io.CopyN(w, result.Body, result.Size)
	} else {
		// 对象存储可以合法地省略 Content-Length；此时持续读取至 EOF，
		// 不能把未知长度 -1 交给 io.CopyN，避免成功打开的对象被截断为空响应。
		_, err = io.Copy(w, result.Body)
	}
	if err != nil {
		// 响应头发出后已无法改写为 JSON 错误，只记录流中断供服务端诊断。
		log.Printf("stream filestore object: %v", err)
	}
}

func (h *Handler) removeFile(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[pathRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	apiErr = h.service.RemoveFile(r.Context(), principal, request)
	writeFilestoreResult(w, struct{}{}, apiErr)
}

func (h *Handler) readMetadata(w http.ResponseWriter, r *http.Request) {
	principal, request, apiErr := decodeAuthenticatedJSON[pathRequest](w, r)
	if apiErr != nil {
		writeFilestoreError(w, apiErr)
		return
	}
	response, apiErr := h.service.ReadMetadata(r.Context(), principal, request)
	writeFilestoreResult(w, response, apiErr)
}

func decodeAuthenticatedJSON[T any](w http.ResponseWriter, r *http.Request) (Principal, T, *apiError) {
	principal, apiErr := authenticatedPrincipal(r)
	if apiErr != nil {
		return Principal{}, *new(T), apiErr
	}
	if apiErr := requireJSONContentType(r); apiErr != nil {
		return Principal{}, *new(T), apiErr
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFilestoreJSONBody)
	// 小型控制请求一次读完后严格解码；文件内容走 multipart 流，不进入此内存上限。
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return Principal{}, *new(T), invalidArgument("Invalid JSON request")
	}
	var request T
	if err := decodeStrictJSON(data, &request); err != nil {
		return Principal{}, request, invalidArgument("Invalid JSON request: " + err.Error())
	}
	return principal, request, nil
}

func authenticatedPrincipal(r *http.Request) (Principal, *apiError) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return Principal{}, &apiError{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "Missing bearer token"}
	}
	return principal, nil
}

func writeFilestoreResult(w http.ResponseWriter, value any, apiErr *apiError) {
	if apiErr != nil {
		logFilestoreRequestError(apiErr)
		writeFilestoreError(w, apiErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode filestore response: %v", err)
	}
}

func logFilestoreRequestError(apiErr *apiError) {
	if apiErr != nil && apiErr.Status >= http.StatusInternalServerError {
		log.Printf("filestore request failed: %v", apiErr)
	}
}

func extendFilestoreDeadlines(w http.ResponseWriter) {
	controller := http.NewResponseController(w)
	// 全局 HTTP 超时仍是最后防线；这里只为大对象上传下载提供协议允许的传输窗口。
	deadline := time.Now().Add(filestoreTransferTTL)
	if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Printf("set filestore read deadline: %v", err)
	}
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Printf("set filestore write deadline: %v", err)
	}
}

func (h *Handler) notFound(w http.ResponseWriter, _ *http.Request) {
	writeFilestoreError(w, &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "Not found"})
}

func requireJSONContentType(r *http.Request) *apiError {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(r.Header.Get("Content-Type")))
	if err != nil || mediaType != "application/json" {
		return invalidArgument("Content-Type must be application/json")
	}
	return nil
}

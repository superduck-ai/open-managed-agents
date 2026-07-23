package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestHandlerReturnsFlatUnauthorizedErrorWithoutPrincipal(t *testing.T) {
	t.Parallel()

	service := &fakeFilestoreService{}
	handler := NewHandler(config.Config{}, service)
	request := httptest.NewRequest(http.MethodPost, "/readMetadata", strings.NewReader(`{"filesystemId":"fs_test","path":"/a"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertFlatHandlerError(t, recorder, http.StatusUnauthorized, "unauthenticated", "Missing bearer token")
	if len(service.calls) != 0 {
		t.Fatalf("service calls = %v", service.calls)
	}
}

func TestHandlerEnforcesReadonlyFilestoreToken(t *testing.T) {
	t.Parallel()

	readonlyPrincipal := Principal{
		OrganizationID: 11,
		WorkspaceID:    22,
		AccountID:      33,
		Readonly:       true,
	}
	for _, path := range []string{
		"/makeDirectory",
		"/removeDirectory",
		"/createFile",
		"/copyFile",
		"/moveFile",
		"/moveDirectory",
		"/removeFile",
	} {
		path := path
		t.Run("reject_"+strings.TrimPrefix(path, "/"), func(t *testing.T) {
			t.Parallel()
			service := &fakeFilestoreService{}
			handler := NewHandler(config.Config{}, service)
			request := newHandlerRequestWithPrincipal(http.MethodPost, path, http.NoBody, readonlyPrincipal)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertFlatHandlerError(t, recorder, http.StatusForbidden, "permission_denied", "Filestore token is read-only")
			if len(service.calls) != 0 {
				t.Fatalf("service calls = %v", service.calls)
			}
		})
	}

	for _, test := range []struct {
		path     string
		wantCall string
	}{
		{path: "/listDirectory", wantCall: "ListDirectory"},
		{path: "/readFile", wantCall: "ReadFile"},
		{path: "/readMetadata", wantCall: "ReadMetadata"},
	} {
		test := test
		t.Run("allow_"+strings.TrimPrefix(test.path, "/"), func(t *testing.T) {
			t.Parallel()
			service := &fakeFilestoreService{}
			handler := NewHandler(config.Config{}, service)
			request := newHandlerRequestWithPrincipal(
				http.MethodPost,
				test.path,
				strings.NewReader(`{"filesystemId":"fs_test","path":"/"}`),
				readonlyPrincipal,
			)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
			}
			if len(service.calls) != 1 || service.calls[0] != test.wantCall {
				t.Fatalf("service calls = %v, want [%s]", service.calls, test.wantCall)
			}
		})
	}
}

func TestHandlerRejectsInvalidJSONRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		wantMessage string
	}{
		{
			name:        "unknown field",
			contentType: "application/json",
			body:        `{"filesystemId":"fs_test","path":"/a","unknown":true}`,
			wantMessage: "Invalid JSON request: json: unknown field \"unknown\"",
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        `{"filesystemId":"fs_test","path":"/a"}`,
			wantMessage: "Content-Type must be application/json",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeFilestoreService{}
			handler := NewHandler(config.Config{}, service)
			request := newAuthenticatedHandlerRequest(http.MethodPost, "/readMetadata", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertFlatHandlerError(t, recorder, http.StatusBadRequest, "invalid_argument", test.wantMessage)
			if len(service.calls) != 0 {
				t.Fatalf("service calls = %v", service.calls)
			}
		})
	}
}

func TestHandlerRoutesAllFilestorePOSTOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		wantCall string
		newBody  func(*testing.T) (io.Reader, string)
	}{
		{name: "list directory", path: "/listDirectory", wantCall: "ListDirectory", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","path":"/"}`)},
		{name: "make directory", path: "/makeDirectory", wantCall: "MakeDirectory", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","path":"/dir"}`)},
		{name: "remove directory", path: "/removeDirectory", wantCall: "RemoveDirectory", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","path":"/dir"}`)},
		{name: "create file", path: "/createFile", wantCall: "CreateFile", newBody: multipartHandlerBody(`{"filesystemId":"fs_test","path":"/a.txt","mediaType":"text/plain"}`, "contents")},
		{name: "copy file", path: "/copyFile", wantCall: "CopyFile", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","source":"/a","destination":"/b"}`)},
		{name: "move file", path: "/moveFile", wantCall: "MoveFile", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","source":"/a","destination":"/b"}`)},
		{name: "move directory", path: "/moveDirectory", wantCall: "MoveDirectory", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","source":"/a","destination":"/b"}`)},
		{name: "read file", path: "/readFile", wantCall: "ReadFile", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","path":"/a"}`)},
		{name: "remove file", path: "/removeFile", wantCall: "RemoveFile", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","path":"/a"}`)},
		{name: "read metadata", path: "/readMetadata", wantCall: "ReadMetadata", newBody: jsonHandlerBody(`{"filesystemId":"fs_test","path":"/a"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeFilestoreService{}
			handler := NewHandler(filestoreTestConfig(1024, 0, ""), service)
			body, contentType := test.newBody(t)
			request := newAuthenticatedHandlerRequest(http.MethodPost, test.path, body)
			request.Header.Set("Content-Type", contentType)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if len(service.calls) != 1 || service.calls[0] != test.wantCall {
				t.Fatalf("service calls = %v, want [%s]", service.calls, test.wantCall)
			}
		})
	}
}

func TestHandlerEncodesProtoInt64ResponseAsString(t *testing.T) {
	t.Parallel()

	service := &fakeFilestoreService{listResponse: listDirectoryResponse{
		Entries: []entryPayload{{File: &filesystemFilePayload{
			File:         filePayload{UUID: "file-1", Size: protoInt64(42)},
			FilesystemID: "fs_test",
			Path:         "/a.txt",
		}}},
	}}
	handler := NewHandler(config.Config{}, service)
	request := newAuthenticatedHandlerRequest(http.MethodPost, "/listDirectory", strings.NewReader(`{"filesystemId":"fs_test","path":"/"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Entries []struct {
			File struct {
				File struct {
					Size any `json:"size"`
				} `json:"file"`
			} `json:"file"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Entries) != 1 || response.Entries[0].File.File.Size != "42" {
		t.Fatalf("response = %s", recorder.Body.String())
	}
}

func TestHandlerStreamsReadFileBodyWhenSizeIsUnknown(t *testing.T) {
	t.Parallel()

	body := &trackingReadCloser{Reader: strings.NewReader("streamed")}
	service := &fakeFilestoreService{readResult: readFileResult{
		Body: body,
		Size: -1,
	}}
	handler := NewHandler(config.Config{}, service)
	request := newAuthenticatedHandlerRequest(http.MethodPost, "/readFile", strings.NewReader(`{"filesystemId":"fs_test","path":"/a.bin"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, want omitted", got)
	}
	if got := recorder.Body.String(); got != "streamed" {
		t.Fatalf("body = %q", got)
	}
	if !body.closed {
		t.Fatal("read body was not closed")
	}
}

func TestHandlerStreamsReadFileBodyAndMetadata(t *testing.T) {
	t.Parallel()

	body := &trackingReadCloser{Reader: strings.NewReader("hello")}
	service := &fakeFilestoreService{readResult: readFileResult{
		Body:      body,
		Size:      5,
		MediaType: "text/plain",
	}}
	handler := NewHandler(config.Config{}, service)
	request := newAuthenticatedHandlerRequest(http.MethodPost, "/readFile", strings.NewReader(`{"filesystemId":"fs_test","path":"/a.txt"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != "5" {
		t.Fatalf("Content-Length = %q", got)
	}
	if got := recorder.Body.String(); got != "hello" {
		t.Fatalf("body = %q", got)
	}
	if !body.closed {
		t.Fatal("read body was not closed")
	}
}

func TestHandlerPassesCreateFileMultipartParamsAndStream(t *testing.T) {
	t.Parallel()

	service := &fakeFilestoreService{}
	handler := NewHandler(filestoreTestConfig(1024, 0, ""), service)
	paramsJSON := `{"filesystemId":"fs_test","path":"/reports/a.txt","metadata":{"source":"test"},"mediaType":"text/plain","tags":["report"],"overwriteExisting":true,"ttlSeconds":"60"}`
	body, contentType := multipartHandlerBody(paramsJSON, "streamed file contents")(t)
	request := newAuthenticatedHandlerRequest(http.MethodPost, "/createFile", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.createParams.FilesystemID != "fs_test" || service.createParams.Path != "/reports/a.txt" {
		t.Fatalf("params = %+v", service.createParams)
	}
	if service.createParams.MediaType != "text/plain" || service.createParams.TTLSeconds != 60 {
		t.Fatalf("params = %+v", service.createParams)
	}
	if got := service.createParams.Metadata["source"]; got != "test" {
		t.Fatalf("metadata source = %#v", got)
	}
	if got := string(service.createBody); got != "streamed file contents" {
		t.Fatalf("file body = %q", got)
	}
	if service.createPrincipal.WorkspaceID != 22 || service.createPrincipal.OrganizationID != 11 {
		t.Fatalf("principal = %+v", service.createPrincipal)
	}
}

func newAuthenticatedHandlerRequest(method, target string, body io.Reader) *http.Request {
	principal := Principal{OrganizationID: 11, WorkspaceID: 22, AccountID: 33}
	return newHandlerRequestWithPrincipal(method, target, body, principal)
}

func newHandlerRequestWithPrincipal(method, target string, body io.Reader, principal Principal) *http.Request {
	request := httptest.NewRequest(method, target, body)
	return request.WithContext(WithPrincipal(request.Context(), principal))
}

func jsonHandlerBody(body string) func(*testing.T) (io.Reader, string) {
	return func(*testing.T) (io.Reader, string) {
		return strings.NewReader(body), "application/json"
	}
}

func multipartHandlerBody(params, contents string) func(*testing.T) (io.Reader, string) {
	return func(t *testing.T) (io.Reader, string) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		paramsPart, err := writer.CreateFormField("params")
		if err != nil {
			t.Fatalf("create params part: %v", err)
		}
		if _, err := io.WriteString(paramsPart, params); err != nil {
			t.Fatalf("write params part: %v", err)
		}
		filePart, err := writer.CreateFormFile("file", "upload.txt")
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := io.WriteString(filePart, contents); err != nil {
			t.Fatalf("write file part: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close multipart writer: %v", err)
		}
		return bytes.NewReader(body.Bytes()), writer.FormDataContentType()
	}
}

func assertFlatHandlerError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, status, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 2 || response["code"] != code || response["message"] != message {
		t.Fatalf("response = %#v", response)
	}
	if _, nested := response["error"]; nested {
		t.Fatalf("response unexpectedly contains nested error: %#v", response)
	}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type fakeFilestoreService struct {
	calls           []string
	listResponse    listDirectoryResponse
	readResult      readFileResult
	createParams    createFileParams
	createBody      []byte
	createPrincipal Principal
}

func (s *fakeFilestoreService) ListDirectory(
	context.Context,
	Principal,
	listDirectoryRequest,
) (listDirectoryResponse, *apiError) {
	s.calls = append(s.calls, "ListDirectory")
	return s.listResponse, nil
}

func (s *fakeFilestoreService) MakeDirectory(
	context.Context,
	Principal,
	makeDirectoryRequest,
) (directoryResponse, *apiError) {
	s.calls = append(s.calls, "MakeDirectory")
	return directoryResponse{}, nil
}

func (s *fakeFilestoreService) RemoveDirectory(context.Context, Principal, removeDirectoryRequest) *apiError {
	s.calls = append(s.calls, "RemoveDirectory")
	return nil
}

func (s *fakeFilestoreService) CreateFile(
	_ context.Context,
	principal Principal,
	params createFileParams,
	body io.Reader,
) (fileResponse, *apiError) {
	s.calls = append(s.calls, "CreateFile")
	s.createPrincipal = principal
	s.createParams = params
	contents, err := io.ReadAll(body)
	if err != nil {
		return fileResponse{}, &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: err.Error()}
	}
	s.createBody = contents
	return fileResponse{}, nil
}

func (s *fakeFilestoreService) CopyFile(
	context.Context,
	Principal,
	copyMoveFileRequest,
) (fileResponse, *apiError) {
	s.calls = append(s.calls, "CopyFile")
	return fileResponse{}, nil
}

func (s *fakeFilestoreService) MoveFile(
	context.Context,
	Principal,
	copyMoveFileRequest,
) (fileResponse, *apiError) {
	s.calls = append(s.calls, "MoveFile")
	return fileResponse{}, nil
}

func (s *fakeFilestoreService) MoveDirectory(
	context.Context,
	Principal,
	moveDirectoryRequest,
) (directoryResponse, *apiError) {
	s.calls = append(s.calls, "MoveDirectory")
	return directoryResponse{}, nil
}

func (s *fakeFilestoreService) ReadFile(
	context.Context,
	Principal,
	readFileRequest,
) (readFileResult, *apiError) {
	s.calls = append(s.calls, "ReadFile")
	return s.readResult, nil
}

func (s *fakeFilestoreService) RemoveFile(context.Context, Principal, pathRequest) *apiError {
	s.calls = append(s.calls, "RemoveFile")
	return nil
}

func (s *fakeFilestoreService) ReadMetadata(
	context.Context,
	Principal,
	pathRequest,
) (entryPayload, *apiError) {
	s.calls = append(s.calls, "ReadMetadata")
	return entryPayload{}, nil
}

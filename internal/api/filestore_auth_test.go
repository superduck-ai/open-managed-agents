package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
)

var filestoreAuthPaths = []string{
	"/v1/filestore/fs/listDirectory",
	"/v1/filestore/fs/makeDirectory",
	"/v1/filestore/fs/removeDirectory",
	"/v1/filestore/fs/createFile",
	"/v1/filestore/fs/copyFile",
	"/v1/filestore/fs/moveFile",
	"/v1/filestore/fs/moveDirectory",
	"/v1/filestore/fs/readFile",
	"/v1/filestore/fs/removeFile",
	"/v1/filestore/fs/readMetadata",
}

type filestoreAuthCredentials struct {
	ingress   *codesessions.SessionCredentials
	filestore *filestore.TokenCredentials
}

type filestoreAuthFixture struct {
	tokenIdentity         filestore.TokenIdentity
	filesystemUUID        string
	organizationID        int64
	workspaceID           int64
	sessionExternalID     string
	ingressIdentity       codesessions.SessionCredentialIdentity
	workspaceAPIKey       string
	codeSessionCredential string
}

func TestFilestoreRoutingUsesResourceBoundary(t *testing.T) {
	t.Parallel()

	credentials := newFilestoreAuthCredentials(t)
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials.ingress,
		FilestoreCredentials:   credentials.filestore,
	})
	for _, requestPath := range append([]string{
		"/v1/filestore",
		"/v1/filestore/",
		"/v1/filestore/fs",
		"/v1/filestore/fs/unknownOperation",
	}, filestoreAuthPaths...) {
		requestPath := requestPath
		t.Run("filestore_"+strings.NewReplacer("/", "_", " ", "space").Replace(requestPath), func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, requestPath, nil))
			assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
		})
	}

	for _, requestPath := range []string{
		"/v1/filestores",
		"/v1/filestores/fs/listDirectory",
		"/v1/filestore-backup",
		"/v1/filestor",
		"/v1/messages",
	} {
		requestPath := requestPath
		t.Run("ordinary_v1_"+strings.NewReplacer("/", "_", " ", "space").Replace(requestPath), func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, requestPath, nil))
			if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"type":"authentication_error"`) {
				t.Fatalf("response = %d %s, want ordinary v1 authentication envelope", response.Code, response.Body.String())
			}
		})
	}
}

func TestFilestoreProtocolOperationsAreRegistered(t *testing.T) {
	t.Parallel()

	handler := filestore.NewHandler(config.Config{}, filestore.NewService(config.Config{}, nil, nil))
	for _, requestPath := range filestoreAuthPaths {
		requestPath := requestPath
		t.Run(strings.TrimPrefix(requestPath, "/v1/filestore/fs/"), func(t *testing.T) {
			t.Parallel()
			handlerPath := strings.TrimPrefix(requestPath, "/v1/filestore/fs")
			request := httptest.NewRequest(http.MethodPost, handlerPath, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("POST %s status = %d, want registered handler authentication response %d: %s", requestPath, response.Code, http.StatusUnauthorized, response.Body.String())
			}
		})
	}
}

func TestFilestoreAuthMiddlewareWritesFlatErrors(t *testing.T) {
	t.Parallel()

	credentials := newFilestoreAuthCredentials(t)
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials.ingress,
		FilestoreCredentials:   credentials.filestore,
	})
	for _, requestPath := range []string{
		"/v1/filestore",
		"/v1/filestore/unknown",
		filestoreAuthPaths[0],
	} {
		requestPath := requestPath
		t.Run(strings.TrimPrefix(requestPath, "/v1/filestore"), func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, requestPath, nil)
			response := httptest.NewRecorder()

			server.ServeHTTP(response, request)

			assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
		})
	}
}

func TestAuthenticateFilestoreOwnsProtocolError(t *testing.T) {
	t.Parallel()

	server := &Server{}
	for _, test := range []struct {
		name        string
		request     *http.Request
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "missing bearer token",
			request:     httptest.NewRequest(http.MethodPost, filestoreAuthPaths[0], nil),
			wantStatus:  http.StatusUnauthorized,
			wantCode:    "unauthenticated",
			wantMessage: "Invalid bearer token",
		},
		{
			name: "authentication dependency unavailable",
			request: newFilestoreBearerRequest(
				http.MethodPost,
				filestoreAuthPaths[0],
				"not-a-jwt",
			),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "internal",
			wantMessage: "Authentication failed",
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, authErr := server.authenticateFilestore(test.request)
			if authErr == nil {
				t.Fatal("authenticateFilestore() error = nil")
			}
			if authErr.status != test.wantStatus ||
				authErr.code != test.wantCode ||
				authErr.message != test.wantMessage {
				t.Fatalf(
					"authenticateFilestore() error = %#v, want status=%d code=%q message=%q",
					authErr,
					test.wantStatus,
					test.wantCode,
					test.wantMessage,
				)
			}
		})
	}
}

func TestFilestoreJWTAuthentication(t *testing.T) {
	database, cfg, fixture := newFilestoreAuthDatabaseFixture(t)
	credentials := newFilestoreAuthCredentials(t)
	token, err := credentials.filestore.Issue(fixture.tokenIdentity)
	if err != nil {
		t.Fatalf("issue valid filestore token: %v", err)
	}

	server := NewServer(ServerDeps{
		Config:                 cfg,
		DB:                     database,
		CodeSessionCredentials: credentials.ingress,
		FilestoreCredentials:   credentials.filestore,
	})

	for _, requestPath := range filestoreAuthPaths {
		// 鉴权通过后进入真实 Handler；缺请求体/Content-Type 时由协议层返回 invalid_argument，而非中间件的 unauthenticated。
		response := serveFilestoreAuthRequest(server, http.MethodPost, requestPath, token)
		assertFilestoreHandlerReachedAfterAuth(t, response)
	}

	t.Run("middleware injects filestore principal", func(t *testing.T) {
		var principal filestore.Principal
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := filestore.PrincipalFromContext(r.Context())
			if !ok {
				http.Error(w, "principal missing", http.StatusInternalServerError)
				return
			}
			if _, leaked := auth.PrincipalFromContext(r.Context()); leaked {
				http.Error(w, "filestore principal leaked into global auth context", http.StatusInternalServerError)
				return
			}
			principal = got
			w.WriteHeader(http.StatusNoContent)
		})
		request := newFilestoreBearerRequest(http.MethodPost, filestoreAuthPaths[0], token)
		response := httptest.NewRecorder()

		server.filestoreAuthMiddleware(next).ServeHTTP(response, request)

		if response.Code != http.StatusNoContent {
			t.Fatalf("middleware status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
		}
		assertFilestoreTokenPrincipal(t, principal, fixture.tokenIdentity, fixture.filesystemUUID, fixture.tokenIdentity.FilesystemID)
	})

	t.Run("uuid claim is canonicalized in principal", func(t *testing.T) {
		identity := fixture.tokenIdentity
		identity.FilesystemID = strings.ToUpper(fixture.filesystemUUID)
		uuidToken, issueErr := credentials.filestore.Issue(identity)
		if issueErr != nil {
			t.Fatalf("issue UUID-scoped token: %v", issueErr)
		}
		var principal filestore.Principal
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, _ = filestore.PrincipalFromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		})
		response := httptest.NewRecorder()

		server.filestoreAuthMiddleware(next).ServeHTTP(
			response,
			newFilestoreBearerRequest(http.MethodPost, filestoreAuthPaths[0], uuidToken),
		)

		if response.Code != http.StatusNoContent {
			t.Fatalf("middleware status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
		}
		assertFilestoreTokenPrincipal(t, principal, identity, fixture.filesystemUUID, fixture.tokenIdentity.FilesystemID)
	})

	t.Run("readonly claim reaches principal", func(t *testing.T) {
		readonlyToken, issueErr := credentials.filestore.IssueReadonly(fixture.tokenIdentity)
		if issueErr != nil {
			t.Fatalf("issue readonly token: %v", issueErr)
		}
		var principal filestore.Principal
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, _ = filestore.PrincipalFromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		})
		response := httptest.NewRecorder()

		server.filestoreAuthMiddleware(next).ServeHTTP(
			response,
			newFilestoreBearerRequest(http.MethodPost, filestoreAuthPaths[0], readonlyToken),
		)

		if response.Code != http.StatusNoContent || !principal.Readonly {
			t.Fatalf("response/principal = %d/%#v, want authenticated readonly principal", response.Code, principal)
		}
	})

	t.Run("write prefixes reach principal", func(t *testing.T) {
		scopedIdentity := fixture.tokenIdentity
		scopedIdentity.WritePrefixes = []string{"/outputs"}
		scopedToken, issueErr := credentials.filestore.Issue(scopedIdentity)
		if issueErr != nil {
			t.Fatalf("issue path-scoped token: %v", issueErr)
		}
		var principal filestore.Principal
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, _ = filestore.PrincipalFromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		})
		response := httptest.NewRecorder()

		server.filestoreAuthMiddleware(next).ServeHTTP(
			response,
			newFilestoreBearerRequest(http.MethodPost, filestoreAuthPaths[0], scopedToken),
		)

		if response.Code != http.StatusNoContent || !slices.Equal(principal.WritePrefixes, []string{"/outputs"}) {
			t.Fatalf("response/principal = %d/%#v, want scoped principal", response.Code, principal)
		}
	})

	t.Run("failure token signed by another key", func(t *testing.T) {
		otherCredentials := newFilestoreAuthCredentials(t)
		wrongToken, issueErr := otherCredentials.filestore.Issue(fixture.tokenIdentity)
		if issueErr != nil {
			t.Fatalf("issue wrong-key token: %v", issueErr)
		}
		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], wrongToken)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure malformed jwt", func(t *testing.T) {
		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], "not-a-jwt")
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure session ingress token is not a filestore token", func(t *testing.T) {
		ingressToken, issueErr := credentials.ingress.Issue(fixture.ingressIdentity)
		if issueErr != nil {
			t.Fatalf("issue session ingress token: %v", issueErr)
		}
		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], ingressToken)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure valid filestore token in x-api-key header", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, filestoreAuthPaths[0], nil)
		request.Header.Set("X-Api-Key", token)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure workspace api key is not a filestore credential", func(t *testing.T) {
		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], fixture.workspaceAPIKey)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure code session credential is not a filestore credential", func(t *testing.T) {
		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], fixture.codeSessionCredential)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	for _, test := range []struct {
		name   string
		mutate func(*filestore.TokenIdentity)
	}{
		{name: "account", mutate: func(identity *filestore.TokenIdentity) { identity.AccountUUID = uuid.NewString() }},
		{name: "workspace", mutate: func(identity *filestore.TokenIdentity) { identity.WorkspaceUUID = uuid.NewString() }},
		{name: "workspace tagged id", mutate: func(identity *filestore.TokenIdentity) { identity.WorkspaceTaggedID = "workspace_other" }},
		{name: "filesystem", mutate: func(identity *filestore.TokenIdentity) { identity.FilesystemID = "fs_other" }},
		{name: "organization taints", mutate: func(identity *filestore.TokenIdentity) { identity.OrgTaints = []string{"restricted"} }},
		{name: "workspace cmek", mutate: func(identity *filestore.TokenIdentity) { identity.WorkspaceCMEKEnabled = false }},
	} {
		test := test
		t.Run("failure mismatched "+test.name, func(t *testing.T) {
			mismatched := fixture.tokenIdentity
			test.mutate(&mismatched)
			wrongToken, issueErr := credentials.filestore.Issue(mismatched)
			if issueErr != nil {
				t.Fatalf("issue mismatched token: %v", issueErr)
			}
			response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], wrongToken)
			assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
		})
	}

	t.Run("failure database organization policy change revokes existing token", func(t *testing.T) {
		organization, getErr := database.GetPlatformOrganization(context.Background(), fixture.tokenIdentity.OrgUUID)
		if getErr != nil {
			t.Fatalf("load organization policy: %v", getErr)
		}
		if _, updateErr := database.UpdatePlatformOrganization(
			context.Background(),
			fixture.tokenIdentity.OrgUUID,
			platform.OrganizationUpdatePatch{Settings: map[string]any{"org_taints": []string{"changed"}}},
		); updateErr != nil {
			t.Fatalf("change organization policy: %v", updateErr)
		}
		defer func() {
			_, restoreErr := database.UpdatePlatformOrganization(
				context.Background(),
				fixture.tokenIdentity.OrgUUID,
				platform.OrganizationUpdatePatch{Settings: map[string]any{"org_taints": organization.Settings["org_taints"]}},
			)
			if restoreErr != nil {
				t.Errorf("restore organization policy: %v", restoreErr)
			}
		}()

		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], token)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure database workspace cmek change revokes existing token", func(t *testing.T) {
		workspace, getErr := database.GetAdminWorkspace(
			context.Background(),
			fixture.organizationID,
			fixture.tokenIdentity.WorkspaceTaggedID,
		)
		if getErr != nil {
			t.Fatalf("load workspace policy: %v", getErr)
		}
		next := workspace
		next.ExternalKeyID = nil
		next.UpdatedAt = time.Now().UTC()
		if _, updateErr := database.UpdateAdminWorkspace(
			context.Background(),
			fixture.organizationID,
			workspace.ExternalID,
			next,
		); updateErr != nil {
			t.Fatalf("change workspace CMEK policy: %v", updateErr)
		}
		defer func() {
			workspace.UpdatedAt = time.Now().UTC()
			if _, restoreErr := database.UpdateAdminWorkspace(
				context.Background(),
				fixture.organizationID,
				workspace.ExternalID,
				workspace,
			); restoreErr != nil {
				t.Errorf("restore workspace CMEK policy: %v", restoreErr)
			}
		}()

		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], token)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("middleware rejects invalid filestore token with bearer wording", func(t *testing.T) {
		otherCredentials := newFilestoreAuthCredentials(t)
		wrongToken, issueErr := otherCredentials.filestore.Issue(fixture.tokenIdentity)
		if issueErr != nil {
			t.Fatalf("issue wrong-key token: %v", issueErr)
		}
		response := httptest.NewRecorder()
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		server.filestoreAuthMiddleware(next).ServeHTTP(
			response,
			newFilestoreBearerRequest(http.MethodPost, filestoreAuthPaths[0], wrongToken),
		)

		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	t.Run("failure real filesystem from another workspace", func(t *testing.T) {
		suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
		otherWorkspaceUUID := uuid.NewString()
		otherWorkspaceExternalID := "workspace_filestore_cross_" + suffix
		otherSessionUUID := uuid.NewString()
		otherSessionExternalID := "sesn_filestore_cross_" + suffix
		otherFilesystemUUID := uuid.NewString()
		otherFilesystemExternalID := "fs_filestore_cross_" + suffix

		var organizationID, otherWorkspaceID int64
		if err := database.Pool.QueryRow(context.Background(), `
			select id from organizations where uuid = $1
		`, fixture.tokenIdentity.OrgUUID).Scan(&organizationID); err != nil {
			t.Fatalf("load fixture organization: %v", err)
		}
		if err := database.Pool.QueryRow(context.Background(), `
			insert into workspaces (uuid, external_id, organization_id, name)
			values ($1, $2, $3, $4)
			returning id
		`, otherWorkspaceUUID, otherWorkspaceExternalID, organizationID, "Filestore cross-workspace test").Scan(&otherWorkspaceID); err != nil {
			t.Fatalf("insert other workspace: %v", err)
		}
		defer func() {
			_, _ = database.Pool.Exec(context.Background(), `delete from filestore_filesystems where uuid = $1`, otherFilesystemUUID)
			_, _ = database.Pool.Exec(context.Background(), `delete from sessions where uuid = $1`, otherSessionUUID)
			_, _ = database.Pool.Exec(context.Background(), `delete from workspaces where id = $1`, otherWorkspaceID)
		}()
		if _, err := database.Pool.Exec(context.Background(), `
			insert into sessions (
				uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
				environment_id, environment_external_id, agent_id, agent_external_id,
				agent_version, agent_snapshot, status
			)
			values ($1, $2, $3, $4, 0, 0, $5, 0, $6, 1, '{}'::jsonb, 'running')
		`, otherSessionUUID, otherSessionExternalID, organizationID, otherWorkspaceID,
			"env_filestore_cross_"+suffix, "agent_filestore_cross_"+suffix); err != nil {
			t.Fatalf("insert other workspace session: %v", err)
		}
		if _, err := database.Pool.Exec(context.Background(), `
			insert into filestore_filesystems (
				uuid, external_id, organization_uuid, workspace_uuid, session_uuid
			)
			values ($1, $2, $3, $4, $5)
		`, otherFilesystemUUID, otherFilesystemExternalID, fixture.tokenIdentity.OrgUUID,
			otherWorkspaceUUID, otherSessionUUID); err != nil {
			t.Fatalf("insert other workspace filesystem: %v", err)
		}

		crossWorkspaceIdentity := fixture.tokenIdentity
		crossWorkspaceIdentity.FilesystemID = otherFilesystemExternalID
		crossWorkspaceToken, issueErr := credentials.filestore.Issue(crossWorkspaceIdentity)
		if issueErr != nil {
			t.Fatalf("issue cross-workspace token: %v", issueErr)
		}
		response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], crossWorkspaceToken)
		assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
	})

	for _, test := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "trailing slash", method: http.MethodPost, path: filestoreAuthPaths[0] + "/"},
		{name: "child path", method: http.MethodPost, path: filestoreAuthPaths[0] + "/child"},
		{name: "wrong method", method: http.MethodGet, path: filestoreAuthPaths[0]},
		{name: "unknown operation", method: http.MethodPost, path: "/v1/filestore/unknown"},
	} {
		t.Run(test.name+" authenticates before protocol rejection", func(t *testing.T) {
			response := serveFilestoreAuthRequest(server, test.method, test.path, token)
			assertFlatFilestoreAuthError(t, response, http.StatusNotFound, "not_found", "Not found")
		})
	}

	for _, test := range []struct {
		name    string
		mutate  string
		restore string
	}{
		{
			name:    "archived session",
			mutate:  "update sessions set archived_at = now() where workspace_id = $1 and external_id = $2",
			restore: "update sessions set archived_at = null where workspace_id = $1 and external_id = $2",
		},
		{
			name:    "terminated session",
			mutate:  "update sessions set status = 'terminated' where workspace_id = $1 and external_id = $2",
			restore: "update sessions set status = 'running' where workspace_id = $1 and external_id = $2",
		},
		{
			name:    "deleted session",
			mutate:  "update sessions set deleted_at = now() where workspace_id = $1 and external_id = $2",
			restore: "update sessions set deleted_at = null where workspace_id = $1 and external_id = $2",
		},
	} {
		t.Run("failure "+test.name+" revokes existing token", func(t *testing.T) {
			if _, err := database.Pool.Exec(context.Background(), test.mutate, fixture.workspaceID, fixture.sessionExternalID); err != nil {
				t.Fatalf("mutate Session lifecycle: %v", err)
			}
			defer func() {
				if _, err := database.Pool.Exec(context.Background(), test.restore, fixture.workspaceID, fixture.sessionExternalID); err != nil {
					t.Errorf("restore Session lifecycle: %v", err)
				}
			}()

			response := serveFilestoreAuthRequest(server, http.MethodPost, filestoreAuthPaths[0], token)
			assertFlatFilestoreAuthError(t, response, http.StatusUnauthorized, "unauthenticated", "Invalid bearer token")
		})
	}

	t.Run("failure token cannot access messages", func(t *testing.T) {
		request := newFilestoreBearerRequest(http.MethodPost, "/v1/messages", token)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf("messages status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"type":"authentication_error"`) {
			t.Fatalf("messages body = %q, want authentication_error", response.Body.String())
		}
	})
}

func newFilestoreAuthDatabaseFixture(t *testing.T) (*db.DB, config.Config, filestoreAuthFixture) {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	database, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open filestore auth database: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		database.Close()
		t.Fatalf("migrate filestore auth database: %v", err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	organizationUUID := uuid.NewString()
	organizationExternalID := "org_filestore_auth_" + suffix
	workspaceUUID := uuid.NewString()
	workspaceExternalID := "workspace_filestore_auth_" + suffix
	accountUUID := uuid.NewString()
	accountExternalID := "user_filestore_auth_" + suffix
	publicSessionID := "sesn_filestore_auth_" + suffix
	codeSessionID := "cse_filestore_auth_" + suffix
	filesystemID := "fs_filestore_auth_" + suffix
	filesystemUUID := uuid.NewString()
	agentID := "agent_filestore_auth_" + suffix
	environmentID := "env_filestore_auth_" + suffix
	workspaceAPIKey := "sk-ant-api03-filestore-auth-" + suffix
	codeSessionCredential := "sk-ant-oat01-filestore-auth-" + suffix

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = database.Pool.Exec(ctx, `delete from filestore_filesystems where external_id = $1`, filesystemID)
		_, _ = database.Pool.Exec(ctx, `delete from code_sessions where external_id = $1`, codeSessionID)
		_, _ = database.Pool.Exec(ctx, `delete from sessions where external_id = $1`, publicSessionID)
		_, _ = database.Pool.Exec(ctx, `delete from api_keys where key_hash = $1`, auth.HashAPIKey(workspaceAPIKey))
		_, _ = database.Pool.Exec(ctx, `delete from users where external_id = $1`, accountExternalID)
		_, _ = database.Pool.Exec(ctx, `delete from workspaces where external_id = $1`, workspaceExternalID)
		_, _ = database.Pool.Exec(ctx, `delete from organizations where external_id = $1`, organizationExternalID)
		database.Close()
	})

	var organizationID int64
	if err := database.Pool.QueryRow(context.Background(), `
		insert into organizations (uuid, external_id, name, settings)
		values ($1, $2, $3, '{"org_taints":["restricted","compliance"]}'::jsonb)
		returning id
	`, organizationUUID, organizationExternalID, "Filestore auth test").Scan(&organizationID); err != nil {
		t.Fatalf("insert filestore auth organization: %v", err)
	}
	var workspaceID int64
	if err := database.Pool.QueryRow(context.Background(), `
		insert into workspaces (uuid, external_id, organization_id, name, external_key_id)
		values ($1, $2, $3, $4, 'key_filestore_auth')
		returning id
	`, workspaceUUID, workspaceExternalID, organizationID, "Filestore auth test").Scan(&workspaceID); err != nil {
		t.Fatalf("insert filestore auth workspace: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `
		insert into api_keys (external_id, workspace_id, key_hash, status)
		values ($1, $2, $3, 'active')
	`, "api_key_filestore_auth_"+suffix, workspaceID, auth.HashAPIKey(workspaceAPIKey)); err != nil {
		t.Fatalf("insert filestore auth API key: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `
		insert into users (uuid, external_id, organization_id, email, name, role)
		values ($1, $2, $3, $4, $5, 'developer')
	`, accountUUID, accountExternalID, organizationID, accountExternalID+"@example.com", "Filestore auth account"); err != nil {
		t.Fatalf("insert filestore auth account: %v", err)
	}
	sessionUUID := uuid.NewString()
	var sessionID int64
	if err := database.Pool.QueryRow(context.Background(), `
		insert into sessions (
			uuid, external_id, organization_id, workspace_id,
			created_by_api_key_id, environment_id, environment_external_id,
			agent_id, agent_external_id, agent_version, agent_snapshot,
			title, status
		)
		values ($1, $2, $3, $4, 0, 0, $5, 0, $6, 1, '{}'::jsonb, $7, 'running')
		returning id
	`, sessionUUID, publicSessionID, organizationID, workspaceID, environmentID, agentID, "Filestore auth test").Scan(&sessionID); err != nil {
		t.Fatalf("insert filestore auth session: %v", err)
	}
	codeSessionUUID := uuid.NewString()
	if _, err := database.Pool.Exec(context.Background(), `
		insert into code_sessions (
			uuid, external_id, organization_id, workspace_id,
			session_id, session_external_id, environment_id,
			environment_external_id, status, oauth_access_token_hash,
			worker_lease_expires_at
		)
		values ($1, $2, $3, $4, $5, $6, 0, $7, 'active', $8, now() + interval '1 hour')
	`, codeSessionUUID, codeSessionID, organizationID, workspaceID, sessionID, publicSessionID, environmentID,
		auth.HashAPIKey(codeSessionCredential)); err != nil {
		t.Fatalf("insert filestore auth code session: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `
		insert into filestore_filesystems (
			uuid, external_id, organization_uuid, workspace_uuid,
			session_uuid, code_session_uuid
		)
		values ($1, $2, $3, $4, $5, $6)
	`, filesystemUUID, filesystemID, organizationUUID, workspaceUUID, sessionUUID, codeSessionUUID); err != nil {
		t.Fatalf("insert filestore auth filesystem: %v", err)
	}

	return database, cfg, filestoreAuthFixture{
		filesystemUUID:    filesystemUUID,
		organizationID:    organizationID,
		workspaceID:       workspaceID,
		sessionExternalID: publicSessionID,
		tokenIdentity: filestore.TokenIdentity{
			Subject:                   accountExternalID,
			OrgUUID:                   organizationUUID,
			AccountUUID:               accountUUID,
			WorkspaceUUID:             workspaceUUID,
			WorkspaceTaggedID:         workspaceExternalID,
			ResolvedWorkspaceTaggedID: workspaceExternalID,
			FilesystemID:              filesystemID,
			OrgTaints:                 []string{"compliance", "restricted"},
			WorkspaceCMEKEnabled:      true,
		},
		ingressIdentity: codesessions.SessionCredentialIdentity{
			SessionID:        codeSessionID,
			PublicSessionID:  publicSessionID,
			AgentID:          agentID,
			AgentVersion:     1,
			OrganizationUUID: organizationUUID,
			WorkspaceUUID:    workspaceUUID,
		},
		workspaceAPIKey:       workspaceAPIKey,
		codeSessionCredential: codeSessionCredential,
	}
}

func newFilestoreAuthCredentials(t *testing.T) filestoreAuthCredentials {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate filestore auth signing key: %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal filestore auth signing key: %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "code-session-ed25519.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if err := os.WriteFile(keyFile, data, 0o600); err != nil {
		t.Fatalf("write filestore auth signing key: %v", err)
	}
	credentialConfig := config.Config{
		Env: config.EnvironmentProd,
		CodeSession: config.CodeSessionConfig{
			JWTSigningPrivateKeyFile: keyFile,
		},
	}
	ingressCredentials, err := codesessions.NewSessionCredentials(credentialConfig)
	if err != nil {
		t.Fatalf("create session ingress credentials: %v", err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(credentialConfig)
	if err != nil {
		t.Fatalf("create filestore auth credentials: %v", err)
	}
	return filestoreAuthCredentials{ingress: ingressCredentials, filestore: filestoreCredentials}
}

func newFilestoreBearerRequest(method, requestPath, token string) *http.Request {
	request := httptest.NewRequest(method, requestPath, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func serveFilestoreAuthRequest(server http.Handler, method, requestPath, token string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.ServeHTTP(response, newFilestoreBearerRequest(method, requestPath, token))
	return response
}

func assertFlatFilestoreAuthError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()

	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode filestore error: %v: %s", err, response.Body.String())
	}
	if len(payload) != 2 || payload["code"] != code || payload["message"] != message {
		t.Fatalf("filestore error = %#v, want flat code=%q message=%q", payload, code, message)
	}
	if _, exists := payload["error"]; exists {
		t.Fatalf("filestore error unexpectedly contains nested error: %#v", payload)
	}
	if _, exists := payload["type"]; exists {
		t.Fatalf("filestore error unexpectedly contains type envelope: %#v", payload)
	}
}

// assertFilestoreHandlerReachedAfterAuth 断言请求已越过鉴权中间件，到达真实 Filestore Handler。
// 空请求体在不同协议端点上的校验文案不同，因此只锁定状态码与错误码，不比对 message。
func assertFilestoreHandlerReachedAfterAuth(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode filestore error: %v: %s", err, response.Body.String())
	}
	if payload["code"] != "invalid_argument" {
		t.Fatalf("filestore error = %#v, want invalid_argument after successful auth", payload)
	}
	if _, exists := payload["type"]; exists {
		t.Fatalf("filestore error unexpectedly contains type envelope: %#v", payload)
	}
}

func assertFilestoreTokenPrincipal(
	t *testing.T,
	principal filestore.Principal,
	identity filestore.TokenIdentity,
	filesystemUUID string,
	filesystemExternalID string,
) {
	t.Helper()

	if principal.Subject != identity.Subject ||
		principal.AccountUUID != identity.AccountUUID ||
		principal.OrganizationUUID != identity.OrgUUID ||
		principal.WorkspaceUUID != identity.WorkspaceUUID ||
		principal.WorkspaceExternalID != identity.ResolvedWorkspaceTaggedID ||
		principal.FilesystemUUID != filesystemUUID ||
		principal.FilesystemExternalID != filesystemExternalID ||
		principal.FilesystemInternalID <= 0 || principal.Readonly ||
		!filestore.OrgTaintsEqual(principal.OrganizationTaints, identity.OrgTaints) ||
		principal.WorkspaceCMEKEnabled != identity.WorkspaceCMEKEnabled ||
		principal.OrganizationID <= 0 || principal.WorkspaceID <= 0 || principal.AccountID <= 0 {
		t.Fatalf("unexpected filestore principal: %#v", principal)
	}
}

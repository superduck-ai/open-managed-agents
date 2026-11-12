package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

const ownedRepoCaptureEnv = "OMA_OWNED_REPO_CAPTURE_SCOPE"
const ownedRepoMaxBytes = 1 << 20
const ownedRepoMaxRows = 64

type ownedRepoCaptureScope struct {
	RunNonce            string                        `json:"run_nonce"`
	RepositoryURLSHA256 string                        `json:"repository_url_sha256"`
	ExpiresAt           time.Time                     `json:"expires_at"`
	Admissions          map[string]ownedRepoAdmission `json:"admissions"`
	admittedNonce       string
	admitted            ownedRepoAdmission
}
type ownedRepoAdmission struct {
	AgentID      string `json:"agent_id"`
	AgentVersion int    `json:"agent_version"`
	AgentRunID   string `json:"agent_run_id"`
	MountSHA256  string `json:"mount_path_sha256"`
	CheckoutType string `json:"checkout_type"`
	CheckoutSHA  string `json:"checkout_sha"`
}
type ownedRepoRegistration struct {
	Epoch    int64
	RunNonce string
}

var ownedRepoCapture = struct {
	sync.Mutex
	registered map[string]ownedRepoRegistration
}{registered: make(map[string]ownedRepoRegistration)}
var ownedRepoSafeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type ownedRepoNativeRequestKey struct{}

var ownedRepoNativeRequestSequence atomic.Uint64

func ownedRepoNativeRequestContext(ctx context.Context) context.Context {
	if os.Getenv(ownedRepoCaptureEnv) == "" {
		return ctx
	}
	id := "owned_git_" + strconv.FormatInt(time.Now().UnixNano(), 16) + "_" + strconv.FormatUint(ownedRepoNativeRequestSequence.Add(1), 16)
	return context.WithValue(ctx, ownedRepoNativeRequestKey{}, id)
}

var ownedRepoDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ownedRepoHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func ownedRepoLoadScope() (ownedRepoCaptureScope, string, bool) {
	var scope ownedRepoCaptureScope
	file := os.Getenv(ownedRepoCaptureEnv)
	if file == "" || !filepath.IsAbs(file) || filepath.Clean(file) != file {
		return scope, "", false
	}
	dir, err := os.Lstat(filepath.Dir(file))
	if err != nil || !dir.IsDir() || !ownedRepoPrivateOwner(dir) {
		return scope, "", false
	}
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return scope, "", false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 8192 || !ownedRepoPrivateOwner(st) {
		return scope, "", false
	}
	raw, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(raw) > 8192 || json.Unmarshal(raw, &scope) != nil {
		return scope, "", false
	}
	now := time.Now().UTC()
	if !ownedRepoSafeID.MatchString(scope.RunNonce) || len(scope.RunNonce) < 16 || !ownedRepoDigest.MatchString(scope.RepositoryURLSHA256) || !scope.ExpiresAt.After(now) || scope.ExpiresAt.Sub(now) > 15*time.Minute || len(scope.Admissions) != 1 {
		return scope, "", false
	}
	for nonce, a := range scope.Admissions {
		if !a.valid(nonce, scope.RunNonce) {
			return scope, "", false
		}
	}

	return scope, filepath.Join(filepath.Dir(file), "native.jsonl"), true
}

func (h *Handler) ownedRepoCaptureIdentity(ctx context.Context, codeID, org, workspace string) (ownedRepoCaptureScope, string, db.CodeSession, *url.URL, string, bool) {
	return ownedRepoCaptureIdentityDB(ctx, h.db, codeID, org, workspace)
}
func ownedRepoCaptureIdentityDB(ctx context.Context, database *db.DB, codeID, org, workspace string) (ownedRepoCaptureScope, string, db.CodeSession, *url.URL, string, bool) {
	scope, out, ok := ownedRepoLoadScope()
	if !ok {
		return scope, "", db.CodeSession{}, nil, "", false
	}
	code, found, err := database.GetCodeSession(ctx, codeID)
	if err != nil || !found || code.OrganizationUUID != org || code.WorkspaceUUID != workspace {
		return scope, "", code, nil, "", false
	}
	session, found, err := database.GetSession(ctx, workspace, code.SessionExternalID)
	if err != nil || !found || session.UUID != code.SessionUUID || session.OrganizationUUID != org {
		return scope, "", code, nil, "", false
	}
	var snapshot struct {
		Metadata struct {
			RunID string `json:"run_id"`
		} `json:"metadata"`
	}
	var metadata struct {
		Nonce string `json:"owned_repo_capture_nonce"`
	}
	if json.Unmarshal(session.Metadata, &metadata) != nil {
		return scope, "", code, nil, "", false
	}
	admission, allowed := scope.Admissions[metadata.Nonce]
	if !allowed || json.Unmarshal(session.AgentSnapshot, &snapshot) != nil || snapshot.Metadata.RunID != admission.AgentRunID || session.AgentExternalID != admission.AgentID || session.AgentVersion != admission.AgentVersion {
		return scope, "", code, nil, "", false
	}
	scope.admittedNonce, scope.admitted = metadata.Nonce, admission
	resources, err := database.ListSessionResources(ctx, workspace, code.SessionExternalID)
	if err != nil || len(resources) != 1 || resources[0].GitHubRepository == nil {
		return scope, "", code, nil, "", false
	}
	resource := resources[0]
	rawURL := resource.GitHubRepository.URL
	repo := resource.GitHubRepository
	if ownedRepoHash(repo.MountPath) != admission.MountSHA256 || repo.Checkout == nil || repo.Checkout.Type != admission.CheckoutType || repo.Checkout.SHA != admission.CheckoutSHA {
		return scope, "", code, nil, "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || ownedRepoHash(rawURL) != scope.RepositoryURLSHA256 {
		return scope, "", code, nil, "", false
	}
	return scope, out, code, u, resource.ExternalID, true
}

func ownedRepoCatalogue(raw json.RawMessage) (map[string]any, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false
	}
	skillsRaw, skillsPresent := fields["runtime_skills"]
	toolsRaw, toolsPresent := fields["runtime_tools"]
	if !skillsPresent || !toolsPresent || string(skillsRaw) == "null" || string(toolsRaw) == "null" {
		return nil, false
	}
	var entries []json.RawMessage
	var tools []string
	if json.Unmarshal(skillsRaw, &entries) != nil || json.Unmarshal(toolsRaw, &tools) != nil || len(entries) > 500 || len(tools) > 32 {
		return nil, false
	}
	allowedTools := map[string]bool{"Bash": true, "Read": true, "Write": true, "Edit": true, "Grep": true, "Glob": true, "web_search": true, "web_fetch": true, "ask_user_question": true, "subagent": true, "send_message": true, "list_agents": true}
	for _, tool := range tools {
		if !allowedTools[tool] {
			return nil, false
		}
	}
	hashed := make([]map[string]string, 0, len(entries))
	for _, entryRaw := range entries {
		var typed skillsapi.RuntimeCatalogEntry
		var entry map[string]json.RawMessage
		if json.Unmarshal(entryRaw, &typed) != nil || json.Unmarshal(entryRaw, &entry) != nil || typed.Name == "" || typed.Path == "" {
			return nil, false
		}
		row := map[string]string{}
		for key, value := range entry {
			digest, ok := ownedRepoFieldDigest(key, value)
			if !ok {
				return nil, false
			}
			row[key+"_sha256"] = digest
		}

		hashed = append(hashed, row)
	}
	canonical, err := jsoncanonical.Canonicalize(skillsRaw)
	if err != nil {
		return nil, false
	}
	return map[string]any{"runtime_skills": hashed, "runtime_skills_count": len(entries), "runtime_skills_sha256": ownedRepoHash(string(canonical)), "runtime_tools": tools, "metadata_complete": true, "metadata_encoding": "ordered_entries_field_canonical_json_sha256_v2"}, true
}

func ownedRepoAppend(out string, row map[string]any) bool {
	row["observed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(row)
	if err != nil || len(raw) > 96<<10 {
		return false
	}
	f, err := os.OpenFile(out, os.O_RDWR|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !ownedRepoPrivateOwner(st) || st.Size() > ownedRepoMaxBytes || st.Size()+int64(len(raw)+1) > ownedRepoMaxBytes {
		return false
	}
	prior, err := io.ReadAll(io.LimitReader(f, ownedRepoMaxBytes+1))
	if err != nil {
		return false
	}
	lines := 0
	for _, b := range prior {
		if b == '\n' {
			lines++
		}
	}
	if len(prior) > 0 && prior[len(prior)-1] != '\n' {
		return false
	}
	if lines >= ownedRepoMaxRows {
		return false
	}
	n, err := f.Write(append(raw, '\n'))
	return err == nil && n == len(raw)+1
}

func (l dshLifecycle) ObserveOwnedRepositoryRegistration(ctx context.Context, scope db.DSHScope, raw json.RawMessage, issue dsh.RegistrationIssue) {
	if os.Getenv(ownedRepoCaptureEnv) == "" || issue.Status != 200 || !issue.Complete || issue.ResponseEpoch != scope.Epoch || issue.StartedAt.IsZero() || issue.FinishedAt.IsZero() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	cfg, out, code, _, resourceID, ok := l.handler.ownedRepoCaptureIdentity(ctx, scope.CodeSessionID, scope.OrganizationUUID, scope.WorkspaceUUID)
	if !ok || code.SessionExternalID != scope.SessionID || code.CurrentWorkerEpoch != scope.Epoch || scope.Epoch < 1 {
		return
	}
	metadata, ok := ownedRepoCatalogue(raw)
	if !ok {
		return
	}
	row := map[string]any{"event": "owned_dsr_registration_metadata_v2", "run_nonce": cfg.RunNonce, "capture_nonce": cfg.admittedNonce, "agent_id": cfg.admitted.AgentID, "agent_version": cfg.admitted.AgentVersion, "mount_path_sha256": cfg.admitted.MountSHA256, "checkout_sha": cfg.admitted.CheckoutSHA, "session_id": scope.SessionID, "code_session_id": scope.CodeSessionID, "worker_epoch": scope.Epoch, "owner_epoch": code.CurrentWorkerEpoch, "response_epoch": issue.ResponseEpoch, "response_status": issue.Status, "response_write_complete": issue.Complete, "response_issue_started_at": issue.StartedAt, "response_issue_finished_at": issue.FinishedAt, "proof_kind": "server_issued_dsr_snapshot_not_client_wire", "repository_url_sha256": cfg.RepositoryURLSHA256, "resource_id": resourceID, "request_id": httpapi.RequestID(ctx), "protocol": "1", "registration_accepted": true}
	for k, v := range metadata {
		row[k] = v
	}
	ownedRepoCapture.Lock()
	defer ownedRepoCapture.Unlock()
	if ownedRepoAppend(out, row) {
		if len(ownedRepoCapture.registered) < 8 {
			ownedRepoCapture.registered[scope.CodeSessionID] = ownedRepoRegistration{scope.Epoch, cfg.RunNonce}
		} else if _, known := ownedRepoCapture.registered[scope.CodeSessionID]; known {
			ownedRepoCapture.registered[scope.CodeSessionID] = ownedRepoRegistration{scope.Epoch, cfg.RunNonce}
		}
	}
}

func (h *Handler) observeOwnedRepositoryGitResponse(identity upstreamProxyIdentity, response *http.Response) {
	if os.Getenv(ownedRepoCaptureEnv) == "" || response == nil || response.Request == nil || identity.sessionThreadID != "" {
		return
	}
	req := response.Request
	nativeRequestID, _ := req.Context().Value(ownedRepoNativeRequestKey{}).(string)
	if nativeRequestID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 250*time.Millisecond)
	defer cancel()
	cfg, out, code, repo, resourceID, ok := h.ownedRepoCaptureIdentity(ctx, identity.codeSessionExternalID, identity.organizationUUID, identity.workspaceUUID)
	if !ok || req.URL == nil || !strings.EqualFold(req.URL.Hostname(), "github.com") || req.Method != "GET" || response.StatusCode < 100 || response.StatusCode > 599 {
		return
	}
	repositoryPath := strings.TrimSuffix(strings.TrimSuffix(repo.Path, "/"), ".git")
	if req.URL.Path != repositoryPath+"/info/refs" && req.URL.Path != repositoryPath+".git/info/refs" {
		return
	}
	ownedRepoCapture.Lock()
	defer ownedRepoCapture.Unlock()
	registered, accepted := ownedRepoCapture.registered[code.ExternalID]
	phase := "authenticated_proxy_code_epoch_observed_at_response"
	matchedRegistration := accepted && registered.RunNonce == cfg.RunNonce && registered.Epoch == code.CurrentWorkerEpoch
	if code.CurrentWorkerEpoch < 1 {
		return
	}
	row := map[string]any{"event": "owned_git_response_v2", "run_nonce": cfg.RunNonce, "capture_nonce": cfg.admittedNonce, "agent_id": cfg.admitted.AgentID, "agent_version": cfg.admitted.AgentVersion, "mount_path_sha256": cfg.admitted.MountSHA256, "checkout_sha": cfg.admitted.CheckoutSHA, "session_id": code.SessionExternalID, "code_session_id": code.ExternalID, "worker_epoch": code.CurrentWorkerEpoch, "owner_epoch": code.CurrentWorkerEpoch, "epoch_source": phase, "registration_epoch_matched": matchedRegistration, "repository_url_sha256": cfg.RepositoryURLSHA256, "resource_id": resourceID, "request_id": httpapi.RequestID(ctx), "native_request_id": nativeRequestID, "native_request_id_source": "local_MITM_request_context_not_upstream_header", "http_method": req.Method, "host": "github.com", "path_sha256": ownedRepoHash(req.URL.Path), "git_endpoint": "info/refs", "http_status": response.StatusCode}
	ownedRepoAppend(out, row)
}

func (s *Service) observeOwnedRepositoryFreeze(ctx context.Context, scope db.DSHScope, candidate []byte, receipt db.DSHContextFreezeReceipt, roots []string, skillCount int) {
	if os.Getenv(ownedRepoCaptureEnv) == "" || !receipt.Written || receipt.StatementAt.IsZero() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	cfg, out, code, _, resourceID, ok := ownedRepoCaptureIdentityDB(ctx, s.db, scope.CodeSessionID, scope.OrganizationUUID, scope.WorkspaceUUID)
	if !ok || scope.Epoch < 1 || code.SessionExternalID != scope.SessionID || code.CurrentWorkerEpoch < scope.Epoch {
		return
	}
	var snapshot struct {
		Config json.RawMessage `json:"config"`
	}
	equal, err := jsoncanonical.Equal(candidate, receipt.Snapshot)
	if err != nil || !equal || json.Unmarshal(receipt.Snapshot, &snapshot) != nil {
		return
	}
	metadata, ok := ownedRepoCatalogue(snapshot.Config)
	if !ok {
		return
	}
	rootJSON, err := json.Marshal(roots)
	if err != nil {
		return
	}
	row := map[string]any{"event": "owned_dsr_repository_freeze_v2", "run_nonce": cfg.RunNonce, "capture_nonce": cfg.admittedNonce, "agent_id": cfg.admitted.AgentID, "agent_version": cfg.admitted.AgentVersion, "session_id": scope.SessionID, "code_session_id": scope.CodeSessionID, "worker_epoch": scope.Epoch, "owner_epoch": scope.Epoch, "epoch_source": "server_repository_scan_before_dispatch", "owner_epoch_source": "successful_fenced_freeze_transaction", "observed_owner_epoch": code.CurrentWorkerEpoch, "repository_url_sha256": cfg.RepositoryURLSHA256, "mount_path_sha256": cfg.admitted.MountSHA256, "checkout_sha": cfg.admitted.CheckoutSHA, "resource_id": resourceID, "freeze_written": true, "freeze_statement_at": receipt.StatementAt, "scan_complete": true, "freeze_complete": true, "discovered_skill_count": skillCount, "roots_count": len(roots), "roots_sha256": ownedRepoHash(string(rootJSON))}
	for k, v := range metadata {
		row[k] = v
	}
	ownedRepoCapture.Lock()
	defer ownedRepoCapture.Unlock()
	ownedRepoAppend(out, row)
}

func ownedRepoPrivateOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Uid) == uint64(os.Geteuid()) && info.Mode().Perm()&0077 == 0
}

func (a ownedRepoAdmission) valid(nonce, run string) bool {
	return len(nonce) == 32 && ownedRepoSafeID.MatchString(nonce) && strings.HasPrefix(a.AgentID, "agent_") && ownedRepoSafeID.MatchString(a.AgentID) && a.AgentVersion > 0 && a.AgentRunID == run && ownedRepoDigest.MatchString(a.MountSHA256) && a.CheckoutType == "commit" && regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(a.CheckoutSHA)
}

func ownedRepoFieldDigest(key string, value json.RawMessage) (string, bool) {
	if key == "requested_versions" {
		var versions []string
		if json.Unmarshal(value, &versions) != nil || versions == nil || len(versions) > 64 {
			return "", false
		}
		for _, version := range versions {
			if len(version) > 1024 {
				return "", false
			}
		}
	} else {
		switch key {
		case "name", "description", "path", "version", "source", "skill_id", "version_uuid", "sha256":
		default:
			return "", false
		}
		var text *string
		if json.Unmarshal(value, &text) != nil || text == nil || len(*text) > 8192 {
			return "", false
		}
	}
	canonical, err := jsoncanonical.Canonicalize(value)
	if err != nil {
		return "", false
	}
	return ownedRepoHash(string(canonical)), true
}

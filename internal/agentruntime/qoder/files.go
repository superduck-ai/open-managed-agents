package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type qoderFile struct {
	ID           string    `json:"id"`
	Filename     string    `json:"filename"`
	MimeType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	Downloadable bool      `json:"downloadable"`
	ScopeID      string    `json:"scope_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type qoderFilesEnvelope struct {
	Data  []qoderFile `json:"data"`
	Files []qoderFile `json:"files"`
}

type qoderFileContentResponse struct {
	URL         string    `json:"url"`
	DownloadURL string    `json:"download_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Filename    string    `json:"filename"`
	MimeType    string    `json:"mime_type"`
}

type fileBindingSummary struct {
	Name         string `json:"name"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	Downloadable bool   `json:"downloadable"`
	ScopeID      string `json:"scope_id,omitempty"`
}

type fileContentStream struct {
	io.ReadCloser
	contentType   string
	contentLength int64
	filename      string
}

func (s *fileContentStream) ContentType() string  { return s.contentType }
func (s *fileContentStream) ContentLength() int64 { return s.contentLength }
func (s *fileContentStream) Filename() string     { return s.filename }

func (r *Runtime) newDownloadClient(dialer *net.Dialer) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, downloadBlockedError("invalid download address")
			}
			safeIP, err := r.resolveSafeDownloadIP(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(safeIP.String(), port))
		},
		TLSHandshakeTimeout:   defaultDialTimeout,
		ResponseHeaderTimeout: defaultHeaderTimeout,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) == 0 {
				return nil
			}
			if len(via) >= 3 {
				return downloadBlockedError("too many redirects")
			}
			orig := via[0].URL
			if !strings.EqualFold(req.URL.Scheme, "https") {
				return downloadBlockedError("redirect to non-https scheme")
			}
			if !sameOriginHostPort(orig, req.URL) {
				return downloadBlockedError("cross-origin redirect blocked")
			}
			if !isAllowedDownloadHost(req.URL.Hostname(), r.allowedDownloadHosts) {
				return downloadBlockedError("redirect host not allowed")
			}
			return nil
		},
	}
}

func sameOriginHostPort(a, b *url.URL) bool {
	if !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	portA := a.Port()
	if portA == "" {
		portA = "443"
	}
	portB := b.Port()
	if portB == "" {
		portB = "443"
	}
	return portA == portB
}

func (r *Runtime) UploadFile(ctx context.Context, scope agentruntime.Scope, input agentruntime.UploadFileInput) (agentruntime.File, error) {
	if err := r.validateUploadFile(input); err != nil {
		return agentruntime.File{}, err
	}
	remoteScopeID := ""
	if input.ScopeID != "" {
		sessBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, input.ScopeID)
		if err != nil {
			return agentruntime.File{}, err
		}
		remoteScopeID = sessBinding.RemoteID
	}
	remote, err := r.uploadFileMultipart(ctx, input, remoteScopeID)
	if err != nil {
		return agentruntime.File{}, err
	}
	omaID, err := ids.New("file_")
	if err != nil {
		return agentruntime.File{}, agentruntime.UnavailableError("failed to generate file identifier", err)
	}
	summary, _ := json.Marshal(fileBindingSummary{
		Name:         remote.Filename,
		Filename:     remote.Filename,
		MimeType:     remote.MimeType,
		SizeBytes:    remote.SizeBytes,
		Downloadable: remote.Downloadable,
		ScopeID:      input.ScopeID,
	})
	if _, err := r.bindings.Bind(ctx, scope, agentruntime.ResourceRef{
		Type:                agentruntime.ResourceTypeFile,
		OMAExternalID:       omaID,
		RemoteID:            remote.ID,
		ParentOMAExternalID: input.ScopeID,
		Role:                "upload",
	}, summary); err != nil {
		return agentruntime.File{}, err
	}
	return mapQoderFile(omaID, input.ScopeID, "upload", remote), nil
}

func (r *Runtime) validateUploadFile(input agentruntime.UploadFileInput) error {
	constraints := r.caps.FileConstraints
	if constraints.MaxSizeBytes > 0 && int64(len(input.Content)) > constraints.MaxSizeBytes {
		return fileTooLargeError(constraints.MaxSizeBytes)
	}
	if constraints.AllowBinary || len(constraints.AllowedMIMEPrefixes) == 0 {
		return nil
	}
	effectiveMIME := detectEffectiveMIME(input.MimeType, input.Content)
	for _, prefix := range constraints.AllowedMIMEPrefixes {
		normalizedPrefix := strings.ToLower(strings.TrimSpace(prefix))
		if normalizedPrefix == "" {
			continue
		}
		if strings.HasSuffix(normalizedPrefix, "/") {
			if strings.HasPrefix(effectiveMIME, normalizedPrefix) {
				return nil
			}
		} else if effectiveMIME == normalizedPrefix {
			return nil
		}
	}
	return unsupportedFileTypeError(effectiveMIME)
}

func detectEffectiveMIME(declared string, content []byte) string {
	trimmed := strings.TrimSpace(declared)
	if trimmed != "" {
		if mediaType, _, err := mime.ParseMediaType(trimmed); err == nil && mediaType != "" {
			return strings.ToLower(mediaType)
		}
		return strings.ToLower(trimmed)
	}
	detected := http.DetectContentType(content)
	if mediaType, _, err := mime.ParseMediaType(detected); err == nil && mediaType != "" {
		return strings.ToLower(mediaType)
	}
	return strings.ToLower(detected)
}

func (r *Runtime) uploadFileMultipart(ctx context.Context, input agentruntime.UploadFileInput, remoteScopeID string) (qoderFile, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filename := strings.TrimSpace(input.Filename)
	if filename == "" {
		filename = "upload"
	}
	contentType := detectEffectiveMIME(input.MimeType, input.Content)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return qoderFile{}, agentruntime.UnavailableError("failed to construct multipart upload", err)
	}
	if _, err := part.Write(input.Content); err != nil {
		return qoderFile{}, agentruntime.UnavailableError("failed to write upload content", err)
	}
	if remoteScopeID != "" {
		if err := writer.WriteField("scope_id", remoteScopeID); err != nil {
			return qoderFile{}, agentruntime.UnavailableError("failed to write scope_id field", err)
		}
	}
	if err := writer.Close(); err != nil {
		return qoderFile{}, agentruntime.UnavailableError("failed to finalize multipart upload", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.buildURL("/files", nil), &body)
	if err != nil {
		return qoderFile{}, upstreamTransportError(err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := r.restClient.Do(req)
	if err != nil {
		return qoderFile{}, upstreamTransportError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return qoderFile{}, readAndMapUpstreamError(resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes))
	if err != nil {
		return qoderFile{}, upstreamTransportError(err)
	}
	var remote qoderFile
	if err := json.Unmarshal(raw, &remote); err != nil {
		return qoderFile{}, upstreamTransportError(err)
	}
	return remote, nil
}

func (r *Runtime) GetFile(ctx context.Context, scope agentruntime.Scope, fileID string) (agentruntime.File, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeFile, fileID)
	if err != nil {
		return agentruntime.File{}, err
	}
	var remote qoderFile
	if err := r.doJSON(ctx, http.MethodGet, "/files/"+url.PathEscape(binding.RemoteID), nil, nil, nil, &remote); err != nil {
		return agentruntime.File{}, err
	}
	return mapQoderFile(binding.OMAExternalID, binding.ParentOMAExternalID, binding.ResourceRole, remote), nil
}

func (r *Runtime) ListFiles(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListFilesQuery) (agentruntime.Page[agentruntime.File], error) {
	if query.ScopeID != "" {
		return r.listScopedFiles(ctx, scope, query)
	}
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	rows, err := r.bindings.ListOwned(ctx, scope, agentruntime.ResourceTypeFile, "", "", false, limit+1, offset)
	if err != nil {
		return agentruntime.Page[agentruntime.File]{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]agentruntime.File, 0, len(rows))
	for _, row := range rows {
		var remote qoderFile
		if err := r.doJSON(ctx, http.MethodGet, "/files/"+url.PathEscape(row.RemoteID), nil, nil, nil, &remote); err != nil {
			r.logger.WarnContext(ctx, "qoder file hydrate failed, falling back to binding summary", "oma_id", row.OMAExternalID, "error", err)
			items = append(items, fileFromBindingSummary(row))
			continue
		}
		items = append(items, mapQoderFile(row.OMAExternalID, row.ParentOMAExternalID, row.ResourceRole, remote))
	}
	page := agentruntime.Page[agentruntime.File]{
		Items:   items,
		HasMore: hasMore,
	}
	if len(items) > 0 {
		page.FirstID = items[0].ID
		page.LastID = items[len(items)-1].ID
	}
	return page, nil
}

func (r *Runtime) listScopedFiles(ctx context.Context, scope agentruntime.Scope, query agentruntime.ListFilesQuery) (agentruntime.Page[agentruntime.File], error) {
	sessBinding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, query.ScopeID)
	if err != nil {
		return agentruntime.Page[agentruntime.File]{}, err
	}
	params := url.Values{}
	params.Set("scope_id", sessBinding.RemoteID)
	var raw json.RawMessage
	if err := r.doJSON(ctx, http.MethodGet, "/files", params, nil, nil, &raw); err != nil {
		return agentruntime.Page[agentruntime.File]{}, err
	}
	remoteFiles, err := decodeFilesPayload(raw)
	if err != nil {
		return agentruntime.Page[agentruntime.File]{}, upstreamTransportError(err)
	}
	claimed := make([]agentruntime.File, 0, len(remoteFiles))
	for _, rf := range remoteFiles {
		binding, err := r.ensureFileBinding(ctx, scope, sessBinding.OMAExternalID, rf)
		if err != nil {
			return agentruntime.Page[agentruntime.File]{}, err
		}
		claimed = append(claimed, mapQoderFile(binding.OMAExternalID, binding.ParentOMAExternalID, binding.ResourceRole, rf))
	}
	limit := normalizeListLimit(query.Limit)
	offset := normalizeListOffset(query.Offset)
	if offset >= len(claimed) {
		return agentruntime.Page[agentruntime.File]{Items: []agentruntime.File{}}, nil
	}
	end := offset + limit
	hasMore := false
	if end < len(claimed) {
		hasMore = true
	} else {
		end = len(claimed)
	}
	slice := claimed[offset:end]
	page := agentruntime.Page[agentruntime.File]{
		Items:   slice,
		HasMore: hasMore,
	}
	if len(slice) > 0 {
		page.FirstID = slice[0].ID
		page.LastID = slice[len(slice)-1].ID
	}
	return page, nil
}

func (r *Runtime) ensureFileBinding(
	ctx context.Context,
	scope agentruntime.Scope,
	parentSessionOMAID string,
	rf qoderFile,
) (db.RuntimeResourceBinding, error) {
	existing, err := r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeFile, rf.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, agentruntime.ErrResourceNotFound) {
		return db.RuntimeResourceBinding{}, err
	}
	omaID, err := ids.New("file_")
	if err != nil {
		return db.RuntimeResourceBinding{}, agentruntime.UnavailableError("failed to generate file identifier", err)
	}
	summary, _ := json.Marshal(fileBindingSummary{
		Name:         rf.Filename,
		Filename:     rf.Filename,
		MimeType:     rf.MimeType,
		SizeBytes:    rf.SizeBytes,
		Downloadable: rf.Downloadable,
		ScopeID:      parentSessionOMAID,
	})
	claimed, inserted, err := r.bindings.ClaimOutput(ctx, scope, agentruntime.ResourceRef{
		Type:                agentruntime.ResourceTypeFile,
		OMAExternalID:       omaID,
		RemoteID:            rf.ID,
		ParentOMAExternalID: parentSessionOMAID,
		Role:                "session_output",
	}, summary)
	if err != nil {
		return db.RuntimeResourceBinding{}, err
	}
	if inserted {
		return claimed, nil
	}
	return r.bindings.LookupByRemoteID(ctx, scope, agentruntime.ResourceTypeFile, rf.ID)
}

func decodeFilesPayload(raw json.RawMessage) ([]qoderFile, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []qoderFile
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var env qoderFilesEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, err
	}
	if len(env.Data) > 0 {
		return env.Data, nil
	}
	return env.Files, nil
}

func (r *Runtime) DownloadFile(ctx context.Context, scope agentruntime.Scope, fileID string) (agentruntime.FileContentStream, error) {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeFile, fileID)
	if err != nil {
		return nil, err
	}
	var contentResp qoderFileContentResponse
	if err := r.doJSON(ctx, http.MethodGet, "/files/"+url.PathEscape(binding.RemoteID)+"/content", nil, nil, nil, &contentResp); err != nil {
		return nil, err
	}
	downloadURL := strings.TrimSpace(contentResp.URL)
	if downloadURL == "" {
		downloadURL = strings.TrimSpace(contentResp.DownloadURL)
	}
	parsedURL, err := validateDownloadURL(downloadURL, r.allowedDownloadHosts)
	if err != nil {
		r.logger.WarnContext(ctx, "qoder file download blocked", "reason", err.Error())
		return nil, err
	}
	if _, err := r.resolveSafeDownloadIP(ctx, parsedURL.Hostname()); err != nil {
		r.logger.WarnContext(ctx, "qoder file download ip blocked", "host", parsedURL.Hostname(), "reason", err.Error())
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, upstreamTransportError(err)
	}
	resp, err := r.downloadClient.Do(req)
	if err != nil {
		unwrapped := unwrapURLError(err)
		if errors.Is(err, ErrDownloadBlocked) {
			return nil, unwrapped
		}
		r.logger.WarnContext(ctx, "qoder file download failed", "host", parsedURL.Hostname(), "error", unwrapped)
		return nil, upstreamTransportError(unwrapped)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		r.logger.WarnContext(ctx, "qoder file download returned non-2xx", "host", parsedURL.Hostname(), "status", resp.StatusCode)
		return nil, readAndMapUpstreamError(resp)
	}

	var summary fileBindingSummary
	_ = json.Unmarshal(binding.Summary, &summary)
	filename := contentResp.Filename
	if filename == "" {
		filename = summary.Filename
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = contentResp.MimeType
	}
	if contentType == "" {
		contentType = summary.MimeType
	}
	contentLength := resp.ContentLength
	if contentLength < 0 {
		if headerLen := strings.TrimSpace(resp.Header.Get("Content-Length")); headerLen != "" {
			if parsedLen, parseErr := strconv.ParseInt(headerLen, 10, 64); parseErr == nil {
				contentLength = parsedLen
			}
		}
	}
	return &fileContentStream{
		ReadCloser:    resp.Body,
		contentType:   contentType,
		contentLength: contentLength,
		filename:      filename,
	}, nil
}

func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}

func validateDownloadURL(rawURL string, allowedHosts []string) (*url.URL, error) {
	if rawURL == "" {
		return nil, downloadBlockedError("empty download url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, downloadBlockedError("malformed download url")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return nil, downloadBlockedError("download url must use https")
	}
	if parsed.User != nil {
		return nil, downloadBlockedError("userinfo not allowed in download url")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return nil, downloadBlockedError("empty download host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isSafePublicAddr(ip) {
			return nil, downloadBlockedError("private or non-public ip literal blocked")
		}
	}
	if !isAllowedDownloadHost(host, allowedHosts) {
		return nil, downloadBlockedError("download host not in allowlist")
	}
	return parsed, nil
}

func isAllowedDownloadHost(host string, patterns []string) bool {
	normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if normalized == "" {
		return false
	}
	for _, pattern := range patterns {
		if matchHostPattern(normalized, strings.ToLower(strings.TrimSuffix(strings.TrimSpace(pattern), "."))) {
			return true
		}
	}
	return false
}

func matchHostPattern(host, pattern string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return host == pattern
	}
	hostLabels := strings.Split(host, ".")
	patLabels := strings.Split(pattern, ".")
	if len(hostLabels) != len(patLabels) {
		return false
	}
	for i := range patLabels {
		if !matchLabelWildcard(hostLabels[i], patLabels[i]) {
			return false
		}
	}
	return true
}

func matchLabelWildcard(label, pattern string) bool {
	if pattern == "*" {
		return label != ""
	}
	if !strings.Contains(pattern, "*") {
		return label == pattern
	}
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return false
	}
	prefix, suffix := parts[0], parts[1]
	if len(label) < len(prefix)+len(suffix) {
		return false
	}
	return strings.HasPrefix(label, prefix) && strings.HasSuffix(label, suffix)
}

func (r *Runtime) resolveSafeDownloadIP(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isSafePublicAddr(ip) {
			return netip.Addr{}, downloadBlockedError("non-public ip address blocked")
		}
		return ip.Unmap(), nil
	}
	ips, err := r.lookupIP(ctx, host)
	if err != nil || len(ips) == 0 {
		return netip.Addr{}, downloadBlockedError("failed to resolve download host")
	}
	var selected netip.Addr
	for _, rawIP := range ips {
		addr, ok := netip.AddrFromSlice(rawIP)
		if !ok || !r.isAllowedResolvedDownloadAddr(addr) {
			return netip.Addr{}, downloadBlockedError("resolved non-public ip address blocked")
		}
		if !selected.IsValid() {
			selected = addr.Unmap()
		}
	}
	if !selected.IsValid() {
		return netip.Addr{}, downloadBlockedError("no valid public ip address resolved")
	}
	return selected, nil
}

func (r *Runtime) isAllowedResolvedDownloadAddr(addr netip.Addr) bool {
	if isSafePublicAddr(addr) {
		return true
	}
	if r.allowDevFakeIP && addr.IsValid() {
		unmapped := addr.Unmap()
		if unmapped.Is4() && ipv4Benchmark.Contains(unmapped) {
			return true
		}
	}
	return false
}

var (
	cgnatPrefix        = netip.MustParsePrefix("100.64.0.0/10")
	ipv4LoopbackPrefix = netip.MustParsePrefix("127.0.0.0/8")
	ipv4LinkLocal      = netip.MustParsePrefix("169.254.0.0/16")
	ipv4Benchmark      = netip.MustParsePrefix("198.18.0.0/15")
	ipv4ThisNetwork    = netip.MustParsePrefix("0.0.0.0/8")
)

func isSafePublicAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	unmapped := addr.Unmap()
	if !unmapped.IsGlobalUnicast() || unmapped.IsPrivate() || unmapped.IsLoopback() || unmapped.IsLinkLocalUnicast() || unmapped.IsLinkLocalMulticast() || unmapped.IsMulticast() || unmapped.IsUnspecified() {
		return false
	}
	if unmapped.Is4() {
		if cgnatPrefix.Contains(unmapped) || ipv4LoopbackPrefix.Contains(unmapped) || ipv4LinkLocal.Contains(unmapped) || ipv4Benchmark.Contains(unmapped) || ipv4ThisNetwork.Contains(unmapped) {
			return false
		}
	}
	return true
}

func (r *Runtime) DeleteFile(ctx context.Context, scope agentruntime.Scope, fileID string) error {
	binding, err := r.bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeFile, fileID)
	if err != nil {
		return err
	}
	deleteErr := r.doJSON(ctx, http.MethodDelete, "/files/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil)
	if deleteErr != nil && isUpstreamConflict(deleteErr) {
		if detachErr := r.detachFileFromSessions(ctx, scope, binding, deleteErr); detachErr != nil {
			return detachErr
		}
		deleteErr = r.doJSON(ctx, http.MethodDelete, "/files/"+url.PathEscape(binding.RemoteID), nil, nil, nil, nil)
	}
	if deleteErr != nil && !isUpstreamNotFound(deleteErr) {
		return deleteErr
	}
	return r.bindings.SoftDelete(ctx, scope, agentruntime.ResourceTypeFile, binding.OMAExternalID)
}

func (r *Runtime) detachFileFromSessions(ctx context.Context, scope agentruntime.Scope, fileBinding db.RuntimeResourceBinding, conflictErr error) error {
	remoteSessionIDs := collectReferencedRemoteSessions(ctx, r.bindings, scope, fileBinding, conflictErr)
	if len(remoteSessionIDs) == 0 {
		return conflictErr
	}
	for _, remoteSessionID := range remoteSessionIDs {
		resources, err := r.fetchRemoteSessionResources(ctx, remoteSessionID)
		if err != nil {
			if isUpstreamNotFound(err) {
				continue
			}
			return err
		}
		for _, res := range resources {
			if res.FileID != fileBinding.RemoteID {
				continue
			}
			path := "/sessions/" + url.PathEscape(remoteSessionID) + "/resources/" + url.PathEscape(res.ID)
			if err := r.doJSON(ctx, http.MethodDelete, path, nil, nil, nil, nil); err != nil && !isUpstreamNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func collectReferencedRemoteSessions(
	ctx context.Context,
	bindings *agentruntime.BindingService,
	scope agentruntime.Scope,
	fileBinding db.RuntimeResourceBinding,
	conflictErr error,
) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 2)
	add := func(id string) {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	var apiErr *upstreamAPIError
	if errors.As(conflictErr, &apiErr) {
		for _, sid := range apiErr.ReferencedSessionIDs {
			add(sid)
		}
	}
	if len(out) == 0 && fileBinding.ParentOMAExternalID != "" {
		if sessBinding, err := bindings.ResolveOwned(ctx, scope, agentruntime.ResourceTypeSession, fileBinding.ParentOMAExternalID); err == nil {
			add(sessBinding.RemoteID)
		}
	}
	return out
}

func isUpstreamConflict(err error) bool {
	return errors.Is(err, ErrUpstreamConflict)
}

func isUpstreamNotFound(err error) bool {
	return errors.Is(err, ErrUpstreamNotFound)
}

func mapQoderFile(omaID, scopeID, role string, remote qoderFile) agentruntime.File {
	return agentruntime.File{
		ID:           omaID,
		Filename:     remote.Filename,
		MimeType:     remote.MimeType,
		SizeBytes:    remote.SizeBytes,
		Downloadable: remote.Downloadable,
		ScopeID:      scopeID,
		Role:         role,
		CreatedAt:    remote.CreatedAt,
	}
}

func fileFromBindingSummary(row db.RuntimeResourceBinding) agentruntime.File {
	var summary fileBindingSummary
	_ = json.Unmarshal(row.Summary, &summary)
	return agentruntime.File{
		ID:           row.OMAExternalID,
		Filename:     summary.Filename,
		MimeType:     summary.MimeType,
		SizeBytes:    summary.SizeBytes,
		Downloadable: summary.Downloadable,
		ScopeID:      row.ParentOMAExternalID,
		Role:         row.ResourceRole,
		CreatedAt:    row.CreatedAt,
	}
}

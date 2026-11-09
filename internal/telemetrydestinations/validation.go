package telemetrydestinations

import (
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaxDestinations        = 10
	maxWorkspaces          = 1000
	maxHeaders             = 20
	maxHeaderNameBytes     = 128
	maxHeaderValueBytes    = 4096
	maxHeaderTotalBytes    = 16384
	maxURLLength           = 2048
	maxDisplayNameLength   = 255
	DefaultMaxContentBytes = 786432
	contentModeMetadata    = "metadata_only"
	contentModeContent     = "content"
)

var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

var reservedHeaders = []string{"connection", "content-encoding", "content-length", "content-type", "host", "keep-alive", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade"}

func validateEndpointURL(field, raw string, allowInsecure bool) (*url.URL, error) {
	if raw == "" || len(raw) > maxURLLength {
		return nil, invalidRequestf("%s must be a URL of at most %d characters", field, maxURLLength)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return nil, invalidRequestf("%s must be an absolute URL", field)
	}
	if parsed.Scheme != "https" && (!allowInsecure || parsed.Scheme != "http") {
		return nil, invalidRequestf("%s must use https", field)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, invalidRequestf("%s must not include credentials, a query or a fragment", field)
	}
	if port := parsed.Port(); port != "" && port != "443" && !allowInsecure {
		return nil, invalidRequestf("%s must use port 443", field)
	}
	host := parsed.Hostname()
	if _, err := netip.ParseAddr(host); (err == nil || strings.EqualFold(host, "localhost")) && !allowInsecure {
		return nil, invalidRequestf("%s must use a public hostname", field)
	}
	return parsed, nil
}

func defaultTracesURL(base string) string {
	return strings.TrimRight(base, "/") + "/v1/traces"
}

func validateTracesURL(raw string, base *url.URL, allowInsecure bool) error {
	parsed, err := validateEndpointURL("traces.endpoint_url", raw, allowInsecure)
	if err != nil {
		return err
	}
	if !strings.EqualFold(parsed.Hostname(), base.Hostname()) || effectivePort(parsed) != effectivePort(base) || parsed.Scheme != base.Scheme {
		return invalidRequest("traces.endpoint_url must use the same scheme and host as endpoint_url")
	}
	return nil
}

func effectivePort(endpoint *url.URL) string {
	if port := endpoint.Port(); port != "" {
		return port
	}
	if endpoint.Scheme == "http" {
		return "80"
	}
	return "443"
}

func validateHeaders(headers map[string]string) ([]string, error) {
	if len(headers) > maxHeaders {
		return nil, invalidRequestf("headers can contain at most %d entries", maxHeaders)
	}
	names := make([]string, 0, len(headers))
	total := 0
	seen := map[string]bool{}
	for name, value := range headers {
		canonical := http.CanonicalHeaderKey(name)
		if !headerNamePattern.MatchString(name) || len(name) > maxHeaderNameBytes || slices.Contains(reservedHeaders, strings.ToLower(name)) {
			return nil, invalidRequestf("header name %q is not allowed", name)
		}
		if seen[canonical] {
			return nil, invalidRequestf("header %q is duplicated", name)
		}
		seen[canonical] = true
		if len(value) > maxHeaderValueBytes || strings.ContainsAny(value, "\r\n\x00") || !utf8.ValidString(value) {
			return nil, invalidRequestf("header %q has an invalid value", name)
		}
		total += len(name) + len(value)
		names = append(names, name)
	}
	if total > maxHeaderTotalBytes {
		return nil, invalidRequestf("headers can contain at most %d bytes", maxHeaderTotalBytes)
	}
	slices.SortFunc(names, func(left, right string) int { return strings.Compare(strings.ToLower(left), strings.ToLower(right)) })
	return names, nil
}

func validateDisplayName(name *string) error {
	if name != nil && utf8.RuneCountInString(*name) > maxDisplayNameLength {
		return invalidRequestf("display_name can contain at most %d characters", maxDisplayNameLength)
	}
	return nil
}

func validateWorkspaces(request workspacesRequest) error {
	switch request.Type {
	case "all":
		if len(request.WorkspaceIDs) > 0 {
			return invalidRequest("workspaces.workspace_ids must be empty when type is all")
		}
	case "include", "exclude":
		if request.Type == "include" && len(request.WorkspaceIDs) == 0 {
			return invalidRequest("workspaces.workspace_ids must not be empty when type is include")
		}
		if len(request.WorkspaceIDs) > maxWorkspaces {
			return invalidRequestf("workspaces.workspace_ids can contain at most %d workspaces", maxWorkspaces)
		}
	default:
		return invalidRequest("workspaces.type must be all, include or exclude")
	}
	return nil
}

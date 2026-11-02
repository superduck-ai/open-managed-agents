package tracecontext

import (
	"net/http"
	"regexp"
	"strings"
)

const maxTraceStateLength = 512

var traceParentPattern = regexp.MustCompile(`^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})(-.*)?$`)

var traceStatePattern = regexp.MustCompile(`^[\x20-\x7e]*$`)

type Context struct {
	TraceParent string
	TraceState  string
}

func Parse(traceParent, traceState string) (Context, bool) {
	traceParent = strings.TrimSpace(traceParent)
	match := traceParentPattern.FindStringSubmatch(traceParent)
	if match == nil || match[1] == "ff" || (match[1] == "00" && match[5] != "") {
		return Context{}, false
	}
	if strings.Trim(match[2], "0") == "" || strings.Trim(match[3], "0") == "" {
		return Context{}, false
	}
	result := Context{TraceParent: strings.Join(match[1:5], "-")}
	traceState = strings.TrimSpace(traceState)
	if traceState != "" && len(traceState) <= maxTraceStateLength && traceStatePattern.MatchString(traceState) {
		result.TraceState = traceState
	}
	return result, true
}

func FromRequest(r *http.Request) (Context, bool) {
	values := r.Header.Values("traceparent")
	if len(values) != 1 {
		return Context{}, false
	}
	return Parse(values[0], strings.Join(r.Header.Values("tracestate"), ","))
}

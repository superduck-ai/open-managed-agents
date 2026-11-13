package codesessions

import (
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
)

func traceConnection(request *http.Request, connected *atomic.Bool) *http.Request {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}
	return request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
}

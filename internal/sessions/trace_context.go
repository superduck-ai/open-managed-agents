package sessions

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/tracecontext"
)

func attachTraceContext(r *http.Request, events []db.SessionEvent) {
	trace, ok := tracecontext.FromRequest(r)
	if !ok {
		return
	}
	for index := range events {
		if events[index].EventType != "user.message" {
			continue
		}
		events[index].TraceParent = &trace.TraceParent
		if trace.TraceState != "" {
			events[index].TraceState = &trace.TraceState
		}
	}
}

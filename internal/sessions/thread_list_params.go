package sessions

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func parseThreadListParams(r *http.Request, session db.Session) (db.ListSessionThreadsPageParams, error) {
	limit, err := httpapi.ParseLimit(r, 1000)
	if err != nil {
		return db.ListSessionThreadsPageParams{}, err
	}
	if strings.TrimSpace(r.URL.Query().Get("limit")) == "" {
		limit = 500
	}
	cursor, err := decodeThreadCursor(r.URL.Query().Get("page"))
	if err != nil {
		return db.ListSessionThreadsPageParams{}, err
	}
	statuses, err := parseRepeatedStatuses(r)
	if err != nil {
		return db.ListSessionThreadsPageParams{}, err
	}
	return db.ListSessionThreadsPageParams{
		WorkspaceUUID:     session.WorkspaceUUID,
		SessionExternalID: session.ExternalID,
		Limit:             limit,
		Cursor:            cursor,
		Statuses:          statuses,
	}, nil
}

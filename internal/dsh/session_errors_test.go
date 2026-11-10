package dsh

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDSHSessionErrorsRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{db.ErrDSHSessionTerminated, 410, "session_terminated"},
		{db.ErrDSHSessionArchived, 410, "session_archived"},
		{db.ErrInvalidState, 400, "invalid_request"},
		{db.ErrNotFound, 404, "session_not_found"},
	} {
		t.Run(test.code, func(t *testing.T) {
			stub := &executionStub{err: fmt.Errorf("execution: %w", test.err)}
			response := transcriptRequest(executionRouter(stub), "POST", "/sessions/cse_root/inputs/input/finish", `{"result":"success"}`, "1")
			if response.Code != test.status || response.Header().Get("DSR-Error") != test.code {
				t.Fatalf("status=%d headers=%v", response.Code, response.Header())
			}
			var body struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Type != "error" || body.Error.Type == "" || body.Error.Message == "" {
				t.Fatalf("body=%s err=%v", response.Body.String(), err)
			}
		})
	}
}

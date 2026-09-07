package tests

import (
	"bufio"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestAcceptedToolEventsKeepTheirOwnerAfterChildCopies(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "stable-tool-owner", `{"model":"claude-opus-4-6","name":"stable-tool-owner"}`, `{"name":"stable-tool-owner"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	worker := launchLocalCodeSession(t, app, session.ID)
	child := "sthr_" + uuid.NewV4().String()
	postCodeSessionIngressEvents(t, app, worker, `{"events":[{"type":"session.thread_created","uuid":"create-child","session_thread_id":`+quoteJSON(child)+`,"agent_name":"child"}]}`)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	stream := openSessionEventStream(t, app, ctx, "/v1/sessions/"+session.ID+"/events/stream?beta=true")
	defer stream.Body.Close()
	// The result precedes any known use of late-tool. Neither accepted record
	// may move or disappear after the child transcript arrives.
	original := `{"events":[{"type":"agent.tool_use","uuid":"original-use","tool_use_id":"legacy-tool","name":"Bash"},{"type":"agent.tool_result","uuid":"original-result","tool_use_id":"late-tool","content":[{"type":"text","text":"accepted result"}]}]}`
	postCodeSessionIngressEvents(t, app, worker, original)
	before := listSessionEvents(t, app, session.ID, "types[]=agent.tool_use&types[]=agent.tool_result&order=asc&limit=100", defaultTestKey)
	if len(before.Data) != 2 {
		t.Fatalf("initial tool history = %s", before.Data)
	}
	scanner := bufio.NewScanner(stream.Body)
	for i, eventType := range []string{"agent.tool_use", "agent.tool_result"} {
		assertSessionEventJSONEqual(t, assertNextSessionFrameType(t, scanner, eventType), before.Data[i])
	}
	postCodeSessionIngressEvents(t, app, worker, `{"events":[{"type":"agent.tool_use","uuid":"child-use-copy","tool_use_id":"legacy-tool","session_thread_id":`+quoteJSON(child)+`,"name":"Bash"},{"type":"agent.tool_use","uuid":"late-child-use","tool_use_id":"late-tool","session_thread_id":`+quoteJSON(child)+`,"name":"Bash"}]}`)
	postCodeSessionIngressEvents(t, app, worker, original)
	after := listSessionEvents(t, app, session.ID, "types[]=agent.tool_use&types[]=agent.tool_result&order=asc&limit=100", defaultTestKey)
	if !reflect.DeepEqual(before.Data, after.Data) {
		t.Fatalf("later copies or retries changed accepted primary history: before=%s after=%s", before.Data, after.Data)
	}
	childEvents := listThreadEvents(t, app, session.ID, child, defaultTestKey)
	if len(childEvents.Data) != 2 || eventPageContains(childEvents, "accepted result") {
		t.Fatalf("old result was moved or child copies were lost: %s", childEvents.Data)
	}
}

func TestToolResultsResolveOnlyUniqueAcceptedOwners(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "resolved-tool-owner", `{"model":"claude-opus-4-6","name":"resolved-tool-owner"}`, `{"name":"resolved-tool-owner"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	worker := launchLocalCodeSession(t, app, response.ID)
	children := []string{"sthr_" + uuid.NewV4().String(), "sthr_" + uuid.NewV4().String()}
	for i, child := range children {
		postCodeSessionIngressEvents(t, app, worker, `{"events":[{"type":"session.thread_created","uuid":`+quoteJSON(child)+`,"session_thread_id":`+quoteJSON(child)+`,"agent_id":`+quoteJSON("agent-"+child)+`,"agent_name":`+quoteJSON([]string{"first", "second"}[i])+`}]}`)
	}
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !found {
		t.Fatalf("primary thread: found=%v err=%v", found, err)
	}
	for _, tc := range []struct {
		name, toolType, permission, sourceOwner string
		owners                                  []string
		wantOwner                               string
	}{
		{"normal child", "agent.tool_use", "allow", "", children[:1], children[0]},
		{"blocking crosspost", "agent.mcp_tool_use", "ask", "", children[:1], children[0]},
		{"custom crosspost", "agent.custom_tool_use", "allow", "", children[:1], children[0]},
		{"explicit owner wins", "agent.tool_use", "allow", children[1], children[:1], children[1]},
		{"ambiguous owners keep source", "agent.tool_use", "allow", "", children, primary.ExternalID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toolID := uuid.NewV4().String()
			for _, owner := range tc.owners {
				postCodeSessionIngressEvents(t, app, worker, `{"events":[{"uuid":`+quoteJSON(uuid.NewV4().String())+`,"type":`+quoteJSON(tc.toolType)+`,"tool_use_id":`+quoteJSON(toolID)+`,"session_thread_id":`+quoteJSON(owner)+`,"evaluated_permission":`+quoteJSON(tc.permission)+`,"name":"Bash"}]}`)
			}
			resultID := uuid.NewV4().String()
			result := `{"uuid":` + quoteJSON(resultID) + `,"id":` + quoteJSON(resultID) + `,"type":"agent.tool_result","tool_use_id":` + quoteJSON(toolID) + `,"content":[{"type":"text","text":"complete"}]`
			if tc.sourceOwner != "" {
				result += `,"owner_session_thread_id":` + quoteJSON(tc.sourceOwner)
			}
			result += `}`
			postCodeSessionIngressEvents(t, app, worker, `{"events":[`+result+`]}`)
			postCodeSessionIngressEvents(t, app, worker, `{"events":[`+result+`]}`)
			stored, err := app.db.GetSessionEvent(t.Context(), session.WorkspaceUUID, session.ExternalID, resultID)
			if err != nil || stored.ThreadExternalID == nil || *stored.ThreadExternalID != tc.wantOwner {
				t.Fatalf("result owner = %v, want %s: %v", stored.ThreadExternalID, tc.wantOwner, err)
			}
			var page sessionEventPageAPIResponse
			if tc.wantOwner == primary.ExternalID {
				page = listSessionEvents(t, app, response.ID, "types[]=agent.tool_result&limit=100", defaultTestKey)
			} else {
				page = listThreadEvents(t, app, response.ID, tc.wantOwner, defaultTestKey)
			}
			matches := 0
			for _, raw := range page.Data {
				if strings.Contains(string(raw), resultID) {
					matches++
					if sessionEventStringField(t, raw, "session_thread_id") != tc.wantOwner {
						t.Fatalf("public owner differs from durable owner: %s", raw)
					}
				}
			}
			if matches != 1 {
				t.Fatalf("result occurrences = %d, want 1", matches)
			}
		})
	}
}

package agents

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRejects257CharacterNameBeforeModelLookup(t *testing.T) {
	for _, character := range []string{"x", "界", "😀"} {
		raw, _ := json.Marshal(strings.Repeat(character, 257))
		h := NewHandler(nil, nil, nil)
		_, err := h.stateFromCreate(httptest.NewRequest("POST", "/v1/agents", nil), auth.Principal{}, "owned", &agentMutationRequest{Name: raw})
		if err == nil || err.Error() != "name must contain 1 to 256 characters" {
			t.Fatalf("character%q error%v", character, err)
		}
	}
}

func TestAgentNameBoundaryCountsCharactersAndPreservesValidInput(t *testing.T) {
	for _, character := range []string{"x", "界", "😀"} {
		name := strings.Repeat(character, 256)
		raw, _ := json.Marshal(name)
		got, err := parseAgentName(raw)
		if err != nil || got != name {
			t.Fatalf("valid name altered or rejected %v", err)
		}
	}
	for _, raw := range []json.RawMessage{nil, []byte(`null`), []byte(`""`), []byte(`"   "`), []byte(`1`)} {
		if _, err := parseAgentName(raw); err == nil {
			t.Fatalf("invalid name accepted%s", raw)
		}
	}
}

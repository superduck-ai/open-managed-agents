package managedagentsevents

import "testing"

func TestStableAssistantEventIDDoesNotNormalizeInputs(t *testing.T) {
	canonical := StableAssistantEventID("cse_test", "msg_test", 0, "agent.message")
	for name, eventID := range map[string]string{
		"code session id": StableAssistantEventID(" cse_test ", "msg_test", 0, "agent.message"),
		"message id":      StableAssistantEventID("cse_test", " msg_test ", 0, "agent.message"),
		"event type":      StableAssistantEventID("cse_test", "msg_test", 0, " agent.message "),
	} {
		if eventID == canonical {
			t.Fatalf("%s was normalized inside StableAssistantEventID", name)
		}
	}
}

func TestToolUseEventIDScopesChildInstances(t *testing.T) {
	const providerID = "toolu_functions.Write:0"
	primary := ToolUseEventID("cse_test", providerID, "")
	first := ToolUseEventID("cse_test", providerID, "child-a")
	second := ToolUseEventID("cse_test", providerID, "child-b")
	if first == second || first == primary || second == primary {
		t.Fatal("different execution instances share a tool ID")
	}
	if first != ToolUseEventID("cse_test", providerID, "child-a") {
		t.Fatal("same child call lost stable identity")
	}
	if primary != "sevt_cc9cd3653f85f6987897cec8008a6d07" {
		t.Fatal("primary tool identity changed")
	}
}

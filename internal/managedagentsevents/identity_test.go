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

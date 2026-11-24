package cloudharness

import "testing"

func TestMemoryStateStoreReplacesAndRemovesPendingChoice(t *testing.T) {
	ctx := t.Context()
	var store StateStore = NewMemoryStore()
	choice := UserChoice{ID: "choice", Prompt: "Initial", Choices: []string{"yes", "no"}}
	if err := store.AddPendingUserChoice(ctx, "thread", choice); err != nil {
		t.Fatal(err)
	}
	choice.Prompt = "Updated"
	if err := store.AddPendingUserChoice(ctx, "thread", choice); err != nil {
		t.Fatal(err)
	}
	choice.Choices[0] = "mutated by caller"
	choices, err := store.GetPendingUserChoices(ctx, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 1 || choices[0].Prompt != "Updated" || choices[0].Choices[0] != "yes" {
		t.Fatalf("unexpected pending choices %#v", choices)
	}
	if err := store.RemovePendingUserChoice(ctx, "thread", choice.ID); err != nil {
		t.Fatal(err)
	}
	choices, err = store.GetPendingUserChoices(ctx, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 0 {
		t.Fatalf("choice remained after removal: %#v", choices)
	}
}

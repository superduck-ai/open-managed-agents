package cloudharness

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

type modelFunc func(context.Context, []Message) (ModelCompleted, error)

func (f modelFunc) Complete(ctx context.Context, history []Message) (ModelCompleted, error) {
	return f(ctx, history)
}

type toolFunc func(context.Context, ToolCall) ToolResult

func (f toolFunc) Execute(ctx context.Context, call ToolCall) ToolResult {
	return f(ctx, call)
}

type senderFunc func(context.Context, InputMessage, string) error

func (f senderFunc) SendToInputQueue(ctx context.Context, message InputMessage, dedupID string) error {
	return f(ctx, message, dedupID)
}

type observerFunc func(ThreadID, AgentEvent)

func (f observerFunc) OnEvent(threadID ThreadID, event AgentEvent) {
	f(threadID, event)
}

func TestClaimedStepStopsWhenModelCommitFails(t *testing.T) {
	commitErr := errors.New("lease revoked")
	call := ToolCall{ID: "call", Name: "read", Arguments: "{}"}
	system := NewClaimedAgentSystem(ClaimedTask{
		Work: ModelWork{AutoExecuteTools: true},
		CompleteModel: func(context.Context) (ModelCompleted, error) {
			return ModelCompleted{Message: AssistantMessage{Call: call}}, nil
		},
		CommitModel: func(context.Context, Transition) error { return commitErr },
		DispatchTool: func(context.Context, ToolWork) error {
			t.Error("dispatched a tool after a failed commit")
			return nil
		},
	})
	_, err := system.Step(t.Context(), []StepInput{inputFor("thread", "", ResumeThread{})}, nil, nil)
	if !errors.Is(err, commitErr) {
		t.Fatalf("step error = %v", err)
	}
}

func TestClaimedStepCommitsModelBeforeSeparateToolTask(t *testing.T) {
	call := ToolCall{ID: "call", Name: "read", Arguments: "{}"}
	message := AssistantMessage{Call: call}
	var next Work
	var execution []string
	dispatch := func(_ context.Context, work ToolWork) error {
		execution = append(execution, "dispatch")
		if work.Call != call {
			t.Errorf("dispatched call = %+v, want %+v", work.Call, call)
		}
		return nil
	}
	model := NewClaimedAgentSystem(ClaimedTask{
		ModelPolicy: ModelWork{AutoExecuteTools: true},
		CompleteModel: func(context.Context) (ModelCompleted, error) {
			execution = append(execution, "model")
			return ModelCompleted{Message: message}, nil
		},
		CommitModel: func(_ context.Context, transition Transition) error {
			execution = append(execution, "commit")
			next = transition.Pending
			if !slices.Equal(transition.Messages, []Message{message}) {
				t.Errorf("committed messages = %+v", transition.Messages)
			}
			return nil
		},
		DispatchTool: dispatch,
	})
	user := inputFor("thread", "input", UserMessage{Text: "Read the file."})
	if _, err := model.Step(t.Context(), []StepInput{user}, nil, nil); err != nil {
		t.Fatal(err)
	}
	tool := NewClaimedAgentSystem(ClaimedTask{Work: next, DispatchTool: dispatch})
	resume := inputFor("thread", "", ResumeThread{})
	if _, err := tool.Step(t.Context(), []StepInput{resume}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(execution, []string{"model", "commit", "dispatch"}) {
		t.Fatalf("execution order = %v", execution)
	}
}

func TestClaimedStepResumesFromMatchingToolResult(t *testing.T) {
	call := ToolCall{ID: "current", Name: "read", Arguments: "{}"}
	calls, commits := 0, 0
	system := NewClaimedAgentSystem(ClaimedTask{
		Work: ToolWork{Call: call},
		CompleteModel: func(context.Context) (ModelCompleted, error) {
			calls++
			return ModelCompleted{Message: AssistantMessage{Text: "done"}}, nil
		},
		CommitModel: func(context.Context, Transition) error {
			commits++
			return nil
		},
	})
	stale := inputFor("thread", "old:result", ToolResult{CallID: "old"})
	if _, err := system.Step(t.Context(), []StepInput{stale}, nil, nil); !errors.Is(err, ErrUnexpectedInput) || calls != 0 || commits != 0 {
		t.Fatalf("stale result: model calls = %d, commits = %d, error = %v", calls, commits, err)
	}
	result := inputFor("thread", "current:result", ToolResult{CallID: call.ID})
	if _, err := system.Step(t.Context(), []StepInput{result}, nil, nil); err != nil || calls != 1 || commits != 1 {
		t.Fatalf("current result: model calls = %d, commits = %d, error = %v", calls, commits, err)
	}
}

func TestAgentSystemReemitsPendingApprovalWhenResumed(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	call := ToolCall{ID: "call", Name: "add"}
	if err := store.AppendMessages(ctx, "thread", []MessageWithID{
		{ID: "input", Message: UserMessage{Text: "original"}},
		{ID: "call", Message: AssistantMessage{Call: call}},
	}); err != nil {
		t.Fatal(err)
	}
	store.setPending("thread", ApprovalWork{Call: call})
	want := []AgentEvent{{Kind: ToolApprovalRequested, Call: call}}
	for _, input := range []StepInput{
		inputFor("thread", "input", UserMessage{Text: "replayed"}),
		inputFor("thread", "stale-approval", Approval{CallID: "stale", Granted: true}),
		inputFor("thread", "", ResumeThread{}),
	} {
		var events []AgentEvent
		observer := observerFunc(func(_ ThreadID, event AgentEvent) { events = append(events, event) })
		system := NewAgentSystem(store, nil, nil, nil)
		if _, err := system.Step(ctx, []StepInput{input}, observer, nil); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(events, want) {
			t.Fatalf("input %T produced events %#v, want %#v", input.Message.Content, events, want)
		}
	}
}

func TestAgentSystemRetriesToolDeliveryWithoutExecutingAgain(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	queue := &MemoryInputQueue{}
	queueError := errors.New("queue unavailable")
	failingSender := senderFunc(func(context.Context, InputMessage, string) error { return queueError })
	var executions int
	tool := toolFunc(func(_ context.Context, call ToolCall) ToolResult {
		executions++
		return ToolResult{CallID: call.ID, Text: "42"}
	})
	call := ToolCall{ID: "call", Name: "add", Arguments: "{}"}
	model := modelFunc(func(_ context.Context, history []Message) (ModelCompleted, error) {
		if len(history) == 1 {
			return ModelCompleted{Message: AssistantMessage{Call: call}}, nil
		}
		want := []Message{UserMessage{Text: "calculate"}, AssistantMessage{Call: call}, ToolResult{CallID: call.ID, Text: "42"}}
		if !slices.Equal(history, want) {
			return ModelCompleted{}, errors.New("model received unexpected history")
		}
		return ModelCompleted{Message: AssistantMessage{Text: "42"}}, nil
	})

	system := NewAgentSystem(store, model, tool, failingSender)
	if _, err := system.Step(ctx, []StepInput{inputFor("thread", "user", UserMessage{Text: "calculate"})}, nil, nil); err != nil {
		t.Fatal(err)
	}
	approval := []StepInput{inputFor("thread", "approval", Approval{CallID: call.ID, Granted: true})}
	if _, err := system.Step(ctx, approval, nil, nil); !errors.Is(err, queueError) {
		t.Fatalf("got %v, want queue error", err)
	}

	replacement := NewAgentSystem(store, model, tool, queue)
	outcome, err := replacement.Step(ctx, approval, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome[0].Outcome.(Skipped); !ok || executions != 1 {
		t.Fatalf("retry outcome=%#v tool executions=%d", outcome, executions)
	}
	err = queue.DeliverPending(ctx, 1, func(ctx context.Context, inputs []StepInput) error {
		completed, err := replacement.Step(ctx, inputs, nil, nil)
		if err == nil {
			outcome = completed
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome[0].Outcome.(Completed); !ok {
		t.Fatalf("tool result outcome=%#v, want Completed", outcome)
	}
}

func TestAgentSystemBatchesByThreadAndWaitsForOtherThreads(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	store := NewMemoryStore()
	modelError := errors.New("second thread model failed")
	started := make(chan struct{})
	var calls atomic.Int32
	model := modelFunc(func(ctx context.Context, history []Message) (ModelCompleted, error) {
		if calls.Add(1) == 2 {
			close(started)
		}
		select {
		case <-started:
		case <-ctx.Done():
			return ModelCompleted{}, ctx.Err()
		}
		if slices.Equal(history, []Message{UserMessage{Text: "error"}}) {
			return ModelCompleted{}, modelError
		}
		if !slices.Equal(history, []Message{UserMessage{Text: "first"}, UserMessage{Text: "last"}}) {
			return ModelCompleted{}, errors.New("thread inputs were not folded in arrival order")
		}
		return ModelCompleted{Message: AssistantMessage{Text: "done"}}, nil
	})
	system := NewAgentSystem(store, model, nil, nil)
	inputs := []StepInput{
		inputFor("first", "1", UserMessage{Text: "first"}),
		inputFor("second", "1", UserMessage{Text: "error"}),
		inputFor("first", "2", UserMessage{Text: "last"}),
	}
	if _, err := system.Step(ctx, inputs, nil, nil); !errors.Is(err, modelError) {
		t.Fatalf("got %v, want modelError", err)
	}
	history, err := store.LoadHistoryUpTo(ctx, "first", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2] != (AssistantMessage{Text: "done"}) {
		t.Fatalf("other thread did not commit: %#v", history)
	}
}

func TestAgentSystemDefersSyntheticInputUntilApprovalSettles(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	queue := &MemoryDeferQueue{}
	call := ToolCall{ID: "call", Name: "add"}
	if err := store.AppendMessages(ctx, "thread", []MessageWithID{
		{ID: "prior-user", Message: UserMessage{Text: "original"}},
		{ID: "call", Message: AssistantMessage{Call: call}},
	}); err != nil {
		t.Fatal(err)
	}
	store.setPending("thread", ApprovalWork{Call: call})
	modelError := errors.New("model unavailable with deferred input")
	attempts := 0
	model := modelFunc(func(_ context.Context, history []Message) (ModelCompleted, error) {
		if len(history) != 4 || history[3] != (UserMessage{Text: "deferred"}) {
			return ModelCompleted{}, errors.New("deferred input did not follow approval")
		}
		attempts++
		if attempts == 1 {
			return ModelCompleted{}, modelError
		}
		return ModelCompleted{Message: AssistantMessage{Text: "done"}}, nil
	})
	system := NewAgentSystem(store, model, nil, nil)
	synthetic := inputFor("thread", "event", UserMessage{Text: "deferred"})
	synthetic.Message.Synthetic = true
	outcome, err := system.Step(ctx, []StepInput{synthetic}, nil, queue)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome[0].Outcome.(Skipped); !ok {
		t.Fatalf("deferred input outcome=%#v, want Skipped", outcome)
	}
	replay := inputFor("thread", "prior-user", UserMessage{Text: "original"})
	outcome, err = system.Step(ctx, []StepInput{replay}, nil, queue)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome[0].Outcome.(Skipped); !ok {
		t.Fatalf("replayed user input released deferred work: %#v", outcome)
	}
	denial := inputFor("thread", "denial", Approval{CallID: call.ID})
	if _, err := system.Step(ctx, []StepInput{denial}, nil, queue); !errors.Is(err, modelError) {
		t.Fatalf("deferred completion error = %v, want %v", err, modelError)
	}
	outcome, err = system.Step(ctx, []StepInput{denial}, nil, queue)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome[0].Outcome.(Completed); !ok {
		t.Fatalf("settled input outcome=%#v, want Completed", outcome)
	}
}

func TestAgentSystemDeduplicatesInput(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	model := modelFunc(func(context.Context, []Message) (ModelCompleted, error) {
		return ModelCompleted{Message: AssistantMessage{Text: "hello"}}, nil
	})
	inputs := []StepInput{inputFor("thread", "input-id", UserMessage{Text: "hello"})}
	system := NewAgentSystem(store, model, nil, nil)
	if _, err := system.Step(ctx, inputs, nil, nil); err != nil {
		t.Fatal(err)
	}
	replacement := NewAgentSystem(store, nil, nil, nil)
	outcomes, err := replacement.Step(ctx, inputs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcomes[0].Outcome.(Skipped); !ok {
		t.Fatalf("replay = %#v, want Skipped", outcomes)
	}
}

func inputFor(threadID ThreadID, dedupID string, input Input) StepInput {
	return StepInput{Message: InputMessage{GroupID: threadID, Content: input}, DedupID: dedupID}
}

func TestAgentSystemAppendsInputAndOutputOnlyAfterModelSucceeds(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	modelError := errors.New("model unavailable")
	user := UserMessage{Text: "read"}
	answer := AssistantMessage{Text: "done"}
	input := []StepInput{inputFor("thread", "input", user)}
	failingModel := modelFunc(func(context.Context, []Message) (ModelCompleted, error) {
		return ModelCompleted{}, modelError
	})
	system := NewAgentSystem(store, failingModel, nil, nil)
	if _, err := system.Step(ctx, input, nil, nil); !errors.Is(err, modelError) {
		t.Fatalf("failed completion = %v, want %v", err, modelError)
	}
	history, err := store.LoadHistoryUpTo(ctx, "thread", nil, nil)
	if err != nil || len(history) != 0 {
		t.Fatalf("failed completion stored history: %+v, %v", history, err)
	}

	model := modelFunc(func(_ context.Context, history []Message) (ModelCompleted, error) {
		if !slices.Equal(history, []Message{user}) {
			return ModelCompleted{}, errors.New("retried model must see the input exactly once")
		}
		return ModelCompleted{Message: answer}, nil
	})
	replacement := NewAgentSystem(store, model, nil, nil)
	if _, err := replacement.Step(ctx, input, nil, nil); err != nil {
		t.Fatal(err)
	}
	history, err = store.LoadHistoryUpTo(ctx, "thread", nil, nil)
	if err != nil || !slices.Equal(history, []Message{user, answer}) {
		t.Fatalf("committed history = %+v, %v", history, err)
	}
}

func TestAgentSystemStoresInputMetadataOnRoot(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryStore()
	if err := store.EnsureRootThread(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	childID, err := store.SpawnThread(ctx, "root", "spawn", SpawnContext{})
	if err != nil {
		t.Fatal(err)
	}
	model := modelFunc(func(context.Context, []Message) (ModelCompleted, error) {
		return ModelCompleted{Message: AssistantMessage{Text: "ok"}}, nil
	})
	input := inputFor(childID, "input", UserMessage{Text: "hello"})
	input.Message.Metadata = []byte(`{"event":9007199254740993}`)
	system := NewAgentSystem(store, model, nil, nil)
	if _, err := system.Step(ctx, []StepInput{input}, nil, nil); err != nil {
		t.Fatal(err)
	}
	root, err := store.GetMetadata(ctx, "root")
	if err != nil || string(root) != string(input.Message.Metadata) {
		t.Fatalf("root metadata = %s, %v", root, err)
	}
	child, err := store.GetMetadata(ctx, childID)
	if err != nil || len(child) != 0 {
		t.Fatalf("child metadata = %s, %v", child, err)
	}
}

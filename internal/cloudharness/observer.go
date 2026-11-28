package cloudharness

type ThreadObserver interface {
	OnEvent(threadID ThreadID, event AgentEvent)
}

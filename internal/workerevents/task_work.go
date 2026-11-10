package workerevents

import "context"

type TaskWorkSignal interface {
	NotifyTaskWork(context.Context, string) error
	SubscribeTaskWork(context.Context, string) (<-chan struct{}, error)
}

func taskWorkSubject(codeSessionID string) (string, error) {
	if _, err := Subject(codeSessionID); err != nil {
		return "", err
	}
	return "oma.brain.task.available." + codeSessionID, nil
}

func (b *JetStreamBroker) NotifyTaskWork(_ context.Context, codeSessionID string) error {
	subject, err := taskWorkSubject(codeSessionID)
	if err != nil {
		return err
	}
	return b.connection.Publish(subject, nil)
}
func (b *JetStreamBroker) SubscribeTaskWork(ctx context.Context, codeSessionID string) (<-chan struct{}, error) {
	subject, err := taskWorkSubject(codeSessionID)
	if err != nil {
		return nil, err
	}
	return b.subscribeWorkSubject(ctx, subject)
}
func (b *MemoryBroker) NotifyTaskWork(_ context.Context, codeSessionID string) error {
	if _, err := taskWorkSubject(codeSessionID); err != nil {
		return err
	}
	b.brainWork.notify(codeSessionID)
	return nil
}
func (b *MemoryBroker) SubscribeTaskWork(ctx context.Context, codeSessionID string) (<-chan struct{}, error) {
	if _, err := taskWorkSubject(codeSessionID); err != nil {
		return nil, err
	}
	return b.brainWork.subscribe(ctx, codeSessionID)
}

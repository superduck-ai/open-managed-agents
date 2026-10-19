package codesessions

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type brainTaskPublisher interface {
	PublishBrainTask(context.Context, workerevents.BrainTask) error
}

func (s *Service) DispatchWorker(ctx context.Context, credentials ManagedAgentCreateResult) error {
	publisher, ok := s.workerEvents.(brainTaskPublisher)
	if !ok {
		return errWorkerDispatchUnavailable
	}
	return publisher.PublishBrainTask(ctx, workerevents.BrainTask{
		SessionID: credentials.CodeSessionID, WorkerEpoch: credentials.WorkerEpoch,
		IngressToken: credentials.SessionIngressToken, ModelToken: credentials.OAuthAccessToken,
	})
}

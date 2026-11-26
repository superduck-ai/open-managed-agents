package channels

import (
	"context"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func adapterConfig(worker *channelWorker, secret string) contracts.AdapterConfig {
	return contracts.AdapterConfig{
		AppID: worker.record.AppID, Secret: secret,
		IncludeThinking: worker.record.IncludeThinking, ReplyInThread: worker.record.ReplyInThread,
		FileMaxBytes: worker.supervisor.fileMaxBytes,
		Online:       worker.observeOnline, Receive: worker.receiveChannelMessage, Action: worker.handleAction,
		NewSession: worker.receiveNewSessionMenu,
		Prepare:    worker.prepareInputMessage, Context: worker.baseContext,
		Busy: func(scope string) bool {
			worker.mu.Lock()
			defer worker.mu.Unlock()
			queue := worker.scopes[scope]
			return queue != nil && queue.held || len(worker.replies[scope]) > 0
		},
	}
}

type Dependencies struct {
	Adapters         map[string]contracts.AdapterFactory
	Registrar        contracts.Registrar
	TokenURL         string
	ValidAppID       func(string) bool
	CheckCredentials func(context.Context, *http.Client, string, string, string) (bool, error)
}

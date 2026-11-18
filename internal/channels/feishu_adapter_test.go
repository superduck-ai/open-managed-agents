package channels

import (
	"context"
	"log/slog"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/channels/feishu"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type feishuChannel = feishu.Adapter

var feishuMessage = feishu.Message
var newFeishuInputPipelines = feishu.NewInputPipelines
var questionElements = feishu.QuestionElements

func (reply *channelReply) render() string { return feishu.Render(reply.view()) }
func (reply *channelReply) interactionElements() []map[string]any {
	return feishu.InteractionElements(reply.view())
}

func (worker *channelWorker) dispatchMessage(ctx context.Context, message *channeltypes.NormalizedMessage) error {
	normalized, err := feishu.NormalizeMessage(message)
	if err != nil {
		return err
	}
	return worker.receiveChannelMessage(ctx, normalized)
}
func (worker *channelWorker) handleCardAction(ctx context.Context, event *channeltypes.CardActionEvent) error {
	return worker.handleAction(ctx, feishu.NormalizeAction(event))
}

func testDependencies() Dependencies {
	return Dependencies{Adapters: map[string]contracts.AdapterFactory{"feishu": feishu.New}, Registrar: feishu.Registrar{}, TokenURL: feishu.TokenURL, ValidAppID: feishu.ValidAppID, CheckCredentials: feishu.MatchCredentials}
}
func testRuntimeSupervisor(store runtimeStore, cipher *secrets.Service, oma *omaClient, logger *slog.Logger) *runtimeSupervisor {
	supervisor := newRuntimeSupervisor(store, cipher, oma, logger)
	supervisor.adapters = testDependencies().Adapters
	return supervisor
}
func testLocalService(store localStore, cipher *secrets.Service, supervisor *runtimeSupervisor, logger *slog.Logger) *localService {
	return newLocalService(store, cipher, supervisor, logger, testDependencies())
}

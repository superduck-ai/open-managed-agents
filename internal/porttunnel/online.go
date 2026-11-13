package porttunnel

import (
	"context"

	"github.com/nats-io/nats.go"
)

const onlineSubjectPrefix = "oma.session_port.online."

type OnlineNotifier interface {
	Publish(codeSessionUUID string) error
	Subscribe(codeSessionUUID string) (<-chan struct{}, func(), error)
}

type NATSOnlineNotifier struct {
	connection *nats.Conn
}

func NewNATSOnlineNotifier(connection *nats.Conn) *NATSOnlineNotifier {
	return &NATSOnlineNotifier{connection: connection}
}

func onlineSubject(codeSessionUUID string) string {
	return onlineSubjectPrefix + codeSessionUUID
}

func (n *NATSOnlineNotifier) Publish(codeSessionUUID string) error {
	return n.connection.Publish(onlineSubject(codeSessionUUID), nil)
}

func (n *NATSOnlineNotifier) Subscribe(codeSessionUUID string) (<-chan struct{}, func(), error) {
	signal := make(chan struct{}, 1)
	subscription, err := n.connection.Subscribe(onlineSubject(codeSessionUUID), func(*nats.Msg) {
		select {
		case signal <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return signal, func() { _ = subscription.Unsubscribe() }, nil
}

func waitSignal(ctx context.Context, signals ...<-chan struct{}) error {
	cases := make(chan struct{}, 1)
	stop := make(chan struct{})
	defer close(stop)
	for _, signal := range signals {
		go func(signal <-chan struct{}) {
			select {
			case <-signal:
				select {
				case cases <- struct{}{}:
				default:
				}
			case <-stop:
			}
		}(signal)
	}
	select {
	case <-cases:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

package platformauth

import (
	"context"
	"errors"
	"log/slog"
)

type Delivery struct {
	Channel string
	Send    func(context.Context) error
}

func Deliver(ctx context.Context, logger *slog.Logger, deliveries ...Delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	results := make(chan bool, len(deliveries))
	for _, delivery := range deliveries {
		go func() {
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), smtpTimeout)
			defer cancel()
			err := delivery.Send(sendCtx)
			if err != nil {
				safeError := errNotificationDelivery
				if feishuError, ok := errors.AsType[*feishuDeliveryError](err); ok {
					safeError = feishuError
				}
				logger.WarnContext(sendCtx, "notification submission failed", "channel", delivery.Channel, "error", safeError)
			}
			results <- err == nil
		}()
	}
	for range deliveries {
		if <-results {
			return nil
		}
	}
	return errNotificationDelivery
}

type loginCodeSenders struct {
	smtp   LoginCodeSender
	feishu LoginCodeSender
	logger *slog.Logger
}

func (s *loginCodeSenders) SendLoginCode(ctx context.Context, recipient, code string) error {
	var deliveries []Delivery
	if s.smtp != nil {
		deliveries = append(deliveries, Delivery{Channel: "smtp", Send: func(ctx context.Context) error {
			return s.smtp.SendLoginCode(ctx, recipient, code)
		}})
	}
	if s.feishu != nil {
		deliveries = append(deliveries, Delivery{Channel: "feishu", Send: func(ctx context.Context) error {
			return s.feishu.SendLoginCode(ctx, recipient, code)
		}})
	}
	return Deliver(ctx, s.logger, deliveries...)
}

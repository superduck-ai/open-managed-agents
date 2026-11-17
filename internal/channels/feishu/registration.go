package feishu

import (
	"context"
	"errors"

	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

type Registrar struct{}

func registrationOptions(options *contracts.RegistrationOptions) *registration.Options {
	return &registration.Options{
		Source: "open-managed-agents", AppPreset: &registration.AppPreset{Name: options.Name},
		Addons: &registration.AppAddons{
			Scopes:    registration.AppAddonsScopes{Tenant: []string{"im:message", "im:message:send_as_bot", "im:chat:readonly"}},
			Events:    registration.AppAddonsEvents{Items: registration.AppAddonsEventItems{Tenant: []string{"im.message.receive_v1"}}},
			Callbacks: registration.AppAddonsCallbacks{Items: []string{"card.action.trigger"}},
		},
		OnQRCode:       func(info *registration.QRCodeInfo) { options.OnQRCode(info.URL, info.ExpireIn) },
		OnStatusChange: func(info *registration.StatusChangeInfo) { options.OnInterval(info.Interval) },
	}
}

func (Registrar) RegisterApp(ctx context.Context, options *contracts.RegistrationOptions) (*contracts.RegistrationResult, error) {
	result, err := registration.RegisterApp(ctx, registrationOptions(options))
	if _, ok := errors.AsType[*registration.AccessDeniedError](err); ok {
		return nil, errors.Join(contracts.ErrRegistrationDenied, err)
	}
	if _, ok := errors.AsType[*registration.ExpiredError](err); ok {
		return nil, errors.Join(contracts.ErrRegistrationExpired, err)
	}
	if err != nil {
		return nil, err
	}
	return &contracts.RegistrationResult{ClientID: result.ClientID, ClientSecret: result.ClientSecret}, nil
}

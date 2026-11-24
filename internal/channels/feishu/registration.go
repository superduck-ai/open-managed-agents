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
			Scopes: registration.AppAddonsScopes{
				Tenant: []string{
					"application:app_slash_command:read",
					"application:app_slash_command:write",
					"application:application:self_manage",
					"application:bot.basic_info:read",
					"application:bot.menu:write",
					"cardkit:card:read",
					"cardkit:card:write",
					"contact:contact.base:readonly",
					"docs:document.comment:create",
					"docs:document.comment:delete",
					"docs:document.comment:read",
					"docs:document.comment:update",
					"docs:document.comment:write_only",
					"docx:document.block:convert",
					"docx:document:readonly",
					"docx:document:write_only",
					"drive:drive.metadata:readonly",
					"im:chat.members:bot_access",
					"im:chat:create",
					"im:chat:read",
					"im:chat:update",
					"im:message",
					"im:message.group_at_msg.include_bot:readonly",
					"im:message.group_at_msg:readonly",
					"im:message.p2p_msg:readonly",
					"im:message.pins:read",
					"im:message.pins:write_only",
					"im:message.reactions:read",
					"im:message.reactions:write_only",
					"im:message:readonly",
					"im:message:send_as_bot",
					"im:message:send_multi_users",
					"im:message:send_sys_msg",
					"im:message:update",
					"im:resource",
					"wiki:node:read",
				},
				User: []string{
					"contact:user.employee_id:readonly",
					"offline_access",
				},
			},
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

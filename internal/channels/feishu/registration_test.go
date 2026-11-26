package feishu

import (
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func TestRegistrationAddonsSeparateCallbacks(t *testing.T) {
	options := registrationOptions(&contracts.RegistrationOptions{Name: "验收机器人"})
	if options.Source != "open-managed-agents" || options.AppPreset.Name != "验收机器人" {
		t.Fatal("飞书应用注册预置不完整")
	}
	addons := options.Addons
	if len(addons.Scopes.Tenant) == 0 {
		t.Fatal("应用权限预置缺失")
	}
	if slices.Contains(addons.Events.Items.Tenant, "card.action.trigger") {
		t.Fatal("card.action.trigger 不应放在事件订阅里")
	}
	if !slices.Contains(addons.Callbacks.Items, "card.action.trigger") {
		t.Fatal("card.action.trigger 应注册为回调")
	}
	if !slices.Contains(addons.Events.Items.Tenant, "im.message.receive_v1") {
		t.Fatal("消息事件订阅缺失")
	}
	if !slices.Contains(addons.Events.Items.Tenant, "application.bot.menu_v6") {
		t.Fatal("机器人菜单事件订阅缺失")
	}
}

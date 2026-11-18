package invitations

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestInvitationFeishuCard(t *testing.T) {
	organization := "研发 <at id=all></at> **管理员**"
	message, err := invitationFeishuMessage(organization, "admin@example.com", "https://oma.example.com/invites", time.Date(2026, 12, 1, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Schema string `json:"schema"`
		Config struct {
			WidthMode string `json:"width_mode"`
		} `json:"config"`
		Header struct {
			Title struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
		Body struct {
			Elements []struct {
				Tag   string `json:"tag"`
				Type  string `json:"type"`
				Width string `json:"width"`
				Text  *struct {
					Tag      string `json:"tag"`
					Content  string `json:"content"`
					TextSize string `json:"text_size"`
				} `json:"text"`
				Behaviors []struct {
					Type       string `json:"type"`
					DefaultURL string `json:"default_url"`
				} `json:"behaviors"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal(message, &card); err != nil {
		t.Fatal(err)
	}
	if card.Schema != "2.0" || card.Config.WidthMode != "compact" || card.Header.Title.Content != "组织邀请" || card.Header.Title.Tag != "plain_text" {
		t.Fatal("card must have a short organization invitation title")
	}
	var texts []string
	buttons := 0
	organizationEmphasized := false
	for _, element := range card.Body.Elements {
		if element.Text != nil {
			if element.Text.Tag != "plain_text" {
				t.Fatal("invitation details must not interpret user-controlled text as markup or mentions")
			}
			texts = append(texts, element.Text.Content)
			if element.Text.Content == organization && element.Text.TextSize == "heading-2" {
				organizationEmphasized = true
			}
		}
		if element.Tag == "button" {
			buttons++
			if element.Type != "primary_filled" || element.Width != "fill" || element.Text == nil || element.Text.Content != "查看邀请" || len(element.Behaviors) != 1 || element.Behaviors[0].Type != "open_url" || element.Behaviors[0].DefaultURL != "https://oma.example.com/invites" {
				t.Fatal("invitation button must open the configured invitation page without a callback")
			}
		}
	}
	if buttons != 1 || !organizationEmphasized {
		t.Fatal("invitation must emphasize the organization and one link button")
	}
	text := strings.Join(texts, "\n")
	for _, part := range []string{organization, "admin@example.com", "2026-12-01 16:00（北京时间）", "使用受邀邮箱登录后确认加入"} {
		if !strings.Contains(text, part) {
			t.Fatalf("card omitted %q", part)
		}
	}
}

func TestPlatformInvitationFeishuIdentity(t *testing.T) {
	fixture := &mailFixture{}
	mailer := NewMailer(config.AuthConfig{ConsoleURL: "https://oma.example.com", Feishu: config.FeishuConfig{AppID: "cli_test", AppSecret: "secret"}}, fixture, nil)
	mailer.feishu = fixture
	if got := mailer.NotifyPlatform(t.Context(), "target-org", "recipient@example.com", time.Now().Add(time.Hour)); got != "sent" {
		t.Fatal(got)
	}
	if fixture.memberLookups != 0 || !strings.Contains(fixture.message, "平台管理员") || strings.Contains(fixture.message, "admin@example.com") {
		t.Fatal("platform invitation must not impersonate an organization member")
	}
}

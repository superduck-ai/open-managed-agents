# Handtest 2026-11-10 — channels volc align

Local real-device check for PR #624 / lark-channel session metadata.

- OMA worktree env: `open-managed-agents-0003`
- Bridge: `node dist/managed-channels.js` on `127.0.0.1:39100`
- Feishu app: `cli_aa6c7433e1b8dbf1` (验收 Bot C2 20261101)
- Agent: `channels-handtest` / `agent_oKzV3sWkIy5LT3hkLwcTG9t4`
- Channel: `handtest-feishu` / `channel_c87a04e6-97a7-4431-9c6e-aa660abc7936`
- OMA console login (SMTP 未配置，任意非空 OTP): `test@oma.ai`

Screenshots:
- `01-channels-list.png`
- `02-channel-detail.png`
- `03-channel-sessions.png`

布局说明：渠道列表 / 详情 / 会话管理已与部署、环境页一致，走控制台宽页面（约 `max-w-[1440px]` + `px-6 py-6 lg:px-8`），不再套居中窄卡片。以上三张为改宽后重拍。

Session metadata evidence: `session-metadata.json`

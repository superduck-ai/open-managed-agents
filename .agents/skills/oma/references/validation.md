# Validation scope

Live validation used ant 2.5.0 against https://oma.gz.cvte.cn on 2026-11-04/05. The address is a test target, not a default. Model: deepseek/deepseek-flash. No real API key is included. These were direct CLI/API tests, not independent execution of the full upstream skill.

Passed:

- Invalid key: 401; missing Agent: 404; stale Agent version: 409.
- Model listing and Agent configuration dry-run, creation, unchanged reapply, and update.
- Existing and newly created environments; a limited environment completed two conversational turns.
- Initial response and subsequent recovery of the preceding input's passphrase.
- Event pagination with disjoint page IDs.
- Agent update to v2 while an existing Session retained its v1 snapshot.
- File upload and deletion.
- Earlier smoke sessions returned OMA_ONBOARD_OK and idle/end_turn.

Limitations:

- Uploaded file download returned 400 with downloadable=false.
- Raw HTTP SSE delivered events; ant stream produced no output during observation.
- Environment requests required beta=true on the tested instance.
- No validation of network blocking, tool confirmation, generated artifacts, MCP, Vault, Memory, Deployment, concurrent recovery, other SDKs, or all upstream skill features.
- Connection isolation and bounded waiting are instructions, not executable enforcement helpers.

Retained inspection resources: Agent agent_wtoikXWMG3g9djQjBUMm8bf9 and Environment env_sL1RWEcMWKIm0at9Iw5KjcKy. Do not use them as defaults for new users.

Skill structure and repository hygiene checks passed. No application code changed and verify-be was not run. Revalidate deployment-specific differences when changing the target or upgrading ant.

"""Run real Claude clients against an externally supplied Tunnel test fixture.

The auth file is parsed as data, never sourced as shell code. Only bounded,
credential-free evidence is printed; the random marker is never in the prompt.
"""

import asyncio
import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import sys

PROMPT = "Call mcp__tunnel__tunnel_proof exactly once. Return the marker from its result verbatim. Do not guess it."


def load_auth():
    allowed = {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"}
    for line in Path(os.environ["TEST_CLAUDE_AUTH_FILE"]).read_text().splitlines():
        line = line.strip().removeprefix("export ")
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        if key in allowed:
            parts = shlex.split(value)
            if len(parts) != 1:
                raise ValueError("auth file must contain single-value environment assignments")
            os.environ[key] = parts[0]
    os.environ["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
    os.environ["CLAUDE_CONFIG_DIR"] = str(Path.cwd() / "claude-config")


def run_process(arguments, timeout=110, cwd=None):
    process = subprocess.Popen(arguments, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        output, errors = process.communicate(timeout=timeout)
    except BaseException:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()
        raise
    if process.returncode:
        detail = (output + errors)[-4000:]
        for key in ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"):
            if os.environ.get(key):
                detail = detail.replace(os.environ[key], "<redacted>")
        raise RuntimeError(f"client exited {process.returncode}: {detail}")
    return output


async def run_python():
    from claude_agent_sdk import AssistantMessage, ClaudeAgentOptions, ResultMessage, SystemMessage, ToolUseBlock, query

    evidence = {"client": "python", "tool_calls": 0, "result_verified": False, "connected": False}
    options = ClaudeAgentOptions(
        model=os.environ["TEST_CLAUDE_MODEL"],
        cli_path=os.environ["TEST_CLAUDE_CLI"],
        cwd=str(Path.cwd()),
        setting_sources=[],
        tools=[],
        allowed_tools=["mcp__tunnel__tunnel_proof"],
        mcp_servers={"tunnel": {"type": "http", "url": os.environ["TEST_TUNNEL_ENDPOINT"]}},
        max_turns=4,
        extra_args={"no-session-persistence": None},
    )
    async for message in query(prompt=PROMPT, options=options):
        if isinstance(message, SystemMessage) and message.subtype == "init":
            evidence["connected"] = any(server.get("name") == "tunnel" and server.get("status") == "connected" for server in message.data.get("mcp_servers", []))
        if isinstance(message, AssistantMessage):
            evidence["tool_calls"] += sum(isinstance(block, ToolUseBlock) and block.name == "mcp__tunnel__tunnel_proof" for block in message.content)
        if isinstance(message, ResultMessage):
            evidence["result_verified"] = message.subtype == "success" and not message.is_error and os.environ["TEST_EXPECTED_MARKER"] in (message.result or "")
            evidence["result_subtype"] = message.subtype
    return evidence


def run_cli():
    config = Path.cwd() / "mcp.json"
    config.write_text(json.dumps({"mcpServers": {"tunnel": {"type": "http", "url": os.environ["TEST_TUNNEL_ENDPOINT"]}}}))
    config.chmod(0o600)
    output = run_process([
        os.environ["TEST_CLAUDE_CLI"], "-p", PROMPT,
        "--model", os.environ["TEST_CLAUDE_MODEL"], "--output-format", "stream-json", "--verbose",
        "--tools", "", "--allowedTools", "mcp__tunnel__tunnel_proof", "--max-turns", "4",
        "--setting-sources", "", "--strict-mcp-config", "--mcp-config", str(config), "--no-session-persistence",
    ])
    evidence = {"client": "cli", "tool_calls": 0, "result_verified": False, "connected": False}
    for line in output.splitlines():
        message = json.loads(line)
        if message.get("type") == "system" and message.get("subtype") == "init":
            evidence["connected"] = any(server.get("name") == "tunnel" and server.get("status") == "connected" for server in message.get("mcp_servers", []))
        if message.get("type") == "assistant":
            evidence["tool_calls"] += sum(block.get("type") == "tool_use" and block.get("name") == "mcp__tunnel__tunnel_proof" for block in message["message"]["content"])
        if message.get("type") == "result":
            evidence["result_verified"] = message.get("subtype") == "success" and not message.get("is_error") and os.environ["TEST_EXPECTED_MARKER"] in message.get("result", "")
            evidence["result_subtype"] = message.get("subtype")
    return evidence


def main():
    load_auth()
    client = sys.argv[1]
    if client == "managed":
        os.environ["TEST_MANAGED_TUNNEL_E2E"] = "1"
        run_process(["go", "test", "-tags=e2e", "./tests", "-run", "^TestManagedAgentNATSTunnelE2E$", "-count=1", "-v", "-timeout=9m"], timeout=570, cwd=Path(__file__).resolve().parents[3])
        print(json.dumps({"client": "managed", "gateway_and_private_tool_verified": True}))
        return
    if client == "typescript":
        output = run_process(["node", str(Path(__file__).with_name("tunnel-client.mjs"))])
        evidence = json.loads(output.strip().splitlines()[-1])
    elif client == "python":
        evidence = asyncio.run(asyncio.wait_for(run_python(), timeout=110))
    elif client == "cli":
        evidence = run_cli()
    else:
        raise ValueError("unknown client")
    print(json.dumps(evidence))
    if evidence["tool_calls"] != 1 or not evidence["result_verified"]:
        raise RuntimeError("real tool invocation or final marker verification failed")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        detail = str(error)
        for key in ("ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"):
            if os.environ.get(key):
                detail = detail.replace(os.environ[key], "<redacted>")
        print(json.dumps({"error": type(error).__name__, "detail": detail[:4000]}))
        sys.exit(1)

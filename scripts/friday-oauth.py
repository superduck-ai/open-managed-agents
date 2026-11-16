#!/usr/bin/env python3
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import urllib.error
import urllib.request
import uuid


arguments = sys.argv[1:]
directory = Path(__file__).resolve().parent
search_path = os.pathsep.join(
    part for part in os.get_exec_path() if Path(part).resolve() != directory
)
executable = os.environ.get("OMA_FRIDAY_EXECUTABLE") or shutil.which(
    "friday", path=search_path
)
if not executable:
    sys.exit("未找到 Friday CLI 可执行文件。")

environment = os.environ.copy()
provider = environment.get("OMA_FRIDAY_OAUTH_PROVIDER")
offline = any(argument in {"-h", "--help", "--version"} for argument in arguments)
if provider and not offline:
    if any(argument == "--api-key" or argument.startswith("--api-key=") for argument in arguments):
        sys.exit("OMA 个人 OAuth 入口不接受 API Key。")
    call_id = environment.get("OMA_TOOL_CALL_ID")
    call_uuid = environment.get("OMA_TOOL_CALL_UUID")
    if not call_id or not call_uuid:
        sys.exit("Friday OAuth 必须在 OMA 当前 Bash 工具中执行。")
    request = urllib.request.Request(
        "http://oma.internal/oauth",
        data=json.dumps(
            {
                "kind": "env_oauth",
                "provider": provider,
                "secret_name": "FRIDAY_4A_TOKEN",
                "parent_call_id": call_id,
                "invocation_id": str(uuid.uuid4()),
                "reason": "missing",
            }
        ).encode(),
        headers={"Content-Type": "application/json", "X-OMA-Tool-Call": call_uuid},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=None) as response:
            result = json.load(response)
    except (urllib.error.URLError, OSError):
        sys.exit("Friday 授权未完成，当前命令未执行；请查看原对话。")
    if result.get("authorized") is not True or not isinstance(
        result.get("placeholder"), str
    ):
        sys.exit("Friday 授权未就绪，当前命令未执行。")
    if result.get("authorization_completed") is True:
        print("Friday OAuth 授权已完成，继续当前命令。", flush=True)
    environment.pop("FRIDAY_API_KEY", None)
    environment["FRIDAY_4A_TOKEN"] = result["placeholder"]
    environment["FRIDAY_CLI_CONFIG_DIR"] = tempfile.mkdtemp(prefix="oma-friday-")
    environment["FRIDAY_PROXY_AUTH"] = "0"
    environment["FRIDAY_SKIP_SSL_VERIFY"] = "false"

os.execvpe(executable, [executable, *arguments], environment)

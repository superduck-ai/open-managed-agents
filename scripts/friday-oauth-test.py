import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import tempfile
import threading


class OAuthProxy(BaseHTTPRequestHandler):
    completed = False

    def log_message(self, *arguments):
        pass

    def do_POST(self):
        assert self.path == "http://oma.internal/oauth"
        assert self.headers["X-OMA-Tool-Call"] == "call-uuid"
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        assert request["parent_call_id"] == "call-id"
        body = json.dumps({"authorized": True, "authorization_completed": self.completed, "placeholder": "oma_ph_test"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


with tempfile.TemporaryDirectory() as directory:
    executable = Path(directory) / "friday-real"
    executable.write_text('#!/bin/sh\nprintf "executed"\n')
    executable.chmod(0o755)
    environment = os.environ.copy()
    environment.update(
        OMA_FRIDAY_EXECUTABLE=str(executable), OMA_FRIDAY_OAUTH_PROVIDER="custom:test"
    )
    environment.pop("OMA_TOOL_CALL_ID", None)
    environment.pop("OMA_TOOL_CALL_UUID", None)
    script = str(Path(__file__).with_name("friday-oauth.py"))
    for arguments in [["app", "list"], ["app", "list", "--api-key=test"]]:
        result = subprocess.run(
            ["python3", script, *arguments],
            env=environment, capture_output=True, text=True, timeout=3,
        )
        assert result.returncode != 0 and "executed" not in result.stdout
    result = subprocess.run(
        ["python3", script, "--help"],
        env=environment, capture_output=True, text=True, timeout=3,
    )
    assert result.returncode == 0 and result.stdout == "executed"
    with ThreadingHTTPServer(("127.0.0.1", 0), OAuthProxy) as server:
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        environment.update(OMA_TOOL_CALL_ID="call-id", OMA_TOOL_CALL_UUID="call-uuid", http_proxy=f"http://127.0.0.1:{server.server_port}", no_proxy="")
        environment.pop("NO_PROXY", None)
        for completed in [False, True]:
            OAuthProxy.completed = completed
            result = subprocess.run(["python3", script, "app", "list"], env=environment, capture_output=True, text=True, timeout=3)
            assert result.returncode == 0 and result.stdout.endswith("executed")
            assert ("OAuth 授权已完成" in result.stdout) == completed
            assert "oma_ph_test" not in result.stdout + result.stderr
        server.shutdown()
        worker.join()

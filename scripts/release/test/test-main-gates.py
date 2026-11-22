#!/usr/bin/env python3
"""執行 check-main-gates.py：只有 ci.yml 與 browser-tests.yml 都精確 success 才放行。"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import parse_qs

ROOT = Path(__file__).resolve().parents[3]
SCRIPT = ROOT / "scripts" / "release" / "check-main-gates.py"
SHA = "400c7edcc6715e7f90d387fa37bf1c6fc83e9e24"
TOKEN = "fixture-token-SHOULD-NOT-APPEAR"
REPO = "fixture-owner/fixture-repo"
FAKE_GH = r"""
import json, os, sys
from urllib.parse import parse_qs
argv = sys.argv[1:]

def values(flag):
    out, i = [], 0
    while i < len(argv):
        item = argv[i]
        if item == flag and i + 1 < len(argv):
            out.append(argv[i + 1])
            i += 2
            continue
        if item.startswith(flag + "="):
            out.append(item.split("=", 1)[1])
        i += 1
    return out

method = (values("--method") or values("-X") or [""])[0]
endpoint = next((item for item in argv if item.startswith("repos/") or item.startswith("http://") or item.startswith("https://")), "")
path, _, query = endpoint.partition("?")
fields = {}
for item in values("-f") + values("-F"):
    if "=" in item:
        key, value = item.split("=", 1)
        fields[key] = value
for key, value in parse_qs(query).items():
    fields.setdefault(key, value[0])
problems = []
if method != "GET":
    problems.append("method is not GET")
if "/actions/workflows/" not in path or not path.rstrip("/").endswith("/runs"):
    problems.append("endpoint mismatch")
if fields.get("head_sha") != os.environ.get("GITHUB_SHA", ""):
    problems.append("head_sha query mismatch")
if fields.get("branch") != "main":
    problems.append("branch query mismatch")
if fields.get("event") != "push":
    problems.append("event query mismatch")
if problems:
    sys.stderr.write("; ".join(problems))
    sys.exit(2)
workflow = path.split("/actions/workflows/", 1)[1].split("/runs", 1)[0]
data = json.loads(open(os.environ["GATE_FIXTURE"], encoding="utf-8").read())
item = data[workflow]
sys.stderr.write(item.get("stderr") or "")
if item.get("exit", 0) == 0:
    json.dump(item["body"], sys.stdout)
sys.exit(item.get("exit", 0))
"""


def run_body(workflow, **over):
    body = {
        "head_sha": SHA,
        "head_branch": "main",
        "event": "push",
        "path": f".github/workflows/{workflow}",
        "status": "completed",
        "conclusion": "success",
    }
    body.update(over)
    return body


def success_fixture():
    return {
        name: {"exit": 0, "stderr": "", "body": {"total_count": 1, "workflow_runs": [run_body(name)]}}
        for name in ("ci.yml", "browser-tests.yml")
    }


def with_run(workflow, **over):
    fixture = success_fixture()
    fixture[workflow]["body"]["workflow_runs"] = [run_body(workflow, **over)]
    return fixture


def child_env(base, folder):
    env = base.copy()
    env["PATH"] = str(folder) + os.pathsep + env.get("PATH", "")
    env["PYTHONUTF8"] = "1"
    env["PYTHONIOENCODING"] = "utf-8"
    env["GITHUB_SHA"] = env.get("GITHUB_SHA", SHA)
    env["GITHUB_REPOSITORY"] = env.get("GITHUB_REPOSITORY", REPO)
    env["GH_TOKEN"] = TOKEN
    env["GITHUB_TOKEN"] = TOKEN
    return env


def invoke(fixture):
    if not SCRIPT.is_file():
        return 127, "", "check-main-gates.py 不存在"
    with tempfile.TemporaryDirectory() as temp:
        folder = Path(temp)
        (folder / "fixture.json").write_text(json.dumps(fixture), encoding="utf-8")
        (folder / "gate_fake_gh.py").write_text(FAKE_GH, encoding="utf-8")
        (folder / "gh.cmd").write_text(
            '@echo off\r\npython "%~dp0gate_fake_gh.py" %*\r\n',
            encoding="ascii",
        )
        env = child_env(os.environ, folder)
        env["GATE_FIXTURE"] = str(folder / "fixture.json")
        env["GITHUB_SHA"] = fixture.get("sha", SHA)
        env["GITHUB_REPOSITORY"] = fixture.get("repo", REPO)
        if fixture.get("drop_sha"):
            env.pop("GITHUB_SHA", None)
        if fixture.get("drop_repo"):
            env.pop("GITHUB_REPOSITORY", None)
        proc = subprocess.run(
            [sys.executable, str(SCRIPT)],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=30,
        )
        return proc.returncode, proc.stdout, proc.stderr


def expect_fail(name, fixture, reason):
    code, out, err = invoke(fixture)
    text = out + err
    if code == 0:
        return f"FAIL {name}（預期阻擋卻放行）"
    if reason not in text:
        return f"FAIL {name}（退出 {code}，缺少原因 {reason!r}）\n{text}"
    if TOKEN in text:
        return f"FAIL {name}（輸出含 token）"
    return ""


def observe_transport():
    real = shutil.which("gh")
    if not real:
        return "FAIL loopback 找不到 gh"
    captured = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self._take("GET")

        def do_POST(self):
            self._take("POST")

        def _take(self, method):
            captured.append({"method": method, "target": self.path})
            body = b'{"message":"Method Not Allowed"}'
            self.send_response(405)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, fmt, *args):
            return

    server = HTTPServer(("127.0.0.1", 0), Handler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            (folder / "gate_transport_gh.py").write_text(
                "import os, sys, subprocess\n"
                "argv = sys.argv[1:]\n"
                "out = [('http://127.0.0.1:%s/%s' % (os.environ['GATE_PORT'], item)) if item.startswith('repos/') else item for item in argv]\n"
                "raise SystemExit(subprocess.call([os.environ['REAL_GH'], *out]))\n",
                encoding="utf-8",
            )
            (folder / "gh.cmd").write_text(
                '@echo off\r\npython "%~dp0gate_transport_gh.py" %*\r\n',
                encoding="ascii",
            )
            config = folder / "empty-config"
            config.mkdir()
            env = child_env(os.environ, folder)
            env["REAL_GH"] = real
            env["GATE_PORT"] = str(port)
            env["GH_CONFIG_DIR"] = str(config)
            env["GH_TOKEN"] = "fixture-token-not-real"
            env.pop("GITHUB_TOKEN", None)
            env.pop("GH_ENTERPRISE_TOKEN", None)
            env.pop("GITHUB_ENTERPRISE_TOKEN", None)
            env["GITHUB_SHA"] = SHA
            env["GITHUB_REPOSITORY"] = REPO
            proc = subprocess.run(
                [sys.executable, str(SCRIPT)],
                cwd=ROOT,
                env=env,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=30,
            )
    finally:
        server.shutdown()
        server.server_close()
    text = (proc.stdout or "") + (proc.stderr or "")
    if "fixture-token-not-real" in text or TOKEN in text:
        return "FAIL loopback 輸出含 token"
    if not captured:
        return f"FAIL loopback 沒有請求（gate exit {proc.returncode}）"
    first = captured[0]
    path, _, query = first["target"].partition("?")
    got = parse_qs(query)
    ok = (
        first["method"] == "GET"
        and path == "/repos/fixture-owner/fixture-repo/actions/workflows/ci.yml/runs"
        and got.get("head_sha") == [SHA]
        and got.get("branch") == ["main"]
        and got.get("event") == ["push"]
    )
    if not ok:
        return "FAIL loopback " + json.dumps(
            {"method": first["method"], "path": path, "query": query}, ensure_ascii=False)
    return ""


def main():
    errors = []
    code, out, err = invoke(success_fixture())
    if code != 0:
        errors.append(f"FAIL 兩條精確 success 應放行（exit {code}）\n{out}{err}")
    elif TOKEN in out + err:
        errors.append("FAIL 成功輸出含 token")
    else:
        print("PASS 兩條精確 success 放行")

    cases = [
        ("缺少 run", {**success_fixture(), "ci.yml": {"exit": 0, "stderr": "", "body": {"total_count": 0, "workflow_runs": []}}}, "應恰好一個"),
        ("錯 SHA", with_run("ci.yml", head_sha="0" * 40), "head_sha"),
        ("錯 branch", with_run("browser-tests.yml", head_branch="v0.24.0"), "branch"),
        ("錯 workflow", with_run("ci.yml", path=".github/workflows/other.yml"), "workflow path"),
        ("錯 event", with_run("ci.yml", event="pull_request"), "event"),
        ("in_progress", with_run("ci.yml", status="in_progress", conclusion=None), "尚未 completed"),
        ("failure", with_run("browser-tests.yml", conclusion="failure"), "不是 success"),
        ("skipped", with_run("ci.yml", conclusion="skipped"), "不是 success"),
        ("API 非 0", {**success_fixture(), "ci.yml": {"exit": 1, "stderr": "HTTP 500 " + TOKEN, "body": {}}}, "查詢失敗（gh exit 1）：HTTP 500"),
        ("缺少 GITHUB_SHA", {"drop_sha": True}, "GITHUB_SHA 未設定"),
        ("缺少 GITHUB_REPOSITORY", {"drop_repo": True}, "GITHUB_REPOSITORY 未設定"),
    ]
    for name, fixture, reason in cases:
        message = expect_fail(name, fixture, reason)
        if message:
            errors.append(message)
        else:
            print("PASS 阻擋 " + name)
    transport = observe_transport()
    if transport:
        errors.append(transport)
    else:
        print("PASS loopback GET query")

    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print("main gates fixture ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())

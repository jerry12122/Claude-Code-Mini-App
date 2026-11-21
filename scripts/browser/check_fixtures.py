"""Run directly: validate fixture boundaries, credential redaction, and safe diagnostics."""
import ast
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import os
from pathlib import Path
import ssl
import subprocess
import tempfile
import threading
from types import SimpleNamespace
from urllib.parse import quote
import urllib.request

from conftest import pytest_runtest_makereport
from test_ui_review import is_session_path, mask_sensitive, sanitize_url

repo = Path(__file__).resolve().parents[2]
for relative in ("scripts/browser/conftest.py", "scripts/chat-autoscroll.test.py", "scripts/e2e/run_e2e.py"):
    tree = ast.parse((repo / relative).read_text(encoding="utf-8"))
    assert any(isinstance(node, ast.Dict) and any(
        isinstance(key, ast.Constant) and key.value == "MINIAPP_TEST_LOOPBACK"
        and isinstance(value, ast.Constant) and value.value == "1"
        for key, value in zip(node.keys, node.values)) for node in ast.walk(tree)), relative
print("Browser, auto-scroll and desktop test child environments explicitly enable loopback")

# Existing check_fixtures makereport credential redaction test
canary = "synthetic-browserstack-access-key"
for key, trace, output, expected_trace, expected_output in (
    (canary, "trace " + canary, "log " + canary, "trace [redacted]", "log [redacted]"),
    (canary, None, "log " + canary, None, "log [redacted]"),
    ("", "local trace", "local log", "local trace", "local log"),
):
    os.environ["BROWSERSTACK_ACCESS_KEY"] = key
    report = SimpleNamespace(longrepr=trace, sections=[("stdout", output)])
    hook = pytest_runtest_makereport()
    next(hook)
    try:
        hook.send(SimpleNamespace(get_result=lambda: report))
    except StopIteration:
        pass
    assert report.longrepr == expected_trace
    assert report.sections == [("stdout", expected_output)]
print("Failure reports redact credentials from traceback and captured output")

# 1. PM Counterexample: userinfo, query, fragment completely stripped from freeform text
pm_input = "failed https://fixture-userinfo@bs-local.com/sessions/id?note=fixture-query#fixture-fragment"
actual_cleaned = mask_sensitive(pm_input)
assert "fixture-userinfo" not in actual_cleaned, "userinfo must be stripped"
assert "note=fixture-query" not in actual_cleaned, "query must be stripped"
assert "fixture-fragment" not in actual_cleaned, "fragment must be stripped"
assert actual_cleaned == "failed https://bs-local.com/sessions/id"
print("PM counterexample verified: userinfo, query, and fragment stripped from freeform message")

# 2. User-only authority in freeform text
u_input = "error at https://user-only@bs-local.com/sessions/id?q=1#frag"
u_res = mask_sensitive(u_input)
assert "user-only" not in u_res and "q=1" not in u_res and "frag" not in u_res
assert u_res == "error at https://bs-local.com/sessions/id"
print("User-only authority stripped from freeform URL")

# 3. Relative URLs with query/fragment in freeform text
rel_input = "failed navigation to /sessions/abc?token=secret#top"
rel_res = mask_sensitive(rel_input)
assert "secret" not in rel_res and "top" not in rel_res and "?" not in rel_res and "#" not in rel_res
assert rel_res == "failed navigation to /sessions/abc"
print("Relative session URL query and fragment stripped from freeform text")

# 4. Canary with characters (+, /, =, ?) ensuring URL-encoded is genuinely different from raw
synth_key = "synthetic+bs/access=key?12345"
synth_user = "synthetic+bs/user?67890"
encoded_key = quote(synth_key, safe="")
assert encoded_key != synth_key, "Encoded canary must differ from raw"
os.environ["BROWSERSTACK_ACCESS_KEY"] = synth_key
os.environ["BROWSERSTACK_USERNAME"] = synth_user

masked_raw = mask_sensitive(f"error with raw: {synth_key}")
assert synth_key not in masked_raw and "[REDACTED_BS_KEY]" in masked_raw
masked_enc = mask_sensitive(f"error with encoded: {encoded_key}")
assert encoded_key not in masked_enc and "[REDACTED_BS_KEY]" in masked_enc

masked_user = mask_sensitive(f"error with user: {synth_user}")
assert synth_user not in masked_user and "[REDACTED_BS_USER]" in masked_user
print("Synthetic canary with (+, /, =, ?) verifies both raw and encoded credential redaction")

# 5. Path filter accuracy: parsed path == /sessions or prefix /sessions/
assert is_session_path("https://example.com/sessions") is True
assert is_session_path("https://example.com/sessions/7a705e9c") is True
assert is_session_path("https://example.com/sessions/7a705e9c/messages") is True
assert is_session_path("https://example.com/other?query=/sessions") is False, "query substring /sessions must not pass"
assert is_session_path("https://example.com/share/token123") is False, "non-session token path must not pass"
assert is_session_path("https://example.com/setup/status") is False
print("Session path filter strictly matches parsed path, excluding query-only /sessions and /share/token")

# 6. Length truncation in freeform fields
long_msg = "A" * 600
truncated_msg = mask_sensitive(long_msg, max_len=500)
assert len(truncated_msg) == 500 + len("...[truncated]")
assert truncated_msg.endswith("...[truncated]")
print("Freeform field length truncation limit enforced")

# 7. TLS proxy rejection and session-only span logging
with tempfile.TemporaryDirectory(prefix="miniapp-tls-check-") as folder:
    binary = Path(folder) / ("tls-proxy.exe" if os.name == "nt" else "tls-proxy")
    source = Path(__file__).with_name("tls_proxy.go")
    subprocess.run(["go", "build", "-o", str(binary), str(source)], check=True, timeout=120)

    # Standard rejection checks
    for arguments in ([], ["https://127.0.0.1:8080"], ["http://example.com:8080"], ["http://127.0.0.1"]):
        result = subprocess.run([str(binary), *arguments], capture_output=True, text=True, timeout=5)
        assert result.returncode != 0
        assert "expected a loopback HTTP target" in result.stderr
    print("TLS fixture rejects missing, non-HTTP, remote, and portless upstreams")

    # Synthetic loopback HTTP upstream for testing proxy diagnostic spans (does NOT start app)
    class DummyUpstreamHandler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')

        def log_message(self, format, *args):
            pass

    dummy_server = HTTPServer(("127.0.0.1", 0), DummyUpstreamHandler)
    dummy_port = dummy_server.server_port
    server_thread = threading.Thread(target=dummy_server.serve_forever, daemon=True)
    server_thread.start()

    try:
        # A. Test with CMA_UI_DIAGNOSTICS=1
        diag_dir = Path(folder) / "diag"
        diag_dir.mkdir(parents=True, exist_ok=True)
        env_with_diag = {
            **os.environ,
            "CMA_UI_DIAGNOSTICS": "1",
            "CMA_UI_DIAG_DIR": str(diag_dir),
        }
        proxy_proc = subprocess.Popen(
            [str(binary), f"http://127.0.0.1:{dummy_port}"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env_with_diag,
            text=True,
        )
        try:
            line = proxy_proc.stdout.readline().strip()
            assert line.startswith("https://127.0.0.1:"), f"Unexpected proxy output: {line}"
            ctx = ssl._create_unverified_context()

            # 1. Non-session token path request: /share/synthetic-secret-token-abcdef
            req_token = urllib.request.Request(f"{line}/share/synthetic-secret-token-abcdef")
            with urllib.request.urlopen(req_token, context=ctx, timeout=5) as resp:
                assert resp.status == 200

            # 2. Query-only /sessions request: /api/other?filter=/sessions
            req_query = urllib.request.Request(f"{line}/api/other?filter=/sessions")
            with urllib.request.urlopen(req_query, context=ctx, timeout=5) as resp:
                assert resp.status == 200

            # 3. Valid session path request: /sessions/valid-id-789
            req_session = urllib.request.Request(f"{line}/sessions/valid-id-789")
            with urllib.request.urlopen(req_session, context=ctx, timeout=5) as resp:
                assert resp.status == 200

            spans_file = diag_dir / "proxy-spans.jsonl"
            assert spans_file.exists(), "proxy-spans.jsonl was not created under CMA_UI_DIAGNOSTICS=1"
            lines = [json.loads(l) for l in spans_file.read_text(encoding="utf-8").strip().splitlines() if l.strip()]
            raw_log_content = spans_file.read_text(encoding="utf-8")

            # Assert non-session token path and query-only /sessions are NOT in spans
            assert "synthetic-secret-token-abcdef" not in raw_log_content, "Token path must not be recorded"
            assert "/api/other" not in raw_log_content, "Non-session path must not be recorded"

            events = [s["event"] for s in lines]
            assert "http_handler_entry" in events, f"http_handler_entry missing from {events}"
            assert "proxy_upstream_start" in events, f"proxy_upstream_start missing from {events}"
            assert "proxy_upstream_response" in events, f"proxy_upstream_response missing from {events}"

            for span in lines:
                assert span.get("safe_path") == "/sessions/valid-id-789"
                assert "http_handler_entry observes HTTP request dispatch, not TCP/TLS transport accept" in span.get("correlation", "")
                assert "headers" not in span
                assert "body" not in span
                assert "?" not in span.get("safe_path", "")
            print("TLS proxy records safe spans only for matching session paths with explicit handler notice")
        finally:
            proxy_proc.terminate()
            try:
                proxy_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proxy_proc.kill()
                proxy_proc.wait(timeout=5)

        # B. Test with CMA_UI_DIAGNOSTICS unset
        nodiag_dir = Path(folder) / "nodiag"
        nodiag_dir.mkdir(parents=True, exist_ok=True)
        env_no_diag = {k: v for k, v in os.environ.items() if k != "CMA_UI_DIAGNOSTICS"}
        env_no_diag["CMA_UI_DIAG_DIR"] = str(nodiag_dir)
        proxy_proc_nodiag = subprocess.Popen(
            [str(binary), f"http://127.0.0.1:{dummy_port}"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env_no_diag,
            text=True,
        )
        try:
            line = proxy_proc_nodiag.stdout.readline().strip()
            assert line.startswith("https://127.0.0.1:"), f"Unexpected proxy output: {line}"
            ctx = ssl._create_unverified_context()
            req = urllib.request.Request(f"{line}/sessions/normal")
            with urllib.request.urlopen(req, context=ctx, timeout=5) as resp:
                assert resp.status == 200

            assert not (nodiag_dir / "proxy-spans.jsonl").exists(), "proxy-spans.jsonl should not exist when CMA_UI_DIAGNOSTICS is not 1"
            print("TLS proxy suppresses diagnostic logging when CMA_UI_DIAGNOSTICS is not set")
        finally:
            proxy_proc_nodiag.terminate()
            try:
                proxy_proc_nodiag.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proxy_proc_nodiag.kill()
                proxy_proc_nodiag.wait(timeout=5)

    finally:
        dummy_server.shutdown()
        dummy_server.server_close()

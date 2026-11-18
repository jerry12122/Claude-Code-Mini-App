"""Run directly: validate fixture boundaries and credential redaction."""
import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace

from conftest import pytest_runtest_makereport

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

with tempfile.TemporaryDirectory(prefix="miniapp-tls-check-") as folder:
    binary = Path(folder) / ("tls-proxy.exe" if os.name == "nt" else "tls-proxy")
    source = Path(__file__).with_name("tls_proxy.go")
    subprocess.run(["go", "build", "-o", str(binary), str(source)], check=True, timeout=120)
    for arguments in ([], ["https://127.0.0.1:8080"], ["http://example.com:8080"], ["http://127.0.0.1"]):
        result = subprocess.run([str(binary), *arguments], capture_output=True, text=True, timeout=5)
        assert result.returncode != 0
        assert "expected a loopback HTTP target" in result.stderr
print("TLS fixture rejects missing, non-HTTP, remote, and portless upstreams")

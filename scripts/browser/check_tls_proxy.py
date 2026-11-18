"""Run directly: ensure the test TLS proxy rejects non-loopback upstreams."""
import os
from pathlib import Path
import subprocess
import tempfile

with tempfile.TemporaryDirectory(prefix="miniapp-tls-check-") as folder:
    binary = Path(folder) / ("tls-proxy.exe" if os.name == "nt" else "tls-proxy")
    source = Path(__file__).with_name("tls_proxy.go")
    subprocess.run(["go", "build", "-o", str(binary), str(source)], check=True, timeout=120)
    for arguments in ([], ["https://127.0.0.1:8080"], ["http://example.com:8080"], ["http://127.0.0.1"]):
        result = subprocess.run([str(binary), *arguments], capture_output=True, text=True, timeout=5)
        assert result.returncode != 0
        assert "expected a loopback HTTP target" in result.stderr
print("TLS fixture rejects missing, non-HTTP, remote, and portless upstreams")

#!/usr/bin/env python3
"""Generate the desktop icon and Windows version resource from root VERSION."""
import pathlib
import re
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[2]
version = (root / "VERSION").read_text(encoding="utf-8").strip()
if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
    raise SystemExit("VERSION must be X.Y.Z")
parts = [int(part) for part in version.split(".")] + [0]
if any(part > 65535 for part in parts):
    raise SystemExit("Windows version components must fit in 16 bits")
desktop = root / "cmd/desktop"
source = f'#define APP_VERSION {",".join(map(str, parts))}\n'
source += f'#define APP_VERSION_STRING "{version}.0\\0"\n'
source += (desktop / "desktop.rc").read_text(encoding="utf-8")
with tempfile.TemporaryDirectory(prefix="miniapp-resource-") as work:
    rc = pathlib.Path(work) / "desktop.rc"
    rc.write_text(source, encoding="utf-8")
    subprocess.run(
        ["windres", "--input", str(rc), "--output=desktop_windows_amd64.syso",
         "--output-format=coff", "--target=pe-x86-64"],
        cwd=desktop, check=True,
    )

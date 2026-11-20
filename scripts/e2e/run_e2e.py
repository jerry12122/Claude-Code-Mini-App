#!/usr/bin/env python3
"""Windows 真程序端到端驗證（desktop-self-update 5.1）。

前置：先執行 `python scripts/e2e/build_fixtures.py`（產生 1.0.0／1.0.1 fixture 與 manifest）。
用法：python scripts/e2e/run_e2e.py [--work DIR] [--only 名稱,...] [--keep]

所有資料與程序都在 --work（預設 %TEMP%\\cma-e2e）下；不碰 port 8080、不碰使用者的 config／DB／workspace，
不建立 Release、不 push tag、不寫入 repo。只結束本腳本啟動、路徑吻合的 PID。
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import pathlib
import random
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import traceback

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from desktop_driver import (  # noqa: E402
    CDP, CLICK_JS, INSTALL_LISTENER_JS, BUTTON_EXISTS_JS, cdp_page_ws, exe_path_of, http, js_str,
    kill_pid_if_path, listening_pids, login, multipart_upload, pick_port, start_detached, wait_until,
    auth, pids_with_path,
)
from fixture_server import FixtureServer  # noqa: E402

PASSWORD = "e2e-pass-7f3a"
APP_PORTS = (18100, 18499)
CDP_PORTS = (18500, 18899)
OPEN_JS = """(function () {
  if (!window.__desktopUpdater) return false;
  window.dispatchEvent(new CustomEvent("desktop-update-open"));
  return true;
})()"""
# 與 Go 端 OpenScript 相同；等同系統匣「檢查更新」先開設定的動作。
CONFIRM_LABEL = "確定重新啟動並更新"
RESTART_LABEL = "重新啟動並更新"

# 點擊之後的新狀態判斷（搭配 App.wait_new，只看點擊後新增的事件）
CHECK_DONE = lambda new: any(s.get("state") == "checking" for s in new) and new[-1].get("state") in (  # noqa: E731
    "available", "up-to-date", "error")
DOWNLOAD_DONE = lambda new: new[-1].get("state") in ("ready", "error")  # noqa: E731


def restart_done(code: str):
    return lambda new: new[-1].get("errorCode") == code  # noqa: E731


def sha256(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Ctx:
    def __init__(self, work: pathlib.Path, manifest: dict):
        self.work = work
        self.manifest = manifest
        self.runs = work / "runs"
        self.results: list[dict] = []
        self.procs: list[dict] = []  # 本腳本啟動的所有程序（PID 紀錄）
        self.used_ports: set[int] = set()

    def record_proc(self, scenario: str, role: str, pid: int, path: str) -> None:
        self.procs.append({"scenario": scenario, "role": role, "pid": pid, "path": path, "cleanup": None})

    def ports(self) -> tuple[int, int]:
        return pick_port(*APP_PORTS, self.used_ports), pick_port(*CDP_PORTS, self.used_ports)


class Check:
    """單一場景的檢查紀錄。"""

    def __init__(self):
        self.items: list[dict] = []
        self.limits: list[str] = []

    def ok(self, name: str, cond: bool, detail: str = "") -> bool:
        self.items.append({"name": name, "ok": bool(cond), "detail": detail})
        return bool(cond)


def prepare_install(ctx: Ctx, name: str, ver: str = "1.0.0", port: int = 0) -> pathlib.Path:
    run = ctx.runs / name
    inst = run / "install"
    if run.exists():
        shutil.rmtree(run)
    (inst / "logs").mkdir(parents=True)
    (inst / "workspace").mkdir()
    (inst / "workdir").mkdir()
    dist = ctx.manifest["versions"][ver]
    shutil.copy2(dist["desktop_exe"], inst / "claude-miniapp-desktop.exe")
    shutil.copy2(dist["server_exe"], inst / "claude-miniapp-server.exe")
    (inst / "config.yaml").write_text(
        f'''bot_token: "123456:e2e-placeholder"
whitelist_tg_ids:
  - 1
web:
  password: "{PASSWORD}"
  allowed_cidrs:
    - "127.0.0.0/8"
  trusted_proxies:
    - "127.0.0.1/32"
    - "::1/128"
  session_ttl: "1h"
no_auth: false
notify:
  on_error: false
  on_cancel: false
  on_shell_error: false
server:
  port: {port}
db:
  path: "./claude-miniapp.db"
''', encoding="utf-8", newline="\n")
    (inst / "logs" / "server.log").write_text(f"E2E-LOG-MARKER-{name}\n", encoding="utf-8", newline="\n")
    return inst


class App:
    """以真程序啟動的桌面版；所有操作都以自己啟動的 PID 為準。"""

    def __init__(self, ctx: Ctx, scenario: str, inst: pathlib.Path, port: int, cdp: int,
                 fixture: FixtureServer, extra_env: dict | None = None):
        self.ctx = ctx
        self.scenario = scenario
        self.inst = inst
        self.exe = inst / "claude-miniapp-desktop.exe"
        self.port = port
        self.cdp_port = cdp
        self.fixture = fixture
        self.proc = None
        self.pid = None
        self.cdp: CDP | None = None
        self.env = {
            "CMA_E2E_API_BASE": fixture.base,
            "CMA_E2E_UNIQUE_ID": f"cma-e2e-{scenario}-{port}",
            "CMA_E2E_WEBVIEW_DATA": str(inst.parent / "webview"),
            "CMA_E2E_CDP_PORT": str(cdp),
        }
        self.env.update({**(extra_env or {}), "MINIAPP_TEST_LOOPBACK": "1"})

    def start(self, timeout: float = 150) -> int:
        self.proc = start_detached(self.exe, self.inst, self.env, self.inst.parent / "desktop.log")
        self.pid = self.proc.pid
        exe_str = str(self.exe)
        self.ctx.record_proc(self.scenario, "desktop", self.pid, exe_str)
        wait_until(lambda: self.pid in listening_pids(self.port), timeout, 1.0, what=f"port {self.port} 由 pid {self.pid} 監聽")
        return self.pid

    def config(self) -> dict:
        """/config 需登入；每次以測試密碼登入取得 token（不保存）。"""
        token = login(self.port, PASSWORD)
        code, body = http("GET", f"http://127.0.0.1:{self.port}/config", headers=auth(token), timeout=10)
        return body if isinstance(body, dict) else {"_raw": body, "_code": code}

    def connect(self) -> None:
        url = f"http://127.0.0.1:{self.port}/"
        ws_url = cdp_page_ws(self.cdp_port, url, 120)
        self.cdp = CDP(ws_url)
        # 不做任何 runtime-ready 補送：bridge 與狀態事件必須靠產品的 wails-runtime-ready.js 自然出現。
        try:
            wait_until(lambda: self.cdp.eval("!!window.__desktopUpdater"), 60, 1.0, what="window.__desktopUpdater")
        except TimeoutError:
            diag = self.cdp.eval(
                "JSON.stringify({href: location.href, origin: location.origin, top: window.top === window,"
                " wv: !!(window.chrome && window.chrome.webview), ready: document.readyState,"
                " text: (document.body ? document.body.innerText : '').slice(0, 120)})")
            raise TimeoutError(f"bridge 未就緒；頁面診斷：{diag}")
        self.cdp.eval(INSTALL_LISTENER_JS)

    def open_settings(self) -> None:
        wait_until(lambda: self.cdp.eval(OPEN_JS) is True, 30, 1.0, what="開啟設定（desktop-update-open）")

    def snapshot(self) -> dict:
        """要求一次狀態快照，回傳最新 desktop-update-state。"""
        n0 = self.cdp.eval("(window.__e2eStates || []).length")
        self.cdp.eval("window.__desktopUpdater.request('status')")
        wait_until(lambda: (self.cdp.eval("(window.__e2eStates || []).length") or 0) > (n0 or 0), 20, 0.3,
                   what="status 事件")
        return self.cdp.eval("window.__e2eStates[window.__e2eStates.length - 1]") or {}

    def states(self) -> list[dict]:
        return self.cdp.eval("window.__e2eStates || []") or []

    def click(self, label: str, timeout: float = 30) -> None:
        wait_until(lambda: self.cdp.eval(CLICK_JS % js_str(label)) is True, timeout, 1.0, what=f"按鈕「{label}」")

    def button_visible(self, label: str) -> bool:
        return bool(self.cdp.eval(BUTTON_EXISTS_JS % js_str(label)))

    def wait_status(self, pred, timeout: float, what: str) -> dict:
        last = {}

        def cond():
            nonlocal last
            last = self.snapshot()
            return pred(last)

        try:
            wait_until(cond, timeout, 2.0, what=what)
        except TimeoutError:
            raise TimeoutError(f"{what} 逾時；最後狀態：{json.dumps(last, ensure_ascii=False)}")
        return last

    def mark(self) -> int:
        """點擊前記下狀態事件數；之後只看這個位置之後新增的事件，避免讀到舊狀態。"""
        return len(self.states())

    def click_check(self) -> None:
        """錯誤且沒有可用版本時，按鈕文字為「重試」（檢查）；其餘為「檢查更新」。"""
        def enabled_label():
            if self.button_visible("檢查更新"):
                return "檢查更新"
            if self.button_visible("重試"):
                return "重試"
            return None

        label = wait_until(enabled_label, 60, 1.0, what="可用的檢查按鈕（檢查更新／重試）")
        self.click(label)

    def wait_new(self, n0: int, pred, timeout: float, what: str) -> dict:
        new: list[dict] = []
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.snapshot()  # 觸發一次最新快照（會新增事件）
            new = self.states()[n0:]
            if new and pred(new):
                return new[-1]
            time.sleep(1.5)
        raise TimeoutError(f"{what} 逾時；點擊後的新狀態：{json.dumps(new, ensure_ascii=False)[-700:]}")

    def close_cdp(self) -> None:
        if self.cdp:
            self.cdp.close()
            self.cdp = None

    def stop(self) -> str:
        """只結束本 App 啟動的 PID（路徑需吻合）。"""
        self.close_cdp()
        if self.pid is None:
            return "未啟動"
        desc = kill_pid_if_path(self.pid, str(self.exe))
        return desc


def db_rows(inst: pathlib.Path) -> dict:
    con = sqlite3.connect(str(inst / "claude-miniapp.db"), timeout=10)
    try:
        sessions = con.execute("SELECT id, name, work_dir FROM sessions ORDER BY id").fetchall()
        attachments = con.execute("SELECT id, session_id, original_name, size FROM attachments ORDER BY id").fetchall()
        queued = con.execute("SELECT COUNT(*) FROM queued_messages").fetchone()[0]
    finally:
        con.close()
    return {"sessions": sessions, "attachments": attachments, "queued": queued}


def seed_data(app: App, inst: pathlib.Path) -> dict:
    """透過真 REST 建立 session 與附件，回傳需要在更新後比對的資料。"""
    token = login(app.port, PASSWORD)
    code, sess = http("POST", f"http://127.0.0.1:{app.port}/sessions",
                      {"name": "e2e-session", "work_dir": str(inst / "workdir"), "agent_type": "claude"},
                      auth(token))
    if code not in (200, 201) or not isinstance(sess, dict) or "id" not in sess:
        raise RuntimeError(f"建立 session 失敗 code={code} body={sess}")
    rnd = random.Random(7).randbytes(4096)
    content = b"\x89PNG\r\n\x1a\n" + rnd
    code, up = multipart_upload(app.port, token, sess["id"], "e2e-shot.png", content)
    if code not in (200, 201):
        raise RuntimeError(f"上傳附件失敗 code={code} body={up}")
    up_path = inst / "workspace"
    stored = [p for p in up_path.rglob("*") if p.is_file()]
    return {
        "session_id": sess["id"], "token": token,
        "attachment_bytes_sha": hashlib.sha256(content).hexdigest(),
        "stored_files": [str(p.relative_to(inst)) for p in stored],
        "stored_sha": {str(p.relative_to(inst)): sha256(p) for p in stored},
        "db_before": db_rows(inst),
    }


def snapshot_dir(inst: pathlib.Path) -> dict:
    """配置、server 檔、log 等在更新前後必須不變（或被保留）的檔案雜湊。"""
    out = {}
    for rel in ["config.yaml", "claude-miniapp-server.exe"]:
        p = inst / rel
        out[rel] = sha256(p) if p.exists() else None
    return out


def ensure_fixture_ready(ctx: Ctx, fx: FixtureServer) -> None:
    code, _ = http("GET", f"{fx.base}/__health", timeout=5)
    if code != 200:
        raise RuntimeError("fixture server 無回應")


# ---------------- scenarios ----------------

def scenario_happy(ctx: Ctx) -> dict:
    name = "happy-update"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        app = App(ctx, name, inst, port, cdp, fx)
        old_pid = app.start()
        cfg0 = app.config()
        ck.ok("起始版本為 1.0.0", cfg0.get("version") == "1.0.0", json.dumps(cfg0, ensure_ascii=False))
        ck.ok("起始 exe 為 1.0.0 建置", sha256(app.exe) == v100["desktop_sha256"], "")
        seed = seed_data(app, inst)
        before_files = snapshot_dir(inst)
        ck.ok("資料已寫入（session 與附件）", len(seed["db_before"]["sessions"]) == 1
              and len(seed["db_before"]["attachments"]) == 1, json.dumps(seed["db_before"], ensure_ascii=False))
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        app.click("檢查更新")
        st = app.wait_status(lambda s: s.get("state") in ("available", "up-to-date", "error"), 120, "檢查完成")
        ck.ok("檢查選到 1.0.1 且未自動下載", st.get("state") == "available" and st.get("availableVersion") == "1.0.1",
              json.dumps(st, ensure_ascii=False))
        ck.ok("檢查階段未下載 EXE（只允許 checksums.txt 的 sidecar 請求）",
              not any(p.startswith("/dl/claude-miniapp") for p in fx.requests), str(fx.requests))
        app.click("下載更新")
        st = app.wait_status(lambda s: s.get("state") in ("ready", "error"), 240, "下載與驗證完成")
        ck.ok("下載並驗證後進入 ready", st.get("state") == "ready", json.dumps(st, ensure_ascii=False))
        ck.ok("ready 階段原 exe 未被改寫", sha256(app.exe) == v100["desktop_sha256"], "")
        app.click(RESTART_LABEL)
        app.click(CONFIRM_LABEL)
        wait_until(lambda: exe_path_of(old_pid) == "", 150, 1.0, what="舊程序結束")
        new_pids = wait_until(
            lambda: [p for p in listening_pids(port) if p != old_pid and exe_path_of(p).lower() == str(app.exe).lower()],
            180, 1.0, what="新程序監聽原 port")
        new_pid = new_pids[0]
        ctx.record_proc(name, "desktop-updated", new_pid, str(app.exe))
        app.pid = new_pid
        app.close_cdp()
        cfg1 = app.config()
        ck.ok("新 PID 與舊 PID 不同", new_pid != old_pid, f"old={old_pid} new={new_pid}")
        ck.ok("原 port 由新程序監聽", new_pid in listening_pids(port), f"port={port}")
        ck.ok("/config 版本為 1.0.1", cfg1.get("version") == "1.0.1", json.dumps(cfg1, ensure_ascii=False))
        ck.ok("/config 的 revision 與 1.0.1 建置一致",
              str(cfg1.get("sha") or cfg1.get("revision") or "").startswith(v101["revision"][:12]),
              f"config={cfg1.get('sha') or cfg1.get('revision')} expected={v101['revision'][:12]}")
        ck.ok("安裝 exe 的 SHA256 等於 1.0.1 建置", sha256(app.exe) == v101["desktop_sha256"], "")
        code, index = http("GET", f"http://127.0.0.1:{port}/", raw=True)
        body = index.decode("utf-8", "replace") if isinstance(index, (bytes, bytearray)) else str(index)
        ck.ok("新程序回傳嵌入的 1.0.1 前端", "cma-e2e-frontend:1.0.1" in body and "cma-e2e-frontend:1.0.0" not in body,
              f"http={code}")
        ck.ok("安裝目錄無磁碟前端（internal/static/index.html 不存在）",
              not (inst / "internal" / "static" / "index.html").exists(), "")
        after_files = snapshot_dir(inst)
        ck.ok("config.yaml 內容不變", before_files["config.yaml"] == after_files["config.yaml"], "")
        ck.ok("獨立 server 檔案不變", before_files["claude-miniapp-server.exe"] == after_files["claude-miniapp-server.exe"],
              "")
        ck.ok("log 保留既有內容", "E2E-LOG-MARKER-" + name in (inst / "logs" / "server.log").read_text(encoding="utf-8"),
              "")
        db_after = db_rows(inst)
        ck.ok("DB 的 session／附件紀錄保留", db_after["sessions"] == seed["db_before"]["sessions"]
              and db_after["attachments"] == seed["db_before"]["attachments"],
              json.dumps(db_after, ensure_ascii=False))
        attach_ok = all(sha256(inst / rel) == h for rel, h in seed["stored_sha"].items())
        ck.ok("附件檔案位元組保留", attach_ok and bool(seed["stored_sha"]), str(seed["stored_files"]))
        ck.ok("更新後 .bak 殘留狀態（記錄）", True,
              f"bak_exists={(inst / 'claude-miniapp-desktop.exe.bak').exists()}")
        ck.ok("fixture 只收到一次資產下載",
              sum(1 for p in fx.requests if p.startswith("/dl/claude-miniapp")) == 1, "")
        ck.limits.append("UI 驅動透過 WebView2 CDP 點擊實際按鈕；未觸及系統匣圖示本身（以同一 OpenScript 開啟設定）")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if app is not None:
            app.stop()  # 只結束 pid（路徑吻合才結束）
        fx.stop()
    return finish(name, "happy", ck)


COMMON_LIMITS = [
    "設定頁以 CDP 派送 desktop-update-open（與系統匣 OpenScript 相同事件）開啟，未點擊系統匣圖示本身。",
]


def finish(name: str, key: str, ck: Check) -> dict:
    verdict = "PASS" if all(i["ok"] for i in ck.items) else "FAIL"
    return {"scenario": name, "key": key, "verdict": verdict, "items": ck.items,
            "limits": COMMON_LIMITS + ck.limits}


def scenario_fault_check_download(ctx: Ctx, name: str, faults: dict, expect_codes: set[str],
                                  stage: str, stop_fixture: bool = False, expect_uptodate: bool = False) -> dict:
    """注入 fixture 故障，UI 走 check →（download）；檢查錯誤碼、exe 不變、ready 不出現。
    expect_uptodate=True 用於「尚無公開正式版（404）」：預期 state=up-to-date 且無錯誤碼（已知行為，非失敗）。"""
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    fx.set_fault(**faults)
    app = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        app = App(ctx, name, inst, port, cdp, fx)
        old_pid = app.start()
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        if stop_fixture:
            fx.stop()
        n0 = app.mark()
        app.click_check()
        st = app.wait_new(n0, CHECK_DONE, 120, "檢查完成")
        if stage == "download" and st.get("state") == "available":
            n0 = app.mark()
            app.click("下載更新")
            st = app.wait_new(n0, DOWNLOAD_DONE, 240, "下載與驗證完成")
        if expect_uptodate:
            ck.ok("狀態為 up-to-date 且無錯誤碼（尚無公開正式版＝已知行為，非錯誤）",
                  st.get("state") == "up-to-date" and st.get("errorCode", "") == "",
                  json.dumps(st, ensure_ascii=False))
        else:
            ck.ok(f"錯誤碼 ∈ {sorted(expect_codes)}（實際 {st.get('errorCode')!r}）",
                  st.get("state") == "error" and st.get("errorCode") in expect_codes, json.dumps(st, ensure_ascii=False))
        ck.ok("未進入 ready", st.get("state") != "ready", "")
        ck.ok("安裝 exe 未修改", sha256(app.exe) == v100["desktop_sha256"], "")
        ck.ok("原程序仍在運行（未重啟）", exe_path_of(old_pid).lower() == str(app.exe).lower(), f"pid={old_pid}")
        ck.limits.append(f"實際錯誤碼：{st.get('errorCode')!r}")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


def scenario_not_writable(ctx: Ctx) -> dict:
    """安裝目錄 ACL 拒絕目前使用者建立檔案；下載前應回報 install_not_writable 且無 /dl 請求。"""
    name = "not-writable"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    user = os.environ.get("USERNAME", "")
    inst = prepare_install(ctx, name, "1.0.0", port)
    acl_applied = False
    try:
        app = App(ctx, name, inst, port, cdp, fx)
        old_pid = app.start()
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        n0 = app.mark()
        app.click_check()
        app.wait_new(n0, CHECK_DONE, 120, "檢查完成")
        res = subprocess.run(["icacls", str(inst), "/deny", f"{user}:(AD,WD)"], capture_output=True, text=True)
        acl_applied = res.returncode == 0
        ck.ok("已套用目錄 ACL 拒絕（icacls）", acl_applied, res.stdout.strip() + res.stderr.strip())
        if acl_applied:
            n0 = app.mark()
            app.click("下載更新")
            st = app.wait_new(n0, DOWNLOAD_DONE, 120, "下載前的可寫性檢查")
            ck.ok("錯誤碼為 install_not_writable", st.get("errorCode") == "install_not_writable",
                  json.dumps(st, ensure_ascii=False))
            ck.ok("未開始下載 EXE（fixture 無 EXE 請求）",
                  not any(p.startswith("/dl/claude-miniapp") for p in fx.requests), str(fx.requests))
            ck.ok("未進入 ready", st.get("state") != "ready", "")
            ck.ok("安裝 exe 未修改", sha256(app.exe) == v100["desktop_sha256"], "")
            ck.ok("原程序仍在運行", exe_path_of(old_pid).lower() == str(app.exe).lower(), "")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if acl_applied:
            res = subprocess.run(["icacls", str(inst), "/remove:d", user], capture_output=True, text=True)
            ck.ok("已還原目錄 ACL（移除拒絕項）", res.returncode == 0, res.stdout.strip())
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


def scenario_busy(ctx: Ctx) -> dict:
    """注入一筆 queued 訊息（測試 DB）；重啟應被 update_busy／queued 拒絕，不啟動 helper，queue 保留。"""
    name = "busy-queued"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        app = App(ctx, name, inst, port, cdp, fx)
        old_pid = app.start()
        seed = seed_data(app, inst)
        con = sqlite3.connect(str(inst / "claude-miniapp.db"), timeout=10)
        con.execute("INSERT INTO queued_messages (session_id, content, created_at) VALUES (?, ?, ?)",
                    (seed["session_id"], "e2e queued", int(time.time())))
        con.commit()
        con.close()
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        n0 = app.mark()
        app.click_check()
        app.wait_new(n0, CHECK_DONE, 120, "檢查完成")
        n0 = app.mark()
        app.click("下載更新")
        app.wait_new(n0, DOWNLOAD_DONE, 240, "下載與驗證完成")
        n0 = app.mark()
        app.click(RESTART_LABEL)
        app.click(CONFIRM_LABEL)
        st = app.wait_new(n0, restart_done("update_busy"), 60, "重啟被拒")
        ck.ok("錯誤碼 update_busy", st.get("errorCode") == "update_busy", json.dumps(st, ensure_ascii=False))
        ck.ok("busyReason 為 queued", st.get("busyReason") == "queued", json.dumps(st, ensure_ascii=False))
        ck.ok("仍保有 ready 更新（規格：拒絕後維持 ready）", st.get("state") == "ready"
              and st.get("availableVersion") == "1.0.1", json.dumps(st, ensure_ascii=False))
        time.sleep(3)
        ck.ok("未啟動 helper（僅一個同路徑程序＝原程序）", pids_with_path(str(app.exe)) == [old_pid],
              str(pids_with_path(str(app.exe))))
        ck.ok("原程序仍在運行", exe_path_of(old_pid).lower() == str(app.exe).lower(), "")
        ck.ok("安裝 exe 未修改", sha256(app.exe) == v100["desktop_sha256"], "")
        ck.ok("queued 訊息保留", db_rows(inst)["queued"] == 1, "")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


def scenario_helper_fail(ctx: Ctx) -> dict:
    """manager 層注入 helper 啟動失敗：應 helper_failed、保留 ready、釋放閘門（第二次重啟不應為 update_in_progress）。"""
    name = "helper-failed"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        app = App(ctx, name, inst, port, cdp, fx, {"CMA_E2E_FAIL_RESTART": "1"})
        old_pid = app.start()
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        n0 = app.mark()
        app.click_check()
        app.wait_new(n0, CHECK_DONE, 120, "檢查完成")
        n0 = app.mark()
        app.click("下載更新")
        app.wait_new(n0, DOWNLOAD_DONE, 240, "下載與驗證完成")
        n0 = app.mark()
        app.click(RESTART_LABEL)
        app.click(CONFIRM_LABEL)
        st = app.wait_new(n0, restart_done("helper_failed"), 60, "helper 失敗")
        ck.ok("錯誤碼 helper_failed", st.get("errorCode") == "helper_failed", json.dumps(st, ensure_ascii=False))
        ck.ok("ready 更新保留（規格：失敗後維持 ready 可用）",
              st.get("state") == "ready" and st.get("availableVersion") == "1.0.1", json.dumps(st, ensure_ascii=False))
        ck.ok("原程序仍在運行（未退出）", exe_path_of(old_pid).lower() == str(app.exe).lower(), f"pid={old_pid}")
        ck.ok("未啟動新 helper（僅原程序）", pids_with_path(str(app.exe)) == [old_pid], "")
        ck.ok("安裝 exe 未修改", sha256(app.exe) == v100["desktop_sha256"], "")
        # 閘門釋放驗證：再次要求重啟，應是 helper_failed，而不是 update_in_progress／busy
        n0 = app.mark()
        app.click(RESTART_LABEL)
        app.click(CONFIRM_LABEL)
        st2 = app.wait_new(n0, restart_done("helper_failed"), 60, "第二次重啟")
        ck.ok("第二次重啟仍為 helper_failed（閘門已釋放）", st2.get("errorCode") == "helper_failed",
              json.dumps(st2, ensure_ascii=False))
        ck.limits.append("注入點在 manager 層（Restart 前），未真的讓 Wails helper 程序建立失敗；"
                         "真實 helper 啟動失敗路徑 未驗證")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


def scenario_watchdog(ctx: Ctx) -> dict:
    """OnShutdown 卡住：ShutdownDeadline 後 watchdog 強制結束（非正常關閉 DB），新程序應能開啟 DB 並保有資料。"""
    name = "shutdown-hang-watchdog"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        app = App(ctx, name, inst, port, cdp, fx, {"CMA_E2E_HANG_SHUTDOWN": "1"})
        old_pid = app.start()
        seed = seed_data(app, inst)
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        app.click("檢查更新")
        app.wait_status(lambda s: s.get("state") in ("available", "error"), 120, "檢查完成")
        app.click("下載更新")
        app.wait_status(lambda s: s.get("state") in ("ready", "error"), 240, "下載與驗證完成")
        t0 = time.time()
        app.click(RESTART_LABEL)
        app.click(CONFIRM_LABEL)
        wait_until(lambda: exe_path_of(old_pid) == "", 120, 1.0, what="舊程序結束（watchdog）")
        elapsed = time.time() - t0
        new_pid = wait_until(
            lambda: [p for p in listening_pids(port) if p != old_pid and exe_path_of(p).lower() == str(app.exe).lower()],
            180, 1.0, what="新程序")[0]
        ctx.record_proc(name, "desktop-updated", new_pid, str(app.exe))
        app.pid = new_pid
        ck.ok("舊程序於 ShutdownDeadline 附近被強制結束（秒）", 15 <= elapsed <= 90, f"elapsed={elapsed:.1f}s")
        ck.ok("新程序版本 1.0.1", (app.config() or {}).get("version") == "1.0.1", "")
        db_after = db_rows(inst)
        ck.ok("強制結束後 DB 仍可開啟且 session／附件紀錄保留",
              db_after["sessions"] == seed["db_before"]["sessions"]
              and db_after["attachments"] == seed["db_before"]["attachments"], json.dumps(db_after, ensure_ascii=False))
        ck.limits.append("此為 watchdog（os.Exit）路徑；非 TerminateProcess 造成的崩潰，WAL 復原以既有 SQLite 行為為準")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


def scenario_replacement_fail(ctx: Ctx) -> dict:
    """雙重故障：以 FileShare.Read 鎖住安裝 exe 約 45 秒，helper 的替換與 .bak 還原都被同一把鎖擋住。
    預期：原 exe SHA 不變、.bak 與 helper 日誌保留、配置／DB 不變；不要求 relaunch（已知行為）。"""
    name = "replacement-fail"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    holder = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        app = App(ctx, name, inst, port, cdp, fx)
        old_pid = app.start()
        app.connect()
        app.open_settings()
        app.wait_status(lambda s: s.get("state") in ("idle", "up-to-date", "available", "error"), 60, "初始狀態")
        n0 = app.mark()
        app.click_check()
        app.wait_new(n0, CHECK_DONE, 120, "檢查完成")
        n0 = app.mark()
        app.click("下載更新")
        app.wait_new(n0, DOWNLOAD_DONE, 240, "下載與驗證完成")
        hold_ps = ctx.work / "hold-target.ps1"
        hold_ps.write_text(
            "param([string]$p,[int]$s)\n"
            "$fs=[IO.File]::Open($p,[IO.FileMode]::Open,[IO.FileAccess]::Read,[IO.FileShare]::Read)\n"
            "Start-Sleep -Seconds $s\n$fs.Close()\n", encoding="utf-8")
        holder = subprocess.Popen(
            ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(hold_ps), str(app.exe), "45"],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            creationflags=0x08000000)
        ctx.record_proc(name, "file-holder-powershell", holder.pid, "powershell")
        time.sleep(3)
        cfg_before, db_before = snapshot_dir(inst), db_rows(inst)
        n0 = app.mark()
        app.click(RESTART_LABEL)
        app.click(CONFIRM_LABEL)
        wait_until(lambda: exe_path_of(old_pid) == "", 120, 1.0, what="舊程序結束")
        helper_log = pathlib.Path(tempfile.gettempdir()) / f"wails-update-{old_pid}.log"
        try:
            wait_until(lambda: helper_log.exists() and "exhausted" in helper_log.read_text(encoding="utf-8", errors="replace"),
                       90, 2, what="helper 記錄 swap 失敗")
        except TimeoutError:
            pass
        helper_text = helper_log.read_text(encoding="utf-8", errors="replace") if helper_log.exists() else ""
        ck.ok("helper 記錄 swap 全數失敗（目標被鎖）", "all swap attempts exhausted" in helper_text, helper_text[-400:])
        ck.ok("安裝 exe 的 SHA256 仍為 1.0.0（原檔保留）", sha256(app.exe) == v100["desktop_sha256"], "")
        ck.ok("配置與 DB 紀錄未改", snapshot_dir(inst) == cfg_before and db_rows(inst) == db_before, "")
        try:
            relaunched = wait_until(
                lambda: [p for p in listening_pids(port) if exe_path_of(p).lower() == str(app.exe).lower()] or None,
                75, 2, what="helper 重新啟動原程序")
        except TimeoutError:
            relaunched = []
        bak = app.exe.with_name(app.exe.name + ".bak")
        ck.ok(".bak 保留且內容為 1.0.0（雙重故障下的還原來源）",
              bak.exists() and sha256(bak) == v100["desktop_sha256"], str(bak))
        ck.ok("helper 日誌記錄 restore failed（雙重故障證據）", "restore failed" in helper_text, "")
        # 雙重故障下 helper 不會自動 relaunch（design.md 已知限制），因此不把 relaunch 當作通過條件；
        # 若真的出現新程序，仍記錄 PID 以便清理，並驗證其版本為 1.0.0。
        if relaunched:
            ctx.record_proc(name, "relaunched-original", relaunched[0], str(app.exe))
            app.pid = relaunched[0]
            cfg = app.config()
            ck.ok("（觀察到 relaunch）重新啟動的程序仍為 1.0.0", cfg.get("version") == "1.0.0",
                  json.dumps(cfg, ensure_ascii=False))
        ck.limits.append(f"觀察：relaunch 偵測＝{'有' if relaunched else '無'}（pids={relaunched}；雙重故障預期無，不列為通過條件）")
        ck.limits.append("雙重故障：以 FileShare.Read 鎖定目標 exe；rename-aside 全數失敗，還原 .bak 也被同一把鎖擋住"
                         "（restore failed），目標 exe 保持 1.0.0 且不 relaunch；.bak 與 helper 日誌保留於 evidence")
        ck.evidence = helper_text
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if holder is not None:
            try:
                holder.wait(timeout=60)
            except subprocess.TimeoutExpired:
                holder.kill()
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


# 子項表：(名稱, fixture 故障, 預期錯誤碼, 階段, 額外參數)
MATRIX_CASES = [
    ("checksum-missing", {"ck": "missing"}, {"checksum_missing"}, "download", {}),
    ("checksum-mismatch", {"ck": "mismatch"}, {"checksum_mismatch"}, "download", {}),
    ("checksum-badformat", {"ck": "badformat"}, {"checksum_missing", "checksum_mismatch"}, "download", {}),
    ("api-connection-drop", {"api": "drop"}, {"check_failed", "update_check_failed"}, "check", {}),
    # 404 = 尚無公開正式版：預期 up-to-date 且無錯誤碼（見 design.md 已知限制）
    ("api-404", {"api": "404"}, set(), "check", {"expect_uptodate": True}),
    ("api-ratelimit", {"api": "ratelimit"}, {"check_failed", "update_check_failed"}, "check", {}),
    ("api-500", {"api": "500"}, {"check_failed", "update_check_failed"}, "check", {}),
    ("download-drop", {"dl": "drop"}, {"download_failed"}, "download", {}),
    ("download-abort-midway", {"dl": "abort"}, {"download_failed"}, "download", {}),
    ("fixture-connection-refused", {}, {"check_failed", "update_check_failed"}, "check",
     {"stop_fixture": True}),
]


def scenario_checks_matrix(ctx: Ctx, only: set[str] | None = None) -> list[dict]:
    """only 為 None 時跑全部子項；否則只跑指定子項（例如 matrix:api-404）。"""
    return [scenario_fault_check_download(ctx, name, faults, codes, stage, **extra)
            for name, faults, codes, stage, extra in MATRIX_CASES
            if only is None or name in only]


def scenario_cleanup(ctx: Ctx) -> dict:
    """啟動時清除更新留下的舊 exe 殘留檔（<exe>.old.<時間戳>），一個都不留：啟動前放假的舊檔，
    真實程序啟動後全部被清掉；其他名稱相近的檔案（非純數字後綴、.bak 等）一律不動。"""
    name = "cleanup-on-start"
    ck = Check()
    v100, v101 = ctx.manifest["versions"]["1.0.0"], ctx.manifest["versions"]["1.0.1"]
    port, cdp = ctx.ports()
    fx = FixtureServer(pathlib.Path(v101["desktop_exe"]), v101["asset_name"], "v1.0.1")
    fx.start()
    app = None
    try:
        inst = prepare_install(ctx, name, "1.0.0", port)
        exe_name = "claude-miniapp-desktop.exe"
        stale = {
            f"{exe_name}.old.1000": b"old-1000",
            f"{exe_name}.old.2000": b"old-2000",
            f"{exe_name}.old.3000": b"old-3000",  # 最新的一份也要清掉
        }
        unrelated = {
            f"{exe_name}.old.abc": b"keep-abc",   # 後綴不是純數字
            f"{exe_name}.bak": b"keep-bak",       # Wails 的 .bak
            f"{exe_name}.old.2000.bak": b"keep-2000-bak",
            "other.exe.old.1": b"keep-other",     # 別的檔案的殘留
        }
        for files in (stale, unrelated):
            for fname, data in files.items():
                (inst / fname).write_bytes(data)
        app = App(ctx, name, inst, port, cdp, fx)
        app.start()
        wait_until(lambda: all(not (inst / f).exists() for f in stale), 60, 1.0, what="殘留檔被清除")
        ck.ok("所有殘留檔（含最新的一份）都已被清除", all(not (inst / f).exists() for f in stale),
              str(sorted(p.name for p in inst.iterdir())))
        ck.ok("名稱相近的其他檔案都沒被動", all((inst / f).read_bytes() == d for f, d in unrelated.items()), "")
        ck.ok("目前的 exe 未被改動", sha256(app.exe) == v100["desktop_sha256"], "")
        log = (inst / "logs" / "server.log").read_text(encoding="utf-8", errors="replace")
        ck.ok("日誌記錄了每一個被清除的檔案", all(f in log for f in stale) and "已清除舊的更新殘留檔" in log, "")
        ck.ok("日誌沒有提到不相關的檔案", not any(f in log.split("已清除舊的更新殘留檔")[-1] for f in unrelated), "")
    except Exception as exc:  # noqa: BLE001
        ck.ok("場景執行無例外", False, f"{exc}\n{traceback.format_exc()}")
    finally:
        if app is not None:
            app.stop()
        fx.stop()
    return finish(name, name, ck)


SCENARIOS = {
    "cleanup": lambda c: [scenario_cleanup(c)],
    "happy": lambda c: [scenario_happy(c)],
    "matrix": scenario_checks_matrix,
    "not-writable": lambda c: [scenario_not_writable(c)],
    "busy": lambda c: [scenario_busy(c)],
    "helper-fail": lambda c: [scenario_helper_fail(c)],
    "watchdog": lambda c: [scenario_watchdog(c)],
    "replacement-fail": lambda c: [scenario_replacement_fail(c)],
}


def cleanup_registry(ctx: Ctx) -> None:
    for p in ctx.procs:
        pid = p["pid"]
        if p["role"] == "file-holder-powershell":
            p["cleanup"] = "已自行結束（holder 約 45 秒後結束）或不存在"
            continue
        p["cleanup"] = kill_pid_if_path(pid, p["path"]) if pid else "略過"
        # helper 日誌以啟動它的桌面程序 PID 命名；只保存並移除本腳本產生的那一份。
        log = pathlib.Path(tempfile.gettempdir()) / f"wails-update-{pid}.log"
        if p["role"].startswith("desktop") and log.exists():
            ev = ctx.work / "evidence"
            ev.mkdir(parents=True, exist_ok=True)
            shutil.copy2(log, ev / f"{p['scenario']}-{log.name}")
            try:
                log.unlink()
                p["cleanup"] += "; helper 日誌已保存至 evidence 並移除暫存副本"
            except OSError as exc:
                p["cleanup"] += f"; helper 日誌移除失敗：{exc}"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--work", default=os.path.join(tempfile.gettempdir(), "cma-e2e"))
    ap.add_argument("--only", default="", help="逗號分隔：" + ",".join(SCENARIOS))
    ap.add_argument("--keep", action="store_true", help="保留 runs 目錄以便檢視")
    args = ap.parse_args()
    work = pathlib.Path(args.work).resolve()
    manifest_path = work / "dist" / "manifest.json"
    if not manifest_path.exists():
        print("找不到 manifest，請先執行 python scripts/e2e/build_fixtures.py", file=sys.stderr)
        return 2
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    ctx = Ctx(work, manifest)
    names = [n for n in (args.only.split(",") if args.only else SCENARIOS) if n]
    all_results: list[dict] = []
    try:
        for n in names:
            group, _, sub = n.partition(":")
            only = set(sub.split("+")) if sub else None
            bad_sub = only is not None and (group != "matrix" or not only <= {c[0] for c in MATRIX_CASES})
            if group not in SCENARIOS or bad_sub:
                print(f"未知場景：{n}（matrix 子項可用 matrix:<名稱>，多個以 + 連接）", file=sys.stderr)
                return 2
            print(f"[run] {n} ...", flush=True)
            out = scenario_checks_matrix(ctx, only) if group == "matrix" else SCENARIOS[group](ctx)
            all_results.extend(out)
            for r in out:
                print(f"[{r['verdict']}] {r['scenario']}", flush=True)
    finally:
        cleanup_registry(ctx)
        report = {"results": all_results, "processes": ctx.procs}
        (work / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        if not args.keep:
            runs = work / "runs"
            if runs.exists():
                shutil.rmtree(runs, ignore_errors=True)
        print(f"[report] {work / 'report.json'}")
    failed = [r for r in all_results if r["verdict"] != "PASS"]
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())

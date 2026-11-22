#!/usr/bin/env python3
"""同一 GITHUB_SHA 在 main 的 push 上，ci.yml 與 browser-tests.yml 都必須 completed/success。

只讀 GitHub Actions。GH_TOKEN 沿用呼叫端環境，程式不印出。缺資料、API 失敗、
尚未完成、非 success、或回傳欄位與查詢不一致，一律失敗。
"""
import json
import os
import re
import shutil
import subprocess
import sys

WORKFLOWS = ("ci.yml", "browser-tests.yml")


def fail(message):
    print("main gates check failed: " + message, file=sys.stderr)
    raise SystemExit(1)


def scrub(text):
    for key in ("GH_TOKEN", "GITHUB_TOKEN"):
        token = os.environ.get(key) or ""
        if token:
            text = text.replace(token, "[redacted]")
    return text


def require_env():
    sha = os.environ.get("GITHUB_SHA", "")
    repo = os.environ.get("GITHUB_REPOSITORY", "")
    if not sha:
        fail("GITHUB_SHA 未設定")
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        fail("GITHUB_SHA 不是 40 位十六進位")
    if not repo:
        fail("GITHUB_REPOSITORY 未設定")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        fail("GITHUB_REPOSITORY 格式不正確")
    return sha, repo


def query(repo, workflow, sha):
    gh = shutil.which("gh")
    if not gh:
        fail("找不到 gh")
    try:
        # -f/-F 會讓 gh 預設改成 POST。讀取 runs 必須明確 GET，參數才進入 query string。
        proc = subprocess.run(
            [
                gh, "api", "--method", "GET", f"repos/{repo}/actions/workflows/{workflow}/runs",
                "-f", f"head_sha={sha}", "-f", "branch=main", "-f", "event=push", "-F", "per_page=20",
            ],
            capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=30,
        )
    except subprocess.TimeoutExpired:
        fail(workflow + " 查詢逾時")
    if proc.returncode != 0:
        lines = scrub(proc.stderr).strip().splitlines()
        detail = lines[0][:180] if lines else "無輸出"
        fail(f"{workflow} 查詢失敗（gh exit {proc.returncode}）：{detail}")
    try:
        data = json.loads(proc.stdout)
    except json.JSONDecodeError:
        fail(workflow + " 回應不是 JSON")
    runs = data.get("workflow_runs")
    total = data.get("total_count")
    if not isinstance(runs, list) or not isinstance(total, int):
        fail(workflow + " 回應缺少 workflow_runs")
    if total != len(runs):
        fail(workflow + " 結果不完整")
    return runs


def exact(runs, workflow, sha):
    want = ".github/workflows/" + workflow
    for run in runs:
        if not isinstance(run, dict):
            fail(workflow + " 回傳的 run 不是物件")
        if run.get("head_sha") != sha:
            fail(workflow + " 回傳的 head_sha 不是這個 commit")
        if run.get("head_branch") != "main":
            fail(workflow + " 回傳的 branch 不是 main")
        if run.get("event") != "push":
            fail(workflow + " 回傳的 event 不是 push")
        if run.get("path") != want:
            fail(workflow + " 回傳的 workflow path 不是 " + want)
    if len(runs) != 1:
        fail(f"{workflow} 相符的 main push run 應恰好一個，實際 {len(runs)}")
    run = runs[0]
    if run.get("status") != "completed":
        fail(f"{workflow} 狀態是 {run.get('status')}，尚未 completed")
    if run.get("conclusion") != "success":
        fail(f"{workflow} conclusion 是 {run.get('conclusion')}，不是 success")


def main():
    sha, repo = require_env()
    for workflow in WORKFLOWS:
        exact(query(repo, workflow, sha), workflow, sha)
    print("main gates ok: ci.yml and browser-tests.yml completed/success at " + sha + " on main push")


if __name__ == "__main__":
    main()

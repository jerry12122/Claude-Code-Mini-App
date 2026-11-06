#!/usr/bin/env python3
"""讀取 fake_gh.py 的狀態目錄，供 test-release.sh 做斷言（只讀，不發出任何請求）。

用法（<st> 為狀態目錄）：
  state_check.py status <st> <tag>            -> none | draft | published [latest]
  state_check.py names  <st> <tag> <名稱...>   -> 資產名稱集合必須完全相同（exit 0/1）
  state_check.py notes-order <st> <tag>        -> 更新內容在最前，下載、更新方法與首次設定依序在後
  state_check.py writes <st>                  -> 輸出寫入呼叫次數
  state_check.py publish-last <st>            -> 寫入序列必須是 create→upload→…→edit-publish，且 edit-publish 為最後一筆
"""
import json
import os
import sys


def load(st):
    with open(os.path.join(st, "state.json"), encoding="utf-8") as f:
        return json.load(f)


def calls(st):
    p = os.path.join(st, "calls.log")
    if not os.path.exists(p):
        return []
    with open(p, encoding="utf-8") as f:
        return [line.strip() for line in f if line.strip()]


def release(st, tag):
    for r in load(st)["releases"]:
        if r["tag_name"] == tag:
            return r
    return None


def main(argv):
    cmd, st = argv[0], argv[1]
    if cmd == "writes":
        print(sum(1 for c in calls(st) if c.startswith("WRITE")))
        return 0
    if cmd == "publish-last":
        seq = [c for c in calls(st) if c.startswith("WRITE")]
        kinds = [c.split()[1] for c in seq]
        print(kinds)
        if not kinds or kinds[-1] != "edit-publish":
            return 1
        if "upload" not in kinds or kinds.index("upload") > len(kinds) - 2:
            return 1
        if "create" in kinds and kinds.index("create") > kinds.index("upload"):
            return 1
        return 0
    tag = argv[2]
    r = release(st, tag)
    if cmd == "notes-order":
        notes = (r or {}).get("notes", "")
        headings = ["## 更新內容\n", "## 下載\n", "## 桌面版應用程式內更新（Windows）\n", "## 首次設定\n"]
        positions = [notes.find(h) for h in headings]
        return 0 if (notes.startswith(headings[0]) and all(notes.count(h) == 1 for h in headings)
                     and positions == sorted(positions) and "### " in notes[:positions[1]]
                     and "@TAG@" not in notes and tag in notes) else 1
    if cmd == "status":
        if r is None:
            print("none")
        else:
            print(("draft" if r["draft"] else "published") + (" latest" if r.get("latest") else ""))
        return 0
    if cmd == "names":
        want = sorted(argv[3:])
        have = sorted(a["name"] for a in (r or {}).get("assets", []))
        print("have:", have)
        return 0 if have == want else 1
    print("未知指令", cmd, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

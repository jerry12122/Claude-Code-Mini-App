#!/usr/bin/env python3
"""檢查 .github/workflows/release.yml 的 YAML 語法與發佈流程結構（只讀，不呼叫 GitHub）。

用法：check_workflow.py <release.yml>
需要 PyYAML（pip install pyyaml）。任一斷言失敗即以非零結束。
"""
import os
import re
import sys

try:
    import yaml
except ImportError:
    print("需要 PyYAML：pip install pyyaml", file=sys.stderr)
    sys.exit(2)

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
errors = []


def expect(cond, msg):
    if not cond:
        errors.append(msg)


def main(path):
    with open(path, encoding="utf-8") as f:
        doc = yaml.safe_load(f)  # 語法錯誤會在這裡丟出例外

    # PyYAML 1.1 會把未加引號的 on 解析成 True
    trigger = doc.get(True, doc.get("on"))
    expect(trigger is not None, "缺少 on 觸發條件")
    if trigger is not None:
        expect(trigger.get("push", {}).get("tags") == ["v*"], "觸發條件應為 push tags v*")

    expect(doc.get("permissions") == {"contents": "write"}, "頂層 permissions 應為 contents: write")

    job = doc.get("jobs", {}).get("release")
    expect(job is not None, "缺少 job release")
    if job is None:
        return
    expect(job.get("runs-on") == "windows-latest", "runs-on 應為 windows-latest")
    expect(job.get("defaults", {}).get("run", {}).get("shell") == "bash", "預設 shell 應為 bash")

    steps = job.get("steps", [])
    names = [s.get("name", s.get("uses", "")) for s in steps]

    def index_of(pred, what):
        for i, s in enumerate(steps):
            if pred(s):
                return i
        expect(False, "找不到步驟：" + what)
        return -1

    i_checkout = index_of(lambda s: str(s.get("uses", "")).startswith("actions/checkout"), "checkout")
    i_go = index_of(lambda s: str(s.get("uses", "")).startswith("actions/setup-go"), "setup-go")
    i_py = index_of(lambda s: str(s.get("uses", "")).startswith("actions/setup-python"), "setup-python")
    i_ver = index_of(lambda s: "check-version.sh" in str(s.get("run", "")), "check-version.sh")
    i_test = index_of(lambda s: str(s.get("run", "")).strip() == "go test ./internal/...", "go test ./internal/...")
    i_test_p = index_of(lambda s: str(s.get("run", "")).strip() == "go test -tags production ./internal/...",
                        "go test -tags production ./internal/...")
    i_pack = index_of(lambda s: "package.sh dist" in str(s.get("run", "")), "package.sh dist")
    i_scan = index_of(lambda s: "govulncheck ./cmd/server" in str(s.get("run", "")), "govulncheck")
    i_pub = index_of(lambda s: "publish.sh dist" in str(s.get("run", "")), "publish.sh dist")

    expect(i_checkout < i_go < i_py, "順序應為 checkout → setup-go → setup-python")
    expect(i_py < i_ver < i_test < i_test_p < i_pack < i_pub,
           "順序應為 setup-python → check-version → go test → go test -tags production → package → publish")
    expect(i_ver < i_scan < i_pack, "漏洞掃描必須在版本檢查後、打包前")
    if i_scan >= 0:
        scan = steps[i_scan]
        commands = str(scan.get("run", ""))
        expect("go install golang.org/x/vuln/cmd/govulncheck@v1.9.0" in commands,
               "掃描工具必須使用固定版本")
        expect("govulncheck -tags production,devtools ./cmd/desktop" in commands,
               "必須涵蓋 Windows 桌面正式版")
        expect("CGO_ENABLED=0 GOOS=linux GOARCH=amd64 govulncheck ./cmd/server" in commands,
               "必須涵蓋 Linux server")
        expect(not scan.get("continue-on-error"), "漏洞掃描失敗必須阻擋發佈")
        expect(not re.search(r"\|\||-json|-format|set\s+\+e", commands),
               "不得忽略掃描退出碼或使用不會阻擋漏洞的輸出模式")

    if i_pub >= 0:
        env = steps[i_pub].get("env", {})
        expect(env.get("GH_TOKEN") == "${{ github.token }}", "發佈步驟的 GH_TOKEN 應為 github.token")

    # 發佈只能經由 publish.sh（草稿優先）；workflow 本身不得直接 gh release create / edit --draft=false
    text = open(path, encoding="utf-8").read()
    expect(not re.search(r"gh\s+release\s+create", text), "workflow 不得直接 gh release create（會立即公開）")
    expect(not re.search(r"--draft=false", text), "--draft=false 只能出現在 publish.sh")

    # 每個被呼叫的腳本都必須存在
    for s in steps:
        for ref in re.findall(r"scripts/release/[\w./-]+", str(s.get("run", ""))):
            expect(os.path.isfile(os.path.join(REPO, ref)), "步驟引用的腳本不存在：" + ref)

    print("workflow steps:")
    for n in names:
        print("  - " + str(n))


if __name__ == "__main__":
    main(sys.argv[1])
    if errors:
        for e in errors:
            print("FAIL " + e, file=sys.stderr)
        sys.exit(1)
    print("workflow structure ok")

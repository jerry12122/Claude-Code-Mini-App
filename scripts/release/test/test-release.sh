#!/usr/bin/env bash
# 發佈腳本的本機測試（不連線 GitHub，不建立任何 Release、tag 或 commit）。
#   - 假的 gh（fake_gh.py）放在暫存 PATH 最前面，只讀寫暫存狀態目錄；未預期的子命令會失敗。
#   - 檢查：腳本語法、版本一致性（暫存 git repo）、打包內容與 checksums、
#     草稿優先發佈的成功與各種失敗路徑、provider 契約（TestReleaseDistContract）、workflow 結構。
# 用法（Git Bash on Windows，需要 Go、7-Zip、Python 3 與 PyYAML）：bash scripts/release/test/test-release.sh
set -uo pipefail

root="$(cd "$(dirname "$0")/../../.." && pwd)"
rel="$root/scripts/release"
sc="$rel/test/state_check.py"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

pass=0
failed=0
ok() { pass=$((pass + 1)); echo "PASS  $1"; }
bad() { failed=$((failed + 1)); echo "FAIL  $1"; }
show() { sed 's/^/      /' "$work/out.txt" | tail -n 12; }

# check "描述" 指令...：指令成功即 PASS
check() {
  local desc="$1"; shift
  if "$@" >"$work/out.txt" 2>&1; then ok "$desc"; else bad "$desc"; show; fi
}
# expect_fail "描述" 指令...：指令失敗即 PASS
expect_fail() {
  local desc="$1"; shift
  if "$@" >"$work/out.txt" 2>&1; then bad "$desc（預期失敗卻成功）"; show; else ok "$desc"; fi
}

# 混合路徑（C:/...）：給 Windows 原生程式（python、7z）與環境變數用
wpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else echo "$1"; fi; }

# ── 假 gh：暫存 PATH 最前面 ───────────────────────────────────────────
py="$(command -v python)"
shim="$work/shim"
mkdir -p "$shim"
printf '#!/usr/bin/env bash\nexec "%s" "%s" "$@"\n' "$py" "$rel/test/fake_gh.py" > "$shim/gh"
chmod +x "$shim/gh"
export PATH="$shim:$PATH"
[ "$(command -v gh)" = "$shim/gh" ] || { echo "gh 不是假的 shim，中止"; exit 2; }
command -v 7z >/dev/null 2>&1 || export PATH="/c/Program Files/7-Zip:$PATH"

ver="$(tr -d '\r\n' < "$root/VERSION")"
export GITHUB_REF_NAME="v$ver"
export GITHUB_REPOSITORY="fixture-owner/fixture-repo"
export GH_TOKEN="fixture-token-not-real"
export GITHUB_SHA="$(git -C "$root" rev-parse HEAD)"
tag="$GITHUB_REF_NAME"
desktop="claude-miniapp-desktop-v$ver-windows-amd64.exe"
expected=("claude-miniapp-$tag-windows-amd64.zip" "claude-miniapp-$tag-linux-amd64.tar.gz" "$desktop" "checksums.txt")

# ── 1. 語法 ───────────────────────────────────────────────────────────
for f in check-version.sh package.sh publish.sh; do
  check "bash -n $f" bash -n "$rel/$f"
done
check "python 語法：release_state.py、fake_gh.py、state_check.py" \
  python -m py_compile "$rel/release_state.py" "$rel/test/fake_gh.py" "$sc"

# ── 2. 版本一致性（在暫存 git repo 內，不動真實 repo 的 tag）──────────
vrepo="$work/vrepo"
mkdir -p "$vrepo/internal/version"
cp "$root/VERSION" "$vrepo/VERSION"
cp "$root/internal/version/version.go" "$vrepo/internal/version/version.go"
git -C "$vrepo" init -q
git -C "$vrepo" add -A
git -C "$vrepo" -c user.name=test -c user.email=test@example.invalid commit -q -m fixture
git -C "$vrepo" tag "$tag"
vsha="$(git -C "$vrepo" rev-parse HEAD)"
run_check() { (cd "$vrepo" && env GITHUB_REF_NAME="$1" GITHUB_SHA="$2" bash "$rel/check-version.sh"); }
check "版本一致：tag／VERSION／version.go／commit 全部相符" run_check "$tag" "$vsha"
expect_fail "tag 與 VERSION 不符（v9.9.9）" run_check "v9.9.9" "$vsha"
expect_fail "觸發 SHA 與 tag 指向的 commit 不符" run_check "$tag" "0000000000000000000000000000000000000000"
expect_fail "tag 含預發版字尾（v0.12.0-rc1）" run_check "$tag-rc1" "$vsha"
sed -i 's/const Version = "[^"]*"/const Version = "0.0.1"/' "$vrepo/internal/version/version.go"
git -C "$vrepo" -c user.name=test -c user.email=test@example.invalid commit -qam drift
git -C "$vrepo" tag -f "$tag" >/dev/null
expect_fail "version.go 的 Version 與 VERSION 不符" run_check "$tag" "$(git -C "$vrepo" rev-parse HEAD)"

# ── 3. 打包：四個資產、checksums 涵蓋三個程式資產、ZIP 內 EXE 與版本化 EXE 位元組相同 ──
dist="$work/dist"
check "package.sh 產出發佈資產" bash "$rel/package.sh" "$dist"
for n in "${expected[@]}"; do
  [ -s "$dist/$n" ] || bad "缺少資產 $n"
done
check "dist 恰好包含預期的四個檔案" bash -c '[ "$(ls "$1" | wc -l)" -eq 4 ]' _ "$dist"
check "checksums.txt 恰好三行，且包含桌面 EXE" bash -c \
  '[ "$(wc -l < "$1/checksums.txt")" -eq 3 ] && grep -qF "  $2" "$1/checksums.txt"' _ "$dist" "$desktop"
(cd "$dist" && sha256sum --text "${expected[@]}") | sed 's/^/      /'
# 本機 internal/static/uploads 有使用者截圖（已 gitignore）；產物不得包含它們
check "產物不含 internal/static/uploads（使用者資料）" bash -c \
  '[ "$(tar -tzf "$1/claude-miniapp-$2-linux-amd64.tar.gz" | grep -c "/internal/static/uploads/")" -eq 0 ] && [ "$(7z l "$1/claude-miniapp-$2-windows-amd64.zip" | grep -c "static.uploads")" -eq 0 ]' _ "$dist" "$tag"

# ── 4. 草稿優先發佈 ─────────────────────────────────────────────────────
new_state() { local d="$work/st-$1"; rm -rf "$d"; mkdir -p "$d"; echo "$d"; }
run_publish() { # $1=狀態目錄；其餘為額外 env（例如 FAKE_GH_FAIL=upload）
  local st="$1"; shift
  env FAKE_GH_STATE="$(wpath "$st")" "$@" bash "$rel/publish.sh" "$dist"
}
seed() { # $1=狀態目錄 $2=draft|published 其餘為既有資產名
  local st="$1"; shift
  env FAKE_GH_STATE="$(wpath "$st")" gh seed "$@" >/dev/null
}
sc_run() { python "$sc" "$@"; }
names_ok() { sc_run names "$(wpath "$1")" "$tag" "${expected[@]}" >/dev/null; }

# 4a. 全新發佈（沒有任何 Release）
st="$(new_state fresh)"
check "全新發佈：建立草稿、上傳、比對、公開為 latest" run_publish "$st"
check "全新發佈後狀態為 published latest" bash -c \
  '[ "$(python "$1" status "$2" "$3")" = "published latest" ]' _ "$sc" "$(wpath "$st")" "$tag"
check "全新發佈後資產恰為預期四個" names_ok "$st"
check "全新發佈：更新內容在最上方，下載與更新方法在下方" sc_run notes-order "$(wpath "$st")" "$tag"
check "全新發佈：公開是最後一個寫入動作" sc_run publish-last "$(wpath "$st")"

# 4b. 已有公開 Release：拒絕，且零寫入
st="$(new_state published)"
seed "$st" published
expect_fail "已公開的 tag：拒絕覆寫" run_publish "$st"
check "已公開的 tag：零寫入呼叫" bash -c '[ "$(python "$1" writes "$2")" -eq 0 ]' _ "$sc" "$(wpath "$st")"

# 4c. 草稿重跑：舊資產移除、重新上傳、最後公開
st="$(new_state draft)"
seed "$st" draft "claude-miniapp-desktop-v0.11.0-windows-amd64.exe" "claude-miniapp-v0.11.0-windows-amd64.zip"
check "草稿重跑：重用草稿並公開" run_publish "$st"
check "草稿重跑：說明採用更新內容在前的新順序" sc_run notes-order "$(wpath "$st")" "$tag"
check "草稿重跑：舊資產已移除，只剩預期四個" names_ok "$st"
check "草稿重跑：舊資產以 delete-asset 移除（2 次）" bash -c \
  '[ "$(grep -c "^WRITE delete-asset" "$1/calls.log")" -eq 2 ]' _ "$st"

# 4d. 上傳中途失敗：停在草稿、不公開
st="$(new_state upload-fail)"
expect_fail "上傳失敗：腳本失敗" run_publish "$st" FAKE_GH_FAIL=upload
check "上傳失敗：仍是草稿" bash -c '[ "$(python "$1" status "$2" "$3")" = "draft" ]' _ "$sc" "$(wpath "$st")" "$tag"
check "上傳失敗：沒有公開呼叫" bash -c '! grep -q "^WRITE edit-publish" "$1/calls.log"' _ "$st"

# 4e. 遠端位元組被竄改：比對失敗、停在草稿
st="$(new_state corrupt)"
expect_fail "遠端位元組與本機不同：腳本失敗" run_publish "$st" FAKE_GH_CORRUPT="$desktop"
check "遠端位元組不符：仍是草稿、沒有公開呼叫" bash -c \
  '[ "$(python "$1" status "$2" "$3")" = "draft" ] && ! grep -q "^WRITE edit-publish" "$4/calls.log"' \
  _ "$sc" "$(wpath "$st")" "$tag" "$st"

# 4f. 公開動作本身失敗：仍停在草稿
st="$(new_state publish-fail)"
expect_fail "公開動作失敗：腳本失敗" run_publish "$st" FAKE_GH_FAIL=publish
check "公開動作失敗：仍是草稿" bash -c '[ "$(python "$1" status "$2" "$3")" = "draft" ]' _ "$sc" "$(wpath "$st")" "$tag"

# 4g. 本機缺一個資產：在任何寫入前失敗
miss="$work/dist-missing"
rm -rf "$miss"; cp -r "$dist" "$miss"; rm "$miss/$desktop"
st="$(new_state missing)"
expect_fail "缺少桌面 EXE：腳本失敗" env FAKE_GH_STATE="$(wpath "$st")" bash "$rel/publish.sh" "$miss"
check "缺少桌面 EXE：零寫入" bash -c '[ "$(python "$1" writes "$2")" -eq 0 ]' _ "$sc" "$(wpath "$st")"

# 4h. 本機 checksums 與檔案不符：在任何寫入前失敗
badsum="$work/dist-badsum"
rm -rf "$badsum"; cp -r "$dist" "$badsum"; printf 'x' >> "$badsum/$desktop"
st="$(new_state badsum)"
expect_fail "本機 checksums 不符：腳本失敗" env FAKE_GH_STATE="$(wpath "$st")" bash "$rel/publish.sh" "$badsum"
check "本機 checksums 不符：零寫入" bash -c '[ "$(python "$1" writes "$2")" -eq 0 ]' _ "$sc" "$(wpath "$st")"

# ── 5. provider 契約：真實打包產物通過 desktopupdate 的解析與驗證 ─────────
check "TestReleaseDistContract：真實 checksums.txt 與桌面 EXE 通過 provider 規則" \
  env RELEASE_DIST="$(wpath "$dist")" go -C "$root" test -count=1 -run TestReleaseDistContract ./internal/desktopupdate/

# ── 6. workflow 結構與 build.bat ─────────────────────────────────────
check "release.yml 可解析且結構正確" python "$rel/test/check_workflow.py" "$root/.github/workflows/release.yml"
check "build.bat 使用 production 標籤" grep -Eq -- '-tags[[:space:]]+"?production([,"]|[[:space:]]|$)' "$root/build.bat"

echo
echo "結果：通過 $pass、失敗 $failed"
[ "$failed" -eq 0 ]

#!/usr/bin/env bash
# 草稿優先的發佈（只在 CI 發佈步驟執行；需要 GITHUB_REF_NAME、GITHUB_REPOSITORY、GH_TOKEN）：
#   1. 同 tag 已有「公開」Release → 停止，禁止覆寫公開版本。
#   2. 同 tag 有草稿 → 重用：移除不在預期清單內的舊資產，更新說明；沒有草稿 → 建立草稿。
#   3. 上傳預期的四個資產（同名以 --clobber 覆寫，只發生在草稿上）。
#   4. 從草稿下載每個資產，比對位元組與 checksums.txt。
#   5. 全部通過後才把草稿改為公開並設為 latest。
# 任一步失敗都停在草稿：草稿不會成為 latest，updater 看不到半成品。
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"
fail() { echo "publish failed: $*" >&2; exit 1; }

dist="$(cd "${1:-dist}" && pwd)"
tag="${GITHUB_REF_NAME:?GITHUB_REF_NAME 未設定}"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY 未設定}"
ver="${tag#v}"
desktop_name="claude-miniapp-desktop-v$ver-windows-amd64.exe"
expected=(
  "claude-miniapp-$tag-windows-amd64.zip"
  "claude-miniapp-$tag-linux-amd64.tar.gz"
  "$desktop_name"
  "checksums.txt"
)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

is_expected() {
  local x
  for x in "${expected[@]}"; do [ "$x" = "$1" ] && return 0; done
  return 1
}

# 0. 上傳前的本機檢查：資產齊備、checksums 與檔案相符（不發出任何寫入請求）
for n in "${expected[@]}"; do [ -s "$dist/$n" ] || fail "缺少資產 $n"; done
(cd "$dist" && sha256sum -c checksums.txt >/dev/null) || fail "本機 checksums.txt 驗證失敗"

# 說明：先放 CHANGELOG 對應版本的更新內容，再附上下載、更新方法與首次設定。
sed "s/@TAG@/$tag/g" "$repo_root/.github/release-header.md" > "$work/header.md"
awk -v v="$ver" '/^## \[/{p = index($0, "[" v "]") > 0; next} p' "$repo_root/CHANGELOG.md" > "$work/changes.md"
test -s "$work/changes.md" || fail "CHANGELOG.md 沒有 [$ver] 區段"
{
  printf '## 更新內容\n\n'
  cat "$work/changes.md"
  printf '\n'
  cat "$work/header.md"
} > "$work/notes.md"

# 1. 查詢同 tag 的 Release（列表包含草稿）
gh api "repos/$repo/releases?per_page=100" > "$work/releases.json"
# 解析輸出一律去掉 CR（第二道防線；release_state.py 已固定輸出 LF）：asset id 帶 CR 會讓 gh 以 invalid control character 拒絕。
state="$(python "$here/release_state.py" find "$tag" < "$work/releases.json" | tr -d '\r')"
case "$state" in
  none)
    echo "建立草稿 $tag"
    gh release create "$tag" -R "$repo" --draft --title "$tag" --notes-file "$work/notes.md"
    ;;
  published\ *)
    fail "$tag 已公開，禁止覆寫公開版本；請改發新版本號"
    ;;
  draft\ *)
    echo "重用既有草稿 $tag（${state#draft }）"
    gh release edit "$tag" -R "$repo" --notes-file "$work/notes.md" >/dev/null
    while read -r name _id _size; do
      if ! is_expected "$name"; then
        echo "移除草稿中的舊資產 $name"
        gh release delete-asset "$tag" "$name" -R "$repo" --yes
      fi
    done < <(python "$here/release_state.py" assets "$tag" < "$work/releases.json" | tr -d '\r')
    ;;
  *)
    fail "無法解析 Release 狀態：$state"
    ;;
esac

# 2. 上傳預期資產（草稿上可覆寫同名資產）
paths=()
for n in "${expected[@]}"; do paths+=("$dist/$n"); done
gh release upload "$tag" -R "$repo" --clobber "${paths[@]}"

# 3. 從草稿下載並比對：名稱集合、大小、位元組與 checksums.txt
gh api "repos/$repo/releases?per_page=100" > "$work/releases2.json"
python "$here/release_state.py" verify "$tag" "$dist" "${expected[@]}" < "$work/releases2.json" > "$work/ids.raw" \
  || fail "遠端資產驗證失敗（停在草稿）"
tr -d '\r' < "$work/ids.raw" > "$work/ids.txt"
mkdir "$work/remote"
while read -r name aid; do
  gh api -H "Accept: application/octet-stream" "repos/$repo/releases/assets/$aid" > "$work/remote/$name" \
    || fail "下載 $name 失敗（停在草稿）"
  cmp -s "$work/remote/$name" "$dist/$name" || fail "$name 的遠端內容與本機不同（停在草稿）"
done < "$work/ids.txt"
(cd "$work/remote" && sha256sum -c checksums.txt >/dev/null) || fail "遠端 checksums.txt 驗證失敗（停在草稿）"

# 4. 全部通過後才公開為 latest
gh release edit "$tag" -R "$repo" --draft=false --latest
echo "已公開 $tag（latest）"

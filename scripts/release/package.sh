#!/usr/bin/env bash
# 組出發佈資產到 OUT 目錄（預設 dist），內容與檔名如下：
#   claude-miniapp-<tag>-windows-amd64.zip      （原有：server + 桌面 production EXE + 前端與文件）
#   claude-miniapp-<tag>-linux-amd64.tar.gz     （原有：Linux server）
#   claude-miniapp-desktop-v<X.Y.Z>-windows-amd64.exe   （新增：桌面 production EXE，供自我更新）
#   checksums.txt                               （sha256sum 格式，涵蓋以上三個程式資產）
# 桌面 EXE 只建置一次，ZIP 內與版本化 EXE 的位元組必須相同（本腳本會檢查）。
# 檔名 claude-miniapp-desktop-v<X.Y.Z>-windows-amd64.exe 必須與 internal/desktopupdate/provider.go 的
# AssetName / desktopAssetPattern 一致，由 internal/desktopupdate/release_contract_test.go 驗證。
set -euo pipefail

fail() { echo "package failed: $*" >&2; exit 1; }

tag="${GITHUB_REF_NAME:?GITHUB_REF_NAME 未設定}"
sha="${GITHUB_SHA:-$(git rev-parse HEAD)}"
ver="${tag#v}"
out_arg="${1:-dist}"
mkdir -p "$out_arg"
out="$(cd "$out_arg" && pwd)"
rm -f "$out"/claude-miniapp-* "$out/checksums.txt"

w="claude-miniapp-$tag-windows-amd64"
l="claude-miniapp-$tag-linux-amd64"
zip_name="$w.zip"
tgz_name="$l.tar.gz"
desktop_name="claude-miniapp-desktop-v$ver-windows-amd64.exe"

stage_root="$(mktemp -d)"
verify_root="$(mktemp -d)"
trap 'rm -rf "$stage_root" "$verify_root"' EXIT

# 前端由磁碟讀取（./internal/static），不能只發 binary
stage() { # $1=目錄名（位於 stage_root 底下）
  local d="$stage_root/$1"
  mkdir -p "$d/internal"
  cp -r internal/static "$d/internal/static"
  rm -rf "$d/internal/static/uploads"  # 使用者的聊天截圖（已 gitignore），絕不打包
  cp -r docs "$d/docs"
  cp config.example.yaml README.md README.zh-TW.md LICENSE CHANGELOG.md "$d/"
}

# Windows：server + 桌面（production 標籤，前端嵌入）
stage "$w"
go build -o "$stage_root/$w/claude-miniapp.exe" ./cmd/server
go build -tags "production,devtools" -ldflags "-H windowsgui" -buildvcs=true \
  -o "$stage_root/$w/claude-miniapp-desktop.exe" ./cmd/desktop
(cd "$stage_root" && 7z a -tzip "$out/$zip_name" "$w" >/dev/null)

# 版本化桌面 EXE：與 ZIP 內的桌面 EXE 同一份位元組
cp "$stage_root/$w/claude-miniapp-desktop.exe" "$out/$desktop_name"

# 嵌入的 vcs.revision 必須是觸發 tag 的 commit
go version -m "$out/$desktop_name" | grep -q "vcs.revision=$sha" \
  || fail "桌面 EXE 的 vcs.revision 不是 $sha"

# Linux：純 Go server，不含桌面
stage "$l"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$stage_root/$l/claude-miniapp" ./cmd/server
tar -czf "$out/$tgz_name" -C "$stage_root" "$l"

# checksums.txt 涵蓋所有程式資產（ZIP、tar.gz、桌面 EXE）
(cd "$out" && sha256sum --text "$zip_name" "$tgz_name" "$desktop_name" > checksums.txt)

# 驗證 1：checksums 與檔案相符
(cd "$out" && sha256sum -c checksums.txt >/dev/null) || fail "checksums.txt 與產物不符"

# 驗證 2：ZIP 內的桌面 EXE 與版本化 EXE 位元組相同
winpath() { if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else echo "$1"; fi; }
(cd "$verify_root" && 7z x -y "$(winpath "$out/$zip_name")" >/dev/null)
[ ! -e "$verify_root/$w/internal/static/uploads" ] || fail "ZIP 含 internal/static/uploads（使用者資料）"
if tar -tzf "$out/$tgz_name" | grep -q '/internal/static/uploads/'; then fail "tar.gz 含 internal/static/uploads（使用者資料）"; fi
cmp "$verify_root/$w/claude-miniapp-desktop.exe" "$out/$desktop_name" \
  || fail "ZIP 內的桌面 EXE 與 $desktop_name 位元組不同"

echo "package ok: $out"
ls -l "$out"

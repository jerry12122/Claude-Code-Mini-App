"""
實驗腳本：分析 kiro-cli 原始回覆內容，測試「工具敘述行過濾」與「code fence 補齊」的 heuristic。
用法：python markdown_experiment.py kiro_msg1.txt kiro_msg2.txt
輸出：xxx.before.md（原始 content 直接餵給 markdown 的樣子）與 xxx.after.md（套用 heuristic 後）
"""
import re
import sys
import io

# 觀察到的工具敘述行 pattern（不應混入最終回覆內文）
NARRATION_PATTERNS = [
    r"^Reading file: .+",
    r"^Searching for: .+",
    r"^✓ Successfully .+",
    r"^✗ .+",
    r"^- Completed in .+",
    r"^Batch \w+ operation .+",
    r"^↱ Operation \d+: .+",
    r"^⋮\s*$",
    r"^- Summary: .+",
    r"^\(\d+ more items? found\)$",
    r"^I will run the following command: .+",
    r"^Purpose: .+",
]
NARRATION_RE = re.compile("|".join(f"(?:{p})" for p in NARRATION_PATTERNS))

# "工具搜尋結果列表" 的縮排項目，例如：
#   1. Method ComputeFooIntent at service\foopkg\intent.go:57:1
TOOL_RESULT_ITEM_RE = re.compile(r"^\s*\d+\.\s+(Method|Function|Type|Class|Variable)\s+\S+\s+at\s+\S+:\d+")

LANG_TAGS = {
    "go", "python", "py", "javascript", "js", "typescript", "ts", "json",
    "yaml", "yml", "bash", "sh", "shell", "sql", "java", "c", "cpp", "c++",
    "csharp", "cs", "rust", "ruby", "php", "html", "css", "powershell",
    "dockerfile", "toml", "xml", "ini", "kotlin", "swift", "scala", "r",
    "lua", "perl", "makefile", "text",
}


def is_narration(line: str) -> bool:
    if NARRATION_RE.match(line.strip()):
        return True
    if TOOL_RESULT_ITEM_RE.match(line):
        return True
    return False


def looks_like_prose(line: str) -> bool:
    """粗略判斷該行是否為中文/英文「敘述句」而非程式碼。"""
    s = line.strip()
    if not s:
        return True
    # 明顯是程式碼結構符號開頭/結尾，視為程式碼
    if re.search(r"[{}();]\s*$", s) or s.startswith(("func ", "var ", "type ", "import ", "package ", "const ")):
        return False
    # 內含中文標點或大量中文字元，視為敘述句
    han_count = len(re.findall(r"[\u4e00-\u9fff]", s))
    if han_count >= 4:
        return True
    if re.search(r"[。！？：，]", s):
        return True
    return False


def refence_code_blocks(lines):
    """對「單獨一行語言標籤 + 之後幾行程式碼」的 pattern 補上 ``` fence。"""
    out = []
    i = 0
    n = len(lines)
    while i < n:
        line = lines[i]
        tag = line.strip().lower()
        if tag in LANG_TAGS and i + 1 < n and not looks_like_prose(lines[i + 1]):
            out.append(f"```{tag}")
            i += 1
            while i < n and not looks_like_prose(lines[i]):
                out.append(lines[i])
                i += 1
            out.append("```")
            continue
        out.append(line)
        i += 1
    return out


def process(content: str):
    lines = content.split("\n")
    kept = [l for l in lines if not is_narration(l)]
    fenced = refence_code_blocks(kept)
    return "\n".join(fenced)


def main():
    for path in sys.argv[1:]:
        with io.open(path, "r", encoding="utf-8") as f:
            raw = f.read()
        # kiro_msgN.txt 格式：id|role|content（第一則訊息起，每行一筆，但 content 內含換行，
        # sqlite3 list mode 用 \n 分行，這裡簡化只示範單一大 blob 的情況，實際上請改用結構化匯出。）
        out = process(raw)
        before_path = path.replace(".txt", ".before.md")
        after_path = path.replace(".txt", ".after.md")
        with io.open(before_path, "w", encoding="utf-8") as f:
            f.write(raw)
        with io.open(after_path, "w", encoding="utf-8") as f:
            f.write(out)
        print(f"{path}: {len(raw)} chars -> filtered narration + re-fenced -> {after_path}")


if __name__ == "__main__":
    main()

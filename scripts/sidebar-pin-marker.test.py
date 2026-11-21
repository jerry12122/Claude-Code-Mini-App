"""展開側欄的釘選列使用既有📌，不啟服務、不打外網。

執行：python scripts/sidebar-pin-marker.test.py
產物預設在系統暫存；SIDEBAR_PIN_ARTIFACTS 可改路徑。
"""
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
root = repo / "internal/static"
out = Path(os.environ.get("SIDEBAR_PIN_ARTIFACTS") or (Path(tempfile.gettempdir()) / "sidebar-pin-artifacts"))
phase = os.environ.get("SIDEBAR_PIN_PHASE", "run")
out.mkdir(parents=True, exist_ok=True)
html = (root / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", html, re.S))
config = re.search(r"<script>\s*(tailwind.config = .*?)</script>", html, re.S)[1]
source = "\n".join((root / "js" / name).read_text(encoding="utf-8") for name in [
    "hooks/messageRows.js", "ui/uiAtoms.js", "ui/ContextMenu.js", "chat/MessageActions.js",
    "ui/SessionTabs.js", "session/SessionCategories.js", "session/manualUnread.js", "session/SessionView.js",
])
harness = r"""
const {useState,useEffect,useLayoutEffect,useRef,useCallback,useMemo}=React;
const AGENT_LABEL={claude:'Claude',cursor:'Cursor',codex:'Codex'};
function FileBrowserModal(){return null}
function showToast(){}
function hapticTap(){}
function copyText(){return Promise.resolve(false)}
function eventsWsURL(){return 'ws://127.0.0.1/events'}
class FakeSocket{set onopen(_){} set onmessage(_){} set onclose(_){} close(){}}
window.WebSocket=FakeSocket;
const requests=[];
const enters=[];
let rows=[
  {id:'A',name:'會話A',status:'running',unread_count:3,agent_type:'claude',work_dir:'D:/work',pinned:true},
  {id:'B',name:'會話B',status:'idle',unread_count:0,agent_type:'claude',work_dir:'D:/work',pinned:false},
];
async function apiFetch(url, opts={}){
  const method=opts.method||'GET';
  requests.push(method+' '+url+(opts.body?(' '+opts.body):''));
  if ((opts.method||'GET')==='GET' && (url==='http://127.0.0.1/sessions' || url==='/sessions' || String(url).startsWith('https://') || String(url).startsWith('http://'))) {
    if (url!=='/sessions') return {ok:false, status:404, json:async()=>({}), text:async()=>'blocked'};
  }
  if (url==='/sessions') return {ok:true, json:async()=>rows.map(s=>({...s}))};
  if (method==='PATCH' && url==='/sessions/A') return {ok:true, json:async()=>({})};
  return {ok:false, status:404, json:async()=>({}), text:async()=>'unexpected'};
}
function Harness(){
  return <div style={{width:320,height:'100vh',background:'#111'}}>
    <SessionView activeSessionId="A" isWideScreen
      onEnter={(s)=>{enters.push(s.id);}} onToggleSidebar={()=>{}} onCreateNew={()=>{}} />
  </div>;
}
window.pinSnapshot=()=>({requests:[...requests], enters:[...enters]});
ReactDOM.createRoot(document.getElementById('root')).render(<Harness/>);
"""
compiled = subprocess.run(
    ["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
    input=source + harness, encoding="utf-8", capture_output=True, cwd=repo, check=True,
).stdout
fixture = out / "sidebar-pin-fixture.html"
fixture.write_text(
    '<!doctype html><html lang="zh-TW"><meta charset="utf-8"><style>' + styles +
    '</style><body class="ra-body"><main id="root"></main></body></html>',
    encoding="utf-8",
)


def boxes_overlap(a, b):
    if not a or not b:
        return False
    ox = min(a["x"] + a["w"], b["x"] + b["w"]) - max(a["x"], b["x"])
    oy = min(a["y"] + a["h"], b["y"] + b["h"]) - max(a["y"], b["y"])
    return ox > 0 and oy > 0


def main():
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="msedge", headless=True)
        page = browser.new_page(viewport={"width": 1100, "height": 800})
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.route("https://**", lambda route: route.abort())
        page.route("http://**", lambda route: route.abort())
        page.goto(fixture.as_uri())
        page.add_script_tag(path=str(root / "js/vendor/tailwind.js"))
        page.add_script_tag(content=config)
        for name in ["react.production.min.js", "react-dom.production.min.js"]:
            page.add_script_tag(path=str(root / "js/vendor" / name))
        page.add_script_tag(content=compiled)
        card_a = page.locator(".ra-session-card").filter(has_text="會話A")
        card_b = page.locator(".ra-session-card").filter(has_text="會話B")
        expect(card_a).to_be_visible()
        marker = card_a.locator('[title="已釘選"]')
        page.screenshot(path=str(out / f"{phase}-pinned.png"))
        expect(marker).to_have_count(1)
        assert marker.inner_text().strip() == "📌", marker.inner_text()
        assert "釘選" not in marker.inner_text()
        expect(marker).to_have_attribute("title", "已釘選")
        description = marker.get_attribute("aria-description") or ""
        accessible = marker.evaluate(
            """el => {
              const node = el.ownerDocument.defaultView.getComputedStyle(el);
              return {description: el.getAttribute('aria-description') || '', title: el.title || '', hidden: node.display};
            }"""
        )
        ax = page.evaluate(
            """() => {
              const el = document.querySelector('.ra-session-card [title="已釘選"]');
              if (!el) return null;
              const ax = el.computedAccessibleNode ? null : null;
              return {aria: el.getAttribute('aria-description'), title: el.getAttribute('title'), text: el.textContent};
            }"""
        )
        # 可存取名稱或描述必須是字面「已釘選」，不從產品 helper 推導。
        acc = page.locator('.ra-session-card [title="已釘選"]')
        named = False
        try:
            expect(acc).to_have_accessible_description("已釘選")
            named = True
        except AssertionError:
            expect(acc).to_have_accessible_name("已釘選")
            named = True
        assert named and (description == "已釘選" or accessible["title"] == "已釘選")
        expect(card_b.locator('[title="已釘選"]')).to_have_count(0)
        evidence = page.evaluate(
            """() => {
              const card = [...document.querySelectorAll('.ra-session-card')].find(el => el.textContent.includes('會話A'));
              const box = (el) => {
                if (!el) return null;
                const r = el.getBoundingClientRect();
                return {x:r.x,y:r.y,w:r.width,h:r.height};
              };
              const pin = card.querySelector('[title="已釘選"]');
              const running = card.querySelector('.ra-status-dot.running');
              const unread = [...card.querySelectorAll('span')].find(el => el.textContent.trim() === '3');
              const agent = card.querySelector('span[class*="w-[22px]"]');
              return {
                active: card.className.includes('active'),
                pin: box(pin), running: box(running), unread: box(unread), agent: box(agent),
                groups: [...document.querySelectorAll('[class~="group/dir"] button')].map(el => el.innerText.replace(/\\s+/g,' ').trim()),
              };
            }"""
        )
        assert evidence["active"] is True
        assert evidence["running"] and evidence["unread"] and evidence["pin"] and evidence["agent"]
        assert evidence["unread"]["w"] > 0 and "3" in card_a.inner_text()
        for left, right, label in (
            ("pin", "running", "pin/running"),
            ("pin", "unread", "pin/unread"),
            ("pin", "agent", "pin/agent"),
            ("running", "unread", "running/unread"),
        ):
            assert not boxes_overlap(evidence[left], evidence[right]), (label, evidence)
        assert any("釘選" in name for name in evidence["groups"])
        assert any("D:/work" in name or "work" in name for name in evidence["groups"])
        marker.click()
        snap = page.evaluate("() => pinSnapshot()")
        assert snap["enters"] == ["A"], snap
        assert all(not item.startswith("PATCH") for item in snap["requests"]), snap["requests"]
        expect(card_a.locator('[title="已釘選"]')).to_have_text("📌")
        expect(card_a).to_have_class(re.compile(r"\bactive\b"))
        page.locator('button[aria-label="更多操作：會話A"]').click()
        page.get_by_role("menuitem", name="取消釘選").click()
        expect(page.locator('[title="已釘選"]')).to_have_count(0)
        after = page.evaluate(
            """() => ({
              groups: [...document.querySelectorAll('.space-y-0')].map(el => el.innerText.replace(/\\s+/g,' ').trim()),
              requests: pinSnapshot().requests,
            })"""
        )
        assert len(after["groups"]) == 1, after["groups"]
        assert "會話A" in after["groups"][0] and "會話B" in after["groups"][0]
        assert not after["groups"][0].startswith("釘選") and "取消釘選" not in after["groups"][0]
        assert any("PATCH /sessions/A" in item and '"pinned":false' in item.replace(" ", "") for item in after["requests"]), after["requests"]
        assert not errors, errors
        page.screenshot(path=str(out / f"{phase}-unpinned.png"))
        (out / f"{phase}-evidence.json").write_text(
            json.dumps({"before": evidence, "after": after, "ax": ax}, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        browser.close()


if __name__ == "__main__":
    main()

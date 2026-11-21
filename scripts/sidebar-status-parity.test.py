"""分頁與收合側欄共用同一 session.status。不啟服務、不打外網。

執行：python scripts/sidebar-status-parity.test.py
"""
import json
import os
import re
import subprocess
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
root = repo / "internal/static"
out = Path(os.environ.get("SIDEBAR_STATUS_ARTIFACTS", r"D:/Temp/miniapp-next-four-20261121/sidebar-status"))
phase = os.environ.get("SIDEBAR_STATUS_PHASE", "run")
out.mkdir(parents=True, exist_ok=True)
html = (root / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", html, re.S))
config = re.search(r"<script>\s*(tailwind.config = .*?)</script>", html, re.S)[1]
source = "\n".join((root / "js" / name).read_text(encoding="utf-8") for name in [
    "hooks/messageRows.js", "ui/uiAtoms.js", "ui/ContextMenu.js", "chat/MessageActions.js",
    "ui/SessionTabs.js", "session/SessionView.js",
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
const timerDelays=[];
const originalSetInterval=window.setInterval;
window.setInterval=(fn,ms,...rest)=>{timerDelays.push(ms);return originalSetInterval(fn,ms,...rest)};
const requests=[];
const enters=[];
let rows=[
  {id:'A',name:'會話A',status:'running',unread_count:3,agent_type:'claude',work_dir:'D:/work'},
  {id:'B',name:'會話B',status:'idle',unread_count:0,agent_type:'claude',work_dir:'D:/work'},
];
async function apiFetch(url, opts={}){
  requests.push((opts.method||'GET')+' '+url);
  if (url==='/sessions') return {ok:true, json:async()=>rows.map(s=>({...s}))};
  return {ok:false, status:404, json:async()=>({}), text:async()=>'unexpected'};
}
function Harness(){
  const [list,setList]=useState(rows.map(s=>({...s})));
  const [active,setActive]=useState(null);
  window.applyRows=(next)=>{rows=next.map(s=>({...s})); setList(rows.map(s=>({...s}))); window.dispatchEvent(new Event('miniapp:sessions-refresh'));};
  window.sidebarSnapshot=()=>({requests:[...requests], timers:[...timerDelays], enters:[...enters], active});
  return <div style={{display:'flex',height:'100vh',background:'#111'}}>
    <div style={{width:88}}><SessionView collapsed activeSessionId={active} isWideScreen
      onEnter={(s)=>{enters.push(s.id); setActive(s.id);}} onToggleSidebar={()=>{}} onCreateNew={()=>{}} /></div>
    <div style={{flex:1,minWidth:0}}><SessionTabBar tabs={['A','B']} activeTab={active} allSessions={list} isWideScreen
      onSelectTab={(id)=>{enters.push('tab:'+id); setActive(id);}} /></div>
  </div>;
}
ReactDOM.createRoot(document.getElementById('root')).render(<Harness/>);
"""
compiled = subprocess.run(
    ["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
    input=source + harness, encoding="utf-8", capture_output=True, cwd=repo, check=True,
).stdout
fixture = out / "sidebar-status-fixture.html"
fixture.write_text(
    '<!doctype html><html lang="zh-TW"><meta charset="utf-8"><style>' + styles +
    '</style><body class="ra-body"><main id="root"></main></body></html>',
    encoding="utf-8",
)

LABELS = {
    "running": "執行中",
    "awaiting_confirm": "待授權",
    "awaiting_input": "待回答",
    "awaiting_shell_confirm": "待確認 Shell",
}


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
        side = page.locator('button[aria-label="會話A"]')
        expect(side).to_be_visible()
        expect(page.locator('[data-session-tab-id="A"] .ra-status-dot.running')).to_be_visible()
        page.screenshot(path=str(out / f"{phase}-running.png"))
        # 反例：同一筆 running，分頁有圓點、收合圖示沒有。
        assert page.locator('button[aria-label="會話A"] .ra-status-dot.running').count() == 1, "收合圖示未顯示 running"
        evidence = []

        def apply(status):
            before = page.evaluate("() => sidebarSnapshot().enters.slice()")
            page.evaluate("(status) => applyRows(["
                          "{id:'A',name:'會話A',status,unread_count:3,agent_type:'claude',work_dir:'D:/work'},"
                          "{id:'B',name:'會話B',status:'idle',unread_count:0,agent_type:'claude',work_dir:'D:/work'}"
                          "])", status)
            return before

        def measure(status):
            return page.evaluate("""(status) => {
              const button = document.querySelector('button[aria-label="會話A"]');
              const other = document.querySelector('button[aria-label="會話B"]');
              const tab = document.querySelector('[data-session-tab-id="A"]');
              const tabB = document.querySelector('[data-session-tab-id="B"]');
              const box = (el) => {
                if (!el) return null;
                const r = el.getBoundingClientRect();
                const cs = getComputedStyle(el);
                return {x:r.x,y:r.y,w:r.width,h:r.height,cssW:cs.width,cssH:cs.height,anim:cs.animationName,dur:cs.animationDuration,cls:el.className};
              };
              const pick = (root, sel) => root ? root.querySelector(sel) : null;
              const unreadEl = [...(button ? button.querySelectorAll('span') : [])].find(el => el.textContent.trim() === '3') || null;
              const mark = pick(button, status === 'running' ? '.ra-status-dot.running' : '.ra-status-ask');
              const hit = mark && button ? document.elementFromPoint(
                Math.min(mark.getBoundingClientRect().right, button.getBoundingClientRect().right) - 1,
                Math.min(mark.getBoundingClientRect().bottom, button.getBoundingClientRect().bottom) - 1
              ) : null;
              return {
                side: box(mark),
                tab: box(pick(tab, status === 'running' ? '.ra-status-dot.running' : '.ra-status-ask')),
                unread: box(unreadEl),
                button: box(button),
                buttonAnim: getComputedStyle(button).animationName,
                description: button.getAttribute('aria-description'),
                otherAsk: !!pick(other, '.ra-status-ask'),
                otherRun: !!pick(other, '.ra-status-dot.running'),
                tabBRun: !!pick(tabB, '.ra-status-dot.running'),
                tabBAsk: !!pick(tabB, '.ra-status-ask'),
                hitButton: !!(hit && hit.closest('button[aria-label="會話A"]')),
                hitIsMark: hit === mark,
                order: [...document.querySelectorAll('button[aria-label^="會話"]')].map(el => el.getAttribute('aria-label')),
              };
            }""", "running" if status.lower() == "running" else status)

        def expect_pair(status):
            label = LABELS.get(status.lower(), "")
            busy = status.lower() == "running" or status.lower() in LABELS and status.lower() != "running"
            running = status.lower() == "running"
            if running:
                expect(page.locator('button[aria-label="會話A"] .ra-status-dot.running')).to_be_visible()
                expect(page.locator('[data-session-tab-id="A"] .ra-status-dot.running')).to_be_visible()
                expect(page.locator('button[aria-label="會話A"] .ra-status-ask')).to_have_count(0)
            elif label:
                expect(page.locator('button[aria-label="會話A"] .ra-status-ask')).to_be_visible()
                expect(page.locator('[data-session-tab-id="A"] .ra-status-ask')).to_be_visible()
                expect(page.locator('button[aria-label="會話A"] .ra-status-dot.running')).to_have_count(0)
                expect(page.locator('[data-session-tab-id="A"] .ra-status-dot.running')).to_have_count(0)
            else:
                expect(page.locator('button[aria-label="會話A"] .ra-status-dot.running, button[aria-label="會話A"] .ra-status-ask')).to_have_count(0)
                expect(page.locator('[data-session-tab-id="A"] .ra-status-dot.running, [data-session-tab-id="A"] .ra-status-ask')).to_have_count(0)
            expect(page.locator('button[aria-label="會話B"] .ra-status-dot, button[aria-label="會話B"] .ra-status-ask')).to_have_count(0)
            expect(page.locator('[data-session-tab-id="B"] .ra-status-dot, [data-session-tab-id="B"] .ra-status-ask')).to_have_count(0)
            expect(page.locator('button[aria-label="會話A"]').locator("text=3")).to_be_visible()
            expect(page.locator('[data-session-tab-id="A"] .ra-unread-badge')).to_have_text("3")
            if label:
                expect(side).to_have_attribute("aria-description", label)
            else:
                expect(side).not_to_have_attribute("aria-description", re.compile(r".+"))
            info = measure("running" if running else status)
            evidence.append({"status": status, **info})
            assert info["otherRun"] is False and info["otherAsk"] is False and info["tabBRun"] is False and info["tabBAsk"] is False
            if info["unread"] and info["side"]:
                a, b = info["unread"], info["side"]
                overlap_x = min(a["x"] + a["w"], b["x"] + b["w"]) - max(a["x"], b["x"])
                overlap_y = min(a["y"] + a["h"], b["y"] + b["h"]) - max(a["y"], b["y"])
                assert overlap_x <= 0 or overlap_y <= 0, (a, b)
            assert info["order"] == ["會話A", "會話B"]
            assert info["buttonAnim"] in ("none", "")
            if running:
                assert info["side"]["cssW"] == "7px" and info["side"]["cssH"] == "7px", info["side"]
                assert info["tab"]["cssW"] == "7px" and info["tab"]["cssH"] == "7px"
                assert "pulse-dot" in info["side"]["anim"] and info["side"]["dur"].startswith("1.2s")
                assert info["hitButton"] and info["hitIsMark"] is False
                assert info["side"]["x"] + info["side"]["w"] / 2 < info["button"]["x"] + info["button"]["w"] / 2
            elif label:
                assert info["side"]["cssW"] == "16px" and info["side"]["cssH"] == "16px", info["side"]
                assert info["hitButton"] and info["hitIsMark"] is False
            return busy

        try:
            seen = page.evaluate("() => sidebarSnapshot().enters.slice()")
            assert seen == []
            expect_pair("running")
            for status in ["idle", "running", "awaiting_confirm", "running", "awaiting_input", "running",
                           "awaiting_shell_confirm", "running", "idle", "pending", "RUNNING", "idle"]:
                seen = apply(status)
                assert seen == [], status
                expect_pair(status)
            snap = page.evaluate("() => sidebarSnapshot()")
            assert all(item == "GET /sessions" for item in snap["requests"]), snap["requests"]
            assert snap["timers"] == [30000], snap["timers"]
            assert snap["enters"] == [] and snap["active"] is None
            side.click()
            snap = page.evaluate("() => sidebarSnapshot()")
            assert snap["enters"] == ["A"] and snap["active"] == "A"
            expect(page.locator('button[aria-label="會話A"]').locator("text=3")).to_be_visible()
            expect(page.locator('button[aria-label="會話A"]')).to_have_class(re.compile(r"ring-violet-500"))
            expect(page.locator('button[aria-label="會話B"]')).not_to_have_class(re.compile(r"ring-violet-500"))
            assert not errors, errors
            page.screenshot(path=str(out / f"{phase}-idle-after-click.png"))
        finally:
            (out / f"{phase}-measure.json").write_text(json.dumps(evidence, ensure_ascii=False, indent=2), encoding="utf-8")
            browser.close()


if __name__ == "__main__":
    main()

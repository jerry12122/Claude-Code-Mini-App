"""Exercise the real tab components in Edge: python scripts/tab-pinning.test.py."""
import re
import subprocess
import tempfile
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
root = repo / "internal/static"
html = (root / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", html, re.S))
config = re.search(r"<script>\s*(tailwind.config = .*?)</script>", html, re.S)[1]
source = "\n".join((root / "js" / f).read_text(encoding="utf-8") for f in [
    "hooks/tabGroups.js", "ui/ContextMenu.js", "chat/MessageActions.js", "ui/SessionTabs.js",
])
harness = r"""
const {useState,useEffect,useLayoutEffect,useRef,useCallback,useMemo}=React;
const AGENT_LABEL={claude:'Claude'};
function sessionStatusDotClass(){return ''} function sessionStatusLabel(){return ''}
function sessionDisplayAgent(){return 'claude'} function getAgentBadgeClass(){return ''}
function AgentBadgeIcon(){return <span aria-hidden="true">C</span>} function hapticTap(){}
window.sessions=Array.from({length:16},(_,i)=>({id:String.fromCharCode(65+i),name:'會話 '+String.fromCharCode(65+i),status:'idle'}));
const initial={...createDefaultTabGroupsState(),groups:[{tabs:['A','B','C'],active:'A'}]};
function Harness(){
 const [state,dispatch]=React.useReducer(tabGroupsReducer,undefined,()=>localStorage.getItem('pin-test')?deserializeTabGroupsState(localStorage.getItem('pin-test')):initial);
 window.tabState=state;window.dispatch=action=>ReactDOM.flushSync(()=>dispatch(action));
 useEffect(()=>localStorage.setItem('pin-test',serializeTabGroupsState(state)),[state]);
 return <div style={{width:'100%',height:200,display:'flex'}}>{state.groups.map((group,i)=><div key={i} style={{width:state.groups.length===1?'100%':'50%'}}>
 <SessionGroupFrame groupIndex={i} group={group} pinnedTabs={state.pinnedTabs} allSessions={sessions}
   groupCount={state.groups.length} isFocused={state.focused===i}
   onFocusGroup={()=>dispatch({type:'focus',groupIndex:i})}
   onSelectTab={sessionId=>dispatch({type:'open',sessionId,groupIndex:i})}
   onCloseTab={sessionId=>dispatch({type:'close',sessionId,groupIndex:i})}
   onTogglePin={sessionId=>dispatch({type:state.pinnedTabs.includes(sessionId)?'unpin':'pin',sessionId})}
   onDropOnTabBar={(sessionId,from,index)=>dispatch(resolveDropAction(state,sessionId,{kind:'tabbar',group:i,index}))}
   onDropOnContent={(sessionId,from,region)=>dispatch(resolveDropAction(state,sessionId,{kind:'content',group:i,region}))}
   onMoveTab={sessionId=>dispatch(state.groups.length===1?{type:'splitDrop',sessionId,edge:'right'}:{type:'move',sessionId,toGroup:1-i})}>
   <div role="tabpanel">{group.active}</div>
 </SessionGroupFrame></div>)}</div>
}
ReactDOM.createRoot(document.getElementById('root')).render(<Harness/>);
"""
compiled = subprocess.run(["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
                          input=source + harness, encoding="utf-8", capture_output=True, cwd=repo, check=True).stdout
(repo / "poc").mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(prefix="pinning-", dir=repo / "poc") as tmp, sync_playwright() as p:
    fixture = Path(tmp) / "tabs.html"
    fixture.write_text('<!doctype html><html lang="zh-TW"><meta charset="utf-8"><style>' + styles +
                       '</style><body class="ra-body"><main id="root"></main></body></html>', encoding="utf-8")
    browser = p.chromium.launch(channel="msedge", headless=True)
    page = browser.new_page(viewport={"width": 1280, "height": 800})
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.route("https://**", lambda route: route.abort())

    def boot():
        page.goto(fixture.as_uri())
        page.add_script_tag(path=str(root / "js/vendor/tailwind.js"))
        page.add_script_tag(content=config)
        for name in ["react.production.min.js", "react-dom.production.min.js"]:
            page.add_script_tag(path=str(root / "js/vendor" / name))
        page.add_script_tag(content=compiled)
        expect(page.get_by_role("tablist").first).to_be_visible()

    def tab(id):
        return page.locator(f'[data-session-tab-id="{id}"] [role="tab"]')

    def menu(id, action, keyboard=False):
        if keyboard:
            tab(id).scroll_into_view_if_needed()
            tab(id).focus()
            expect(tab(id)).to_be_focused()
            # Drain the focus/scroll rendering turn before opening a menu that closes on ancestor scroll.
            tab(id).evaluate("el => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")
            tab(id).press("Shift+F10")
        else:
            tab(id).click(button="right")
        item = page.get_by_role("menuitem", name=action, exact=True)
        expect(item).to_be_visible()
        expect(item).to_be_enabled()
        item.click()

    def state():
        return page.evaluate("tabState")

    try:
        boot()
        menu("C", "釘選分頁", keyboard=True)
        expect(tab("C")).to_be_focused()
        assert state()["groups"][0] == {"tabs": ["C", "A", "B"], "active": "A"}
        expect(tab("C")).to_have_attribute("aria-description", "已釘選；請先取消釘選再關閉")
        tab("C").press("Delete")
        tab("C").click(button="middle")
        assert state()["pinnedTabs"] == ["C"]
        tab("C").click(button="right")
        expect(page.get_by_role("menuitem", name="關閉分頁", exact=False)).to_be_disabled()
        page.keyboard.press("Escape")
        menu("B", "釘選分頁")
        assert state()["groups"][0]["tabs"] == ["C", "B", "A"]
        menu("C", "關閉其他分頁")
        expect(page.get_by_role("tab", selected=True)).to_be_focused()
        assert state()["groups"][0]["tabs"] == ["C", "B"]
        page.evaluate("dispatch({type:'open',sessionId:'A'})")
        tab("B").press("ArrowRight")
        expect(tab("A")).to_be_focused()
        menu("C", "移到另一個窗格")
        assert len(state()["groups"]) == 2 and state()["pinnedTabs"] == ["C", "B"]
        assert len(page.get_by_role("tabpanel").all()) == 2
        page.evaluate("dispatch({type:'toggleSplit'})")
        assert state()["groups"][0]["tabs"] == ["B", "C", "A"]
        menu("B", "關閉右側分頁")
        expect(page.get_by_role("tab", selected=True)).to_be_focused()
        assert state()["groups"][0]["tabs"] == ["B", "C"]
        page.evaluate("dispatch({type:'open',sessionId:'A'})")
        saved = state()
        boot()
        assert state() == saved, "real component state persists across reloads"

        # Overflow stays in one row, each zone scrolls independently, and keyboard reaches every tab.
        page.set_viewport_size({"width": 640, "height": 800})
        page.evaluate("sessions.slice(3).forEach(s=>dispatch({type:'open',sessionId:s.id}));sessions.slice(3,8).forEach(s=>dispatch({type:'pin',sessionId:s.id}))")
        pin_zone = page.locator('[data-tab-zone="pinned"]')
        regular_zone = page.locator('[data-tab-zone="regular"]')
        expect(pin_zone).to_be_visible()
        assert pin_zone.bounding_box()["width"] <= 320
        assert pin_zone.evaluate("el=>el.scrollWidth>el.clientWidth")
        assert regular_zone.evaluate("el=>el.scrollWidth>el.clientWidth")
        before = pin_zone.evaluate("el=>el.scrollLeft")
        regular_zone.dispatch_event("wheel", {"deltaY": 160})
        assert regular_zone.evaluate("el=>el.scrollLeft") > 0
        assert pin_zone.evaluate("el=>el.scrollLeft") == before
        assert pin_zone.bounding_box()["y"] == regular_zone.bounding_box()["y"]
        page.screenshot(path=str(repo / "poc/pinning-preview.png"))
        tab("A").focus()
        tab("A").press("Home")
        expect(tab("B")).to_be_focused()
        tab("B").press("End")
        expect(tab("P")).to_be_focused()
        assert page.evaluate("scrollY") == 0

        # Native drag payload invokes the real drop handler and must clamp to the pin boundary.
        page.evaluate("""() => {
          const bar=document.querySelector('[role=tablist]'), data=new DataTransfer();
          data.setData('application/x-miniapp-session',JSON.stringify({sessionId:'P',fromGroup:0}));
          window.__miniapp_drag_source={sessionId:'P',fromGroup:0};
          bar.dispatchEvent(new DragEvent('dragover',{bubbles:true,cancelable:true,dataTransfer:data,clientX:0}));
          bar.dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:data,clientX:0}));
        }""")
        current = state()
        assert current["groups"][0]["tabs"][len(current["pinnedTabs"])] == "P"
        # The inverse boundary also holds when a pin is dragged over the regular zone.
        page.evaluate("""() => {
          const bar=document.querySelector('[role=tablist]'), data=new DataTransfer();
          data.setData('application/x-miniapp-session',JSON.stringify({sessionId:'B',fromGroup:0}));
          window.__miniapp_drag_source={sessionId:'B',fromGroup:0};
          bar.dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:data,clientX:1000}));
        }""")
        assert state()["groups"][0]["tabs"][len(state()["pinnedTabs"]) - 1] == "B"
        # Idle cleanup closes regular tabs while preserving every pin, with no phantom enabled items.
        menu("B", "關閉閒置分頁")
        assert set(state()["groups"][0]["tabs"]) == set(state()["pinnedTabs"])
        expect(page.get_by_role("tab", selected=True)).to_be_focused()
        tab("B").click(button="right")
        for action in ["關閉其他分頁", "關閉右側分頁"]:
            expect(page.get_by_role("menuitem", name=action, exact=True)).to_be_disabled()
        page.keyboard.press("Escape")
        pin_zone = page.locator('[data-tab-zone="pinned"]')
        assert pin_zone.bounding_box()["width"] > 320, "all-pinned can use the full row"
        tab("B").evaluate("el=>el.parentElement.querySelector('[title=取消釘選分頁]').click()")
        expect(tab("B")).to_be_focused()
        assert "B" not in state()["pinnedTabs"]
        tab("B").press("Delete")
        expect(tab("B")).to_have_count(0)
        # Removal commits before the close handler's requestAnimationFrame restores selection focus.
        expect(page.get_by_role("tab", selected=True)).to_be_focused()
        menu("C", "取消釘選分頁", keyboard=True)
        expect(tab("C")).to_be_focused()
        for scheme in ["light", "dark"]:
            page.emulate_media(color_scheme=scheme, reduced_motion="reduce", forced_colors="active")
            expect(page.get_by_role("tab", selected=True)).to_be_visible()
        assert not errors, errors
        print("tab pinning browser checks passed")
    finally:
        browser.close()

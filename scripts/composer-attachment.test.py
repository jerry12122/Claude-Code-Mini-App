"""Actual ChatView integration checks; Python + Playwright + Edge + Go, no npm."""
import re
import subprocess
import tempfile
from pathlib import Path

from playwright.sync_api import sync_playwright, expect

repo = Path(__file__).resolve().parents[1]
root = repo / "internal/static"
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", (root / "index.html").read_text(encoding="utf-8"), re.S))
tailwind_config = re.search(r"<script>\s*(tailwind.config = .*?)</script>", (root / "index.html").read_text(encoding="utf-8"), re.S)[1]
files = ["hooks/tabGroups.js", "ui/SplitLayout.js", "ui/ui-atoms.js", "hooks/useChatComposer.js", "chat/AttachmentCards.js", "chat/chat-header.js", "chat/ChatView.js"]
if (root / "js/chat/ComposerTools.js").exists():
    files.insert(-1, "chat/ComposerTools.js")
source = "\n".join((root / "js" / f).read_text(encoding="utf-8") for f in files)
compiled = subprocess.run(["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
                          input=source, encoding="utf-8", capture_output=True, cwd=repo, check=True).stdout
setup = r"""
const {useState,useRef,useCallback,useMemo,useEffect,useLayoutEffect}=React;
const isGuestMode=false, AGENT_LABEL={claude:'Claude',codex:'Codex',cursor:'Cursor',telegram:'Telegram'};
const SHARE_TOKEN_FROM_URL='';
const SESSION_STATE_LABEL={IDLE:'閒置',THINKING:'思考中'};
function sessionStateChipClass(){return ''}
function readGuestAuth(){return null}
function useMediaQuery(query){const [v,s]=useState(()=>matchMedia(query).matches);useEffect(()=>{const m=matchMedia(query),f=()=>s(m.matches);m.addEventListener('change',f);f();return()=>m.removeEventListener('change',f)},[query]);return v}
function useShareRemaining(){return null}
function useLongPress(){return ()=>({})}
function readComposerFiles(){return Promise.resolve([])}
function writeComposerFiles(){return Promise.resolve()}
function restoreComposerFiles(a){return a}
function socketPayloadError(){return ''}
function expandMentionPrompt(text){return text}
function workDirGroupShortLabel(){return 'repo'}
function parseMarkdown(x){return x}
function filterMentionSessions(){return []}
function mentionQueryAtCursor(){return null}
const ShareStatusBar=()=>null,ForwardModal=()=>null,
MessageActionsHost=()=>null,SelectionQuotePopover=()=>null,MentionChips=()=>null,
GuestReadOnlyBar=()=>null,MentionMenu=()=>null,VirtualMessages=()=>null;
window.wsMessages=[];window.uploads=[];window.holdUploads=false;window.modelFail=false;
window.uploadReplies=[];window.failNames=[];
function apiFetch(url,opts){
 if(url.startsWith('/model-options/')) return window.modelFail?Promise.reject(Error('offline')):Promise.resolve({ok:true,json:async()=>[{model_id:'sonnet',label:'Sonnet'},{model_id:'opus',label:'Opus'}]});
 if(url.endsWith('/uploads')){
  const file=opts.body.get('file'); window.uploads.push({url,name:file.name});
  const reply=()=>({ok:!window.failNames.includes(file.name),json:async()=>window.failNames.includes(file.name)?{error:'上傳失敗'}:{attachment:{id:'id-'+file.name,name:file.name,size:file.size}}});
  return window.holdUploads?new Promise(resolve=>window.uploadReplies.push(()=>resolve(reply()))):Promise.resolve(reply());
 }
 return Promise.resolve({ok:true,json:async()=>[]});
}
const fixtureMessages=[];
window.config={state:'IDLE',connected:true,inputMode:'agent',modelSel:'',mode:'default',effortSel:''};
function useChatSocket({agentType,showPermModeSelect,showEffortSelect,guest}){
 const [inputMode,setInputMode]=useState(config.inputMode),[mode,setMode]=useState(config.mode),
 [modelSel,setModelSel]=useState(config.modelSel),[effortSel,setEffortSel]=useState(config.effortSel);
 const isGuest=!!guest,serverPermAppliedRef=useRef('default'),serverInputAppliedRef=useRef('agent'),
 serverModelAppliedRef=useRef(''),serverEffortAppliedRef=useRef('');
 const send=useCallback(x=>{wsMessages.push(x);return config.connected},[]);
 // ACTUAL_FLUSH
 return {...config,messages:fixtureMessages,permTools:[],setPermTools:()=>{},permRetryTools:[],question:null,
 inputMode,setInputMode,mode,setMode,modelSel,setModelSel,effortSel,setEffortSel,
 histLoaded:false,tgPaging:{},historyPaging:{},loadOlder:()=>{},queue:[],queuePaused:false,
 send,submitInput:async (data,attachment_ids,request_id)=>{wsMessages.push({type:'input',data,attachment_ids,request_id});return {}},
 flushPendingModes,handleQuotaRefresh:()=>{},commitPermMode:()=>true};
}
window.tgEvents=new Set();window.tgVisible=false;
window.Telegram={WebApp:{isVersionAtLeast:()=>true,BackButton:{get isVisible(){return tgVisible},show(){tgVisible=true},hide(){tgVisible=false},onClick(f){tgEvents.add(f)},offClick(f){tgEvents.delete(f)}}}};
window.props={session:{id:'one',agent_type:'claude',work_dir:'repo'}};
window.renderChat=(patch={})=>{Object.assign(props,patch);ReactDOM.flushSync(()=>appRoot.render(React.createElement(ChatView,props)))};
window.appRoot=ReactDOM.createRoot(document.getElementById('root'));
"""
socket_source = (root / 'js/hooks/useChatSocket.js').read_text(encoding='utf-8')
flush = socket_source[socket_source.index('  const flushPendingModes ='):socket_source.index('  const handleQuotaRefresh =')]
setup = setup.replace('// ACTUAL_FLUSH', flush)
file = {"name": "attachment.txt", "mimeType": "text/plain", "buffer": b"hello"}
browser_errors = []
screenshot_dir = repo / '.spectra/design-cache/mobile-composer-sheet'
screenshot_dir.mkdir(parents=True, exist_ok=True)

with tempfile.TemporaryDirectory(prefix="composer-check-") as tmp, sync_playwright() as p:
    fixture = Path(tmp) / "composer.html"
    fixture.write_text('<!doctype html><html lang="zh-TW"><meta charset="utf-8"><style>' + styles + '</style><body class="h-app ra-body"><div id="root"></div></body></html>', encoding="utf-8")
    browser = p.chromium.launch(channel="msedge", headless=True)
    try:
        def load(width=1280, height=900, touch=False, config=None, props=None, writable=True):
            page = browser.new_page(viewport={"width": width, "height": height}, has_touch=touch)
            page.route('https://**', lambda route: route.abort())
            page.on("pageerror", lambda error: browser_errors.append(str(error)))
            page.goto(fixture.as_uri())
            page.add_script_tag(path=str(root / "js/vendor/tailwind.js"))
            page.add_script_tag(content=tailwind_config)
            for vendor in ["react.production.min.js", "react-dom.production.min.js"]:
                page.add_script_tag(path=str(root / "js/vendor" / vendor))
            page.add_script_tag(content=setup)
            if config:
                page.evaluate("p=>{Object.assign(window.config,p);window.modelFail=!!p.modelFail}", config)
            page.add_script_tag(content=compiled)
            page.evaluate("p=>renderChat(p||{})", props)
            if writable:
                expect(page.locator("textarea[data-chat-input]")).to_be_enabled()
            return page

        page = load()
        field = page.locator("textarea[data-chat-input]")
        field.fill("草稿保留")
        picker = page.locator('input[type="file"]')
        expect(picker).to_have_count(1)
        assert not picker.evaluate("el=>!!el.closest('.ra-composer-tools')"), "picker must live outside tools"
        page.evaluate("window.originalPicker=document.querySelector('input[type=file]')")
        for select in ["chat-model", "chat-effort", "chat-perm-mode"]:
            expect(page.locator("#" + select)).to_have_count(1)
        expect(page.locator('.ra-chat-header select')).to_have_count(0)
        assert page.locator('.ra-composer-tools').evaluate("el=>Array.from(el.querySelectorAll('button,select')).map(x=>x.getAttribute('aria-label'))") == ['AI 代理模式，點擊切換至 Shell', '附加圖片或檔案', '權限模式', 'Model', 'Effort']
        attach = page.get_by_role("button", name="附加圖片或檔案", exact=True)
        for files_to_set in [[], [file], [file]]:
            with page.expect_file_chooser() as chosen:
                attach.click()
            chosen.value.set_files(files_to_set)
        expect(page.get_by_role("button", name="移除附件 attachment.txt", exact=True)).to_have_count(2)
        expect(field).to_have_value("草稿保留")
        assert picker.evaluate("el=>el===originalPicker && el.value===''")
        assert page.evaluate("wsMessages.length") == 0
        page.close()
        # Picker cancellation, accepted placeholders, serial upload failure and retry.
        page = load(390, 844, True)
        field = page.locator('textarea[data-chat-input]')
        field.fill('附件草稿')
        plus = page.get_by_role('button', name='聊天工具', exact=True)
        plus.click()
        attach = page.get_by_role('button', name='附加圖片或檔案', exact=True)
        with page.expect_file_chooser() as chosen:
            attach.click()
        chosen.value.set_files([])
        page.locator('input[type=file]').dispatch_event('cancel')
        expect(page.locator('dialog[open]')).to_have_count(1)
        page.evaluate("holdUploads=true;failNames=['second.txt']")
        with page.expect_file_chooser() as chosen:
            attach.click()
        chosen.value.set_files([file, {**file, 'name': 'second.txt'}])
        expect(page.locator('dialog[open]')).to_have_count(0)
        expect(page.get_by_role('button', name='移除附件 attachment.txt', exact=True)).to_be_visible()
        expect(page.get_by_role('button', name='移除附件 second.txt', exact=True)).to_be_visible()
        expect(plus).to_be_focused()
        assert page.evaluate('uploads.map(x=>x.name)') == ['attachment.txt']
        page.evaluate('uploadReplies.shift()()')
        page.wait_for_function('uploads.length===2')
        page.evaluate('uploadReplies.shift()()')
        expect(page.get_by_role('button', name='重試', exact=True)).to_be_visible()
        page.evaluate('holdUploads=false;failNames=[]')
        page.get_by_role('button', name='重試', exact=True).click()
        expect(page.get_by_role('button', name='重試', exact=True)).to_have_count(0)
        expect(field).to_have_value('附件草稿')
        assert page.evaluate('wsMessages.length') == 0
        assert not field.evaluate('el=>document.activeElement===el')
        page.get_by_role('button', name='移除附件 second.txt', exact=True).click()
        expect(page.get_by_role('button', name='移除附件 second.txt', exact=True)).to_have_count(0)
        page.get_by_role('button', name='移除附件 attachment.txt', exact=True).click()
        page.wait_for_function('!composerToolsBackPending')
        plus.click()
        with page.expect_file_chooser() as chosen:
            attach.click()
        chosen.value.set_files([file])
        expect(page.get_by_role('button', name='移除附件 attachment.txt', exact=True)).to_have_count(1)
        field.focus()
        field.evaluate('el=>el.blur()')
        expect(page.get_by_role('button', name='移除附件 attachment.txt', exact=True)).to_be_visible()
        expect(page.locator('input[type=file]')).to_have_count(1)
        page.close()

        for mutation in ["renderChat({session:{id:'two',agent_type:'claude'}})", "renderChat({guest:{role:'viewer'}})"]:
            page = load(390, 844, True)
            page.get_by_role('button', name='聊天工具', exact=True).click()
            with page.expect_file_chooser() as chosen:
                page.get_by_role('button', name='附加圖片或檔案', exact=True).click()
            page.evaluate(mutation)
            chosen.value.set_files([file])
            assert page.evaluate('uploads.length') == 0, 'stale picker must not upload'
            expect(page.locator('dialog[open]')).to_have_count(0)
            page.close()

        # Modal keyboard scope and owned browser/Telegram back navigation.
        page = load(390, 844, True)
        field = page.locator('textarea[data-chat-input]')
        field.fill('返回保留草稿')
        page.evaluate("window.escaped=0;window.addEventListener('keydown',e=>{if(e.key==='Escape')escaped++});history.replaceState({previous:true},'');history.pushState({existing:42},'')")
        plus = page.get_by_role('button', name='聊天工具', exact=True)
        for turn in range(3):
            plus.click()
            page.wait_for_function('tgEvents.size===1')
            assert page.evaluate('tgVisible && history.state.existing===42')
            count = page.evaluate('history.length')
            plus.evaluate('el=>el.click()')
            assert page.evaluate('history.length') == count
            expect(page.get_by_role('button', name='關閉聊天工具')).to_be_focused()
            field.evaluate('el=>el.focus()')
            assert not field.evaluate('el=>document.activeElement===el'), 'background must be inert'
            for key in ['Tab'] * 8 + ['Shift+Tab'] * 8:
                page.keyboard.press(key)
                assert page.evaluate("document.activeElement===document.body || !!document.activeElement.closest('dialog[open]')")
            page.get_by_role('button', name='思考強度', exact=True).click()
            page.keyboard.press('Escape')
            expect(page.get_by_role('button', name='思考強度', exact=True)).to_be_focused()
            assert page.evaluate('escaped') == 0
            page.get_by_role('button', name='思考強度', exact=True).click()
            page.evaluate('history.back()')
            expect(page.get_by_role('button', name='思考強度', exact=True)).to_be_focused()
            assert page.evaluate('!!history.state.miniappComposerTools')
            page.get_by_role('button', name='模型', exact=True).click()
            page.evaluate('tgEvents.forEach(f=>f())')
            expect(page.get_by_role('button', name='模型', exact=True)).to_be_focused()
            if turn == 0:
                page.keyboard.press('Escape')
            elif turn == 1:
                page.evaluate('history.back()')
            else:
                page.evaluate('tgEvents.forEach(f=>f())')
            expect(page.locator('dialog[open]')).to_have_count(0)
            page.wait_for_function('!composerToolsBackPending && tgEvents.size===0')
            assert page.evaluate('!tgVisible && !history.state?.miniappComposerTools && history.state.existing===42')
            expect(plus).to_be_focused()
            expect(field).to_have_value('返回保留草稿')
            page.evaluate('history.forward()')
            page.wait_for_function('!history.state?.miniappComposerTools && !composerToolsBackPending')
            expect(page.locator('dialog[open]')).to_have_count(0)
        assert page.evaluate('escaped') == 0
        page.evaluate('history.back()')
        page.wait_for_function('history.state?.previous===true')
        page.evaluate('history.forward()')
        page.wait_for_function('history.state?.existing===42')
        page.evaluate('tgVisible=true')
        plus.click()
        page.get_by_role('button', name='關閉聊天工具').click()
        page.wait_for_function('tgEvents.size===0')
        assert page.evaluate('tgVisible'), 'restore preexisting Telegram BackButton visibility'
        page.close()

        # Every obsolete composer releases its dialog and cannot reopen on restoration.
        for mutation, restore in [
            ("renderChat({session:{id:'two',agent_type:'claude'}})", "renderChat({session:{id:'one',agent_type:'claude'}})"),
            ("renderChat({guest:{role:'viewer'}})", "renderChat({guest:null})"),
            ("config.shareEnded=true;renderChat({guest:{role:'editor'}})", "config.shareEnded=false;renderChat({guest:null})"),
            ("config.state='SHELL_EXEC';renderChat()", "config.state='IDLE';renderChat()"),
            ("renderChat({isFocused:false})", "renderChat({isFocused:true})"),
            ("ReactDOM.flushSync(()=>appRoot.render(null))", "renderChat()"),
        ]:
            page = load(390, 844, True)
            page.locator('textarea[data-chat-input]').fill('會話草稿')
            page.get_by_role('button', name='聊天工具', exact=True).click()
            page.evaluate(mutation)
            expect(page.locator('dialog[open]')).to_have_count(0)
            page.wait_for_function('!composerToolsBackPending && tgEvents.size===0')
            assert page.evaluate("!history.state?.miniappComposerTools && getComputedStyle(document.documentElement).overflow!=='hidden'")
            page.evaluate(restore)
            expect(page.locator('dialog[open]')).to_have_count(0)
            expect(page.locator('textarea[data-chat-input]')).to_have_value('會話草稿')
            page.close()

        for width, height in [(390, 844), (844, 390), (320, 480)]:
            page = load(width, height, True)
            page.locator('textarea[data-chat-input]').fill('保留旋轉草稿\n第二行\n第三行')
            plus = page.get_by_role('button', name='聊天工具', exact=True)
            plus.click()
            page.evaluate("Object.defineProperty(visualViewport,'height',{configurable:true,value:300});visualViewport.dispatchEvent(new Event('resize'))")
            assert page.locator('dialog[open]').bounding_box()['height'] <= 211
            expect(page.get_by_role('button', name='關閉聊天工具')).to_be_in_viewport()
            page.evaluate("delete visualViewport.height;visualViewport.dispatchEvent(new Event('resize'))")
            for theme in ['dark', 'light']:
                page.evaluate('theme=>document.documentElement.dataset.theme=theme', theme)
                page.screenshot(path=str(screenshot_dir / f'implementation-{width}-{theme}.png'))
                assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
                assert plus.bounding_box()['width'] >= 44
                close = page.get_by_role('button', name='關閉聊天工具')
                assert close.bounding_box()['width'] >= 44 and close.bounding_box()['height'] >= 44
                page.get_by_role('button', name='權限', exact=True).scroll_into_view_if_needed()
                expect(close).to_be_in_viewport()
                assert page.evaluate("getComputedStyle(document.documentElement).overflow==='hidden'")
                page.get_by_role('button', name='思考強度', exact=True).click()
                page.get_by_role('radio', name='max', exact=True).scroll_into_view_if_needed()
                expect(close).to_be_in_viewport()
                page.get_by_role('button', name='返回聊天工具').click()
            if width == 320:
                page.add_style_tag(content='.ra-tool-row{font-size:28px}.ra-tool-value{font-size:26px}.ra-tools-header h2{font-size:32px}')
                page.get_by_role('button', name='權限', exact=True).scroll_into_view_if_needed()
                expect(page.get_by_role('button', name='關閉聊天工具')).to_be_in_viewport()
                assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
                assert page.locator('.ra-tools-body').evaluate('el=>el.scrollWidth<=el.clientWidth')
            page.set_viewport_size({'width': 844, 'height': 390})
            expect(page.locator('dialog[open]')).to_have_count(1)
            page.set_viewport_size({'width': 844, 'height': 700})
            expect(page.locator('dialog[open]')).to_have_count(1)
            page.get_by_role('button', name='模型', exact=True).click()
            page.get_by_role('radio', name='Opus', exact=True).click()
            page.set_viewport_size({'width': 1280, 'height': 900})
            expect(page.locator('dialog[open]')).to_have_count(0)
            expect(page.locator('#chat-model')).to_have_value('opus')
            expect(page.locator('textarea[data-chat-input]')).to_have_value('保留旋轉草稿\n第二行\n第三行')
            page.wait_for_function('!composerToolsBackPending && tgEvents.size===0')
            assert page.evaluate("!history.state?.miniappComposerTools && getComputedStyle(document.documentElement).overflow!=='hidden'")
            page.set_viewport_size({'width': 390, 'height': 844})
            expect(page.locator('dialog[open]')).to_have_count(0)
            page.close()
        page = load(390, 844, True)
        plus = page.get_by_role("button", name="聊天工具", exact=True)
        expect(plus).to_be_visible()
        plus.click()
        expect(page.get_by_role("dialog", name="聊天工具", exact=True)).to_be_visible()
        page.get_by_role("button", name="模型", exact=True).click()
        page.get_by_role("radio", name="Sonnet", exact=True).click()
        expect(page.get_by_role("button", name="模型", exact=True)).to_contain_text("Sonnet")
        assert page.evaluate("wsMessages.length") == 0
        page.get_by_role("button", name="關閉聊天工具").click()
        expect(plus).to_be_focused()
        field = page.locator("textarea[data-chat-input]")
        field.fill("第一行\n第二行\n第三行\n第四行")
        before = field.bounding_box()["height"]
        field.evaluate("el=>el.blur()")
        assert field.bounding_box()["height"] == before and before > 44
        expect(page.locator("dialog[open]")).to_have_count(0)
        page.wait_for_function("!composerToolsBackPending")
        plus.click()
        page.get_by_role("button", name="模型", exact=True).click()
        expect(page.get_by_role("radio", name="Sonnet", exact=True)).to_be_checked()
        page.get_by_role("radio", name="Sonnet", exact=True).click()
        expect(page.get_by_role("button", name="模型", exact=True)).to_be_focused()
        page.mouse.click(2, 2)
        expect(page.locator("dialog[open]")).to_have_count(0)
        expect(field).to_have_value("第一行\n第二行\n第三行\n第四行")
        page.close()

        # Close/backdrop dismiss the whole sheet even from a setting page.
        page = load(390, 844, True)
        for dismiss in ['close', 'backdrop']:
            page.wait_for_function('!composerToolsBackPending')
            page.get_by_role('button', name='聊天工具', exact=True).click()
            page.get_by_role('button', name='模型', exact=True).click()
            if dismiss == 'close':
                page.get_by_role('button', name='關閉聊天工具').click()
            else:
                page.mouse.click(2, 2)
            expect(page.locator('dialog[open]')).to_have_count(0)
            expect(page.get_by_role('button', name='聊天工具', exact=True)).to_be_focused()
        page.close()

        # Actual draft handlers + useChatSocket's flush callback apply settings only on send.
        page = load(390, 844, True)
        page.locator('textarea[data-chat-input]').fill('設定於送出套用')
        page.get_by_role('button', name='聊天工具', exact=True).click()
        expect(page.locator('.ra-chat-header select')).to_have_count(0)
        for label, value in [('模型', 'sonnet'), ('思考強度', 'high'), ('權限', 'bypassPermissions')]:
            page.get_by_role('button', name=label, exact=True).click()
            page.locator(f'input[type=radio][value="{value}"]').click()
        expect(page.get_by_role('button', name='權限', exact=True)).to_have_class(re.compile('danger'))
        assert page.evaluate('wsMessages.length') == 0
        page.get_by_role('button', name='關閉聊天工具').click()
        page.get_by_role('button', name='送出', exact=True).click()
        page.wait_for_function('wsMessages.some(x=>x.type==="input")')
        assert page.evaluate('wsMessages.map(x=>x.type)') == ['set_mode', 'set_model', 'set_effort', 'input']
        assert page.evaluate('wsMessages.slice(0,3)') == [
            {'type': 'set_mode', 'mode': 'bypassPermissions'}, {'type': 'set_model', 'model': 'sonnet'}, {'type': 'set_effort', 'effort': 'high'}]
        page.close()
        page = load(390, 844, True)
        page.get_by_role('button', name='聊天工具', exact=True).click()
        page.get_by_role('button', name='模型', exact=True).click()
        page.evaluate("config.state='THINKING';renderChat()")
        expect(page.get_by_role('radio', name='Sonnet', exact=True)).to_be_disabled()
        page.get_by_role('button', name='返回聊天工具').click()
        expect(page.get_by_role('button', name='模型', exact=True)).to_contain_text('預設')
        assert page.evaluate('wsMessages.length') == 0
        page.close()

        for agent, perm, effort in [("claude", True, True), ("codex", False, True), ("cursor", True, False),
                                    ("telegram", False, False), ("kiroacp", True, True), ("antigravity", True, True)]:
            page = load(390, 844, True, props={"session": {"id": agent, "agent_type": agent}})
            page.get_by_role("button", name="聊天工具", exact=True).click()
            expect(page.get_by_role("button", name="權限", exact=True)).to_have_count(int(perm))
            expect(page.get_by_role("button", name="思考強度", exact=True)).to_have_count(int(effort))
            expect(page.get_by_role("button", name="模型", exact=True)).to_have_count(1)
            page.close()

        page = load(390, 844, True, props={"guest": {"role": "editor"}})
        page.get_by_role("button", name="聊天工具", exact=True).click()
        expect(page.get_by_role("button", name="附加圖片或檔案", exact=True)).to_be_enabled()
        expect(page.get_by_role("button", name="模型", exact=True)).to_have_count(0)
        expect(page.get_by_role("button", name="輸入模式", exact=True)).to_have_count(0)
        page.close()
        for props, config in [({"guest": {"role": "viewer"}}, {}), ({"guest": {"role": "editor", "mode": "snapshot"}}, {}),
                              ({"session": {"id": "kiro", "agent_type": "kiro"}}, {}),
                              ({"guest": {"role": "editor"}}, {"shareEnded": True})]:
            page = load(390, 844, True, props=props, config=config, writable=False)
            expect(page.get_by_role("button", name="聊天工具", exact=True)).to_have_count(0)
            page.close()

        page = load(390, 844, True)
        page.locator("textarea[data-chat-input]").fill("模式切換保留")
        page.evaluate("window.originalPicker=document.querySelector('input[type=file]')")
        page.get_by_role("button", name="聊天工具", exact=True).click()
        page.get_by_role("button", name="輸入模式", exact=True).click()
        page.get_by_role("radio", name="Shell", exact=True).click()
        expect(page.get_by_role("dialog", name="聊天工具", exact=True)).to_be_visible()
        expect(page.get_by_role("button", name="模型", exact=True)).to_have_count(0)
        expect(page.get_by_role("button", name="附加圖片或檔案", exact=True)).to_have_count(0)
        assert page.locator('input[type=file]').evaluate("el=>el===originalPicker")
        page.get_by_role("button", name="輸入模式", exact=True).click()
        page.get_by_role("radio", name="Claude", exact=True).click()
        expect(page.locator("textarea[data-chat-input]")).to_have_value("模式切換保留")
        page.close()

        page = load(390, 844, True, config={"state": "THINKING"})
        page.get_by_role("button", name="聊天工具", exact=True).click()
        for label in ["模型", "思考強度", "權限", "輸入模式"]:
            expect(page.get_by_role("button", name=label, exact=True)).to_be_disabled()
        expect(page.get_by_role("button", name="關閉聊天工具")).to_be_enabled()
        expect(page.get_by_role("button", name="附加圖片或檔案", exact=True)).to_be_enabled()
        page.close()
        page = load(390, 844, True, config={"modelFail": True, "modelSel": "custom-model", "connected": False})
        page.get_by_role("button", name="聊天工具", exact=True).click()
        page.get_by_role("button", name="模型", exact=True).click()
        expect(page.get_by_role("radio", name="custom-model", exact=True)).to_be_checked()
        page.get_by_role("radio", name="預設", exact=True).click()
        assert page.evaluate("wsMessages.length") == 0
        page.close()
        # An open modal must retain the conversation's scroll position.
        page = load(390, 844, True)
        page.evaluate("document.body.style.minHeight='2000px';window.scrollTo(0,100)")
        before_scroll = page.evaluate('scrollY')
        page.get_by_role('button', name='聊天工具', exact=True).click()
        page.mouse.move(10, 10)
        page.mouse.wheel(0, 400)
        page.wait_for_timeout(100)
        assert page.evaluate('scrollY') == before_scroll, (before_scroll, page.evaluate('scrollY'))
        page.get_by_role('button', name='關閉聊天工具').click()
        assert page.evaluate('scrollY') == before_scroll
        page.close()
        page = load(639, 844, False)
        page.evaluate('delete window.Telegram')
        page.get_by_role('button', name='聊天工具', exact=True).click()
        page.set_viewport_size({'width': 640, 'height': 844})
        expect(page.locator('dialog[open]')).to_have_count(0)
        expect(page.locator('#chat-model')).to_be_visible()
        page.wait_for_function('!composerToolsBackPending')
        page.close()

        # App's actual shortcut handler + reducer + split layout + both ChatViews.
        page = load()
        app_source = (root / 'js/app.js').read_text(encoding='utf-8')
        start = app_source.index('    const onKey = (e) => {', app_source.index('// 鍵盤快捷鍵：'))
        end = app_source.index("    window.addEventListener('keydown', onKey);", start)
        pair = r"""
        function ShortcutPair(){
          const [tabGroups,dispatchTabGroups]=React.useReducer(tabGroupsReducer,{
            groups:[{id:'g0',tabs:['one'],active:'one'},{id:'g1',tabs:['two'],active:'two'}],
            focused:0,ratio:.5,orientation:'horizontal'
          });
          const [options,setOptions]=useState({});
          window.pairOptions=setOptions;
          window.activeGroup=tabGroups.focused;
          useEffect(()=>{
            const isWideScreen=true,setMobileActivePane=()=>{};
            HANDLER
            window.addEventListener('keydown',onKey);
            return()=>window.removeEventListener('keydown',onKey);
          },[]);
          const content=(id,i)=>React.createElement(ChatView,{
            key:id,session:{id,agent_type:'claude',name:id},fullHeight:false,
            isFocused:tabGroups.focused===i,isSecondaryPane:i===1,
            guest:i===1?options.guestTwo:null
          });
          return React.createElement('div',{style:{height:'900px'}},React.createElement(SplitChatContainer,{
            isSplit:true,activePane:tabGroups.focused===1?'secondary':'primary',
            onFocusPane:p=>dispatchTabGroups({type:'focus',groupIndex:p==='secondary'?1:0}),
            primaryContent:content('one',0),secondaryContent:content('two',1)
          }));
        }
        ReactDOM.flushSync(()=>appRoot.render(React.createElement(ShortcutPair)));
        """.replace('HANDLER', app_source[start:end])
        page.add_script_tag(content=pair)
        one, two = page.locator('textarea[data-chat-input]').all()
        expect(one).to_be_enabled()
        expect(two).to_be_enabled()
        one.fill('LEFT')
        two.fill('RIGHT')
        one.focus()
        one.evaluate('el=>el.setSelectionRange(2,2)')
        two.evaluate('el=>el.setSelectionRange(1,1)')
        # Focus must move with the shortcut, before subsequent typing can reach the old pane.
        assert page.evaluate("""() => {
          window.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight',ctrlKey:true,altKey:true}));
          return document.activeElement === document.querySelector('[data-chat-pane="secondary"] textarea[data-chat-input]');
        }""")
        page.wait_for_function('activeGroup===1')
        expect(two).to_be_focused()
        assert two.evaluate('el=>el.selectionStart') == 1
        page.keyboard.type('X')
        expect(two).to_have_value('RXIGHT')
        expect(one).to_have_value('LEFT')
        page.keyboard.press('Control+Alt+ArrowLeft')
        page.wait_for_function('activeGroup===0')
        expect(one).to_be_focused()
        assert one.evaluate('el=>el.selectionStart') == 2
        page.keyboard.type('Y')
        expect(one).to_have_value('LEYFT')
        expect(two).to_have_value('RXIGHT')
        # Repeated shortcut to the same pane also returns from its header to input.
        page.locator('.ra-chat-header').first.get_by_role('button', name='分享這個聊天室', exact=True).last.focus()
        page.keyboard.press('Control+Alt+ArrowLeft')
        expect(one).to_be_focused()
        # A read-only target must not leave typing in the previous conversation.
        page.evaluate("pairOptions({guestTwo:{role:'viewer'}})")
        expect(page.locator('[data-chat-pane="secondary"] textarea[data-chat-input]')).to_have_count(0)
        page.keyboard.press('Control+Alt+ArrowRight')
        page.wait_for_function('activeGroup===1')
        expect(one).not_to_be_focused()
        expect(page.locator('[data-chat-pane="secondary"]')).to_be_focused()
        page.keyboard.type('Z')
        expect(one).to_have_value('LEYFT')
        # IME composition must still suppress shortcuts.
        one.focus()
        one.dispatch_event('keydown', {'key':'ArrowLeft','ctrlKey':True,'altKey':True,'isComposing':True})
        assert page.evaluate('activeGroup') == 1
        page.close()
    finally:
        browser.close()

assert not browser_errors, browser_errors
print("composer attachment checks passed")

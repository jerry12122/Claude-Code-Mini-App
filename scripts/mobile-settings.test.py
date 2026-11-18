"""Run: python scripts/mobile-settings.test.py (Playwright + Edge + Go)."""
import re
import subprocess
import tempfile
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
root = repo / 'internal/static'
index = (root / 'index.html').read_text(encoding='utf-8')
styles = '\n'.join(re.findall(r'<style[^>]*>(.*?)</style>', index, re.S))
config = re.search(r'<script>\s*(tailwind.config = .*?)</script>', index, re.S)[1]
core = (root / 'js/core/core.js').read_text(encoding='utf-8')
source = (root / 'js/ui/SettingsModal.js').read_text(encoding='utf-8')
compiled = subprocess.run(
    ['go', 'run', 'github.com/evanw/esbuild/cmd/esbuild', '--loader=jsx', '--target=es2020'],
    input=source, encoding='utf-8', capture_output=True, cwd=repo, check=True,
).stdout
setup = r"""
const {useState,useEffect,useLayoutEffect,useRef,useMemo}=React;
const APPEARANCE_DEFAULTS={headingColor:'#ffaa55',boldColor:'#88bb99',codeColor:'#aa88ff',customCss:''};
const GENERAL_DEFAULTS={vscodeNoAdmin:false},CHAT_FS_DEFAULT=14,CHAT_FS_MIN=12,CHAT_FS_MAX=24;
let stored={...APPEARANCE_DEFAULTS};
const readStoredAppearance=()=>({...stored}),saveStoredAppearance=d=>{stored={...d}},applyAppearance=()=>{};
const putAppearance=async d=>({...d}),fetchServerConfig=async()=>({version:'1.2.3',sha:'abcdef012345'});
const getGeneralSettings=async()=>({...GENERAL_DEFAULTS}),putGeneralSettings=async g=>g;
let globalInstruction={text:'',mode:'native'};
const apiFetch=async (path,opts)=>{
  if(path!=='/settings/global-instruction') throw Error('Unexpected settings API: '+path);
  if(opts.method==='PUT') globalInstruction=JSON.parse(opts.body);
  return {ok:true,json:async()=>globalInstruction};
};
const readThemePref=()=> 'dark',setThemePref=()=>{},readChatFontSize=()=>14,setChatFontSize=n=>n;
const SharesSection=()=>React.createElement('p',null,'分享內容');
const LogsSection=()=>React.createElement('p',null,'日誌內容');
const WebUpdateSection=()=>React.createElement('p',null,'更新內容');
window.update={bridge:false,openNonce:0,hasUpdate:true};
function Fixture(){
  const [open,setOpen]=useState(false),[nonce,setNonce]=useState(0);
  window.openUpdate=()=>{setNonce(n=>n+1);setOpen(true)};
  return React.createElement(React.Fragment,null,
    React.createElement('button',{onClick:()=>setOpen(true)},'開啟設定'),
    React.createElement(SettingsModal,{open,onClose:()=>setOpen(false),desktopUpdate:{...update,openNonce:nonce}}));
}
"""
media_hook = core[core.index('function useMediaQuery('):core.index('/**', core.index('function useMediaQuery('))]
screens = repo / '.spectra/design-cache/mobile-settings'
screens.mkdir(parents=True, exist_ok=True)
errors = []

with tempfile.TemporaryDirectory(prefix='settings-check-') as tmp, sync_playwright() as p:
    fixture = Path(tmp) / 'settings.html'
    fixture.write_text('<!doctype html><html lang="zh-TW" data-theme="dark"><meta charset="utf-8">'
                       '<meta name="viewport" content="width=device-width,initial-scale=1">'
                       '<style>' + styles + '</style><body><div id="root"></div></body></html>', encoding='utf-8')
    browser = p.chromium.launch(channel='msedge', headless=True)
    try:
        for width, height, touch in [(390, 844, True), (320, 640, True), (844, 390, True), (1280, 900, False)]:
            page = browser.new_page(viewport={'width': width, 'height': height}, has_touch=touch)
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.goto(fixture.as_uri())
            for vendor in ['tailwind.js', 'react.production.min.js', 'react-dom.production.min.js']:
                page.add_script_tag(path=str(root / 'js/vendor' / vendor))
            page.add_script_tag(content=config)
            page.add_script_tag(content=setup + media_hook + compiled)
            page.add_script_tag(content="ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(Fixture))")
            opener = page.get_by_role('button', name='開啟設定', exact=True)
            opener.click()
            nav = page.get_by_role('navigation', name='設定分類')
            back = page.get_by_role('button', name='返回設定分類')
            mobile = touch
            expect(nav).to_be_visible()
            expect(back).to_have_count(0)
            expect(page.get_by_text('開啟 VSCode', exact=True)).to_have_count(0 if mobile else 1)
            if width == 390:
                page.screenshot(path=str(screens / 'categories.png'))
            for label, content in [('一般', '開啟 VSCode'), ('分享', '分享內容'), ('日誌', '日誌內容'), ('更新', '更新內容')]:
                nav.get_by_role('button', name=re.compile('^' + label)).click()
                expect(page.get_by_text(content, exact=True)).to_be_visible()
                if mobile:
                    expect(nav).to_have_count(0)
                    expect(back).to_be_focused()
                if label == '一般':
                    field = page.get_by_role('textbox', name='全域指令', exact=True)
                    field.fill('全域規則\n第二行')
                    mode = page.get_by_role('radio', name=re.compile('^僅原生支援'))
                    expect(mode).to_be_checked()
                    mode.focus()
                    page.keyboard.press('ArrowRight')
                    expect(page.get_by_role('radio', name='會話第一則訊息')).to_be_checked()
                    page.get_by_role('button', name='儲存全域指令').click()
                    expect(page.get_by_role('status')).to_have_text('已儲存全域指令')
                    assert page.evaluate('globalInstruction') == {'text': '全域規則\n第二行', 'mode': 'first'}
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
                if mobile:
                    back.click()
                    expect(nav.get_by_role('button', name=re.compile('^' + label))).to_be_focused()
                else:
                    expect(nav).to_be_visible()
            page.get_by_role('button', name='外觀', exact=True).click()
            draft = page.locator('textarea')
            draft.fill('.prose { color: pink; }')
            if width == 390:
                page.screenshot(path=str(screens / 'appearance.png'))
            if mobile:
                page.keyboard.press('Escape')
                expect(nav).to_be_visible()
                page.get_by_role('button', name='外觀', exact=True).click()
                expect(draft).to_have_value('.prose { color: pink; }')
            page.get_by_role('button', name='儲存', exact=True).click()
            expect(page.get_by_text('已儲存', exact=True)).to_be_visible()
            page.get_by_role('button', name='關閉', exact=True).click()
            expect(opener).to_be_focused()
            opener.click()
            if mobile:
                expect(nav).to_be_visible()
                expect(back).to_have_count(0)
                page.evaluate('openUpdate()')
                expect(page.get_by_text('更新內容', exact=True)).to_be_visible()
                expect(back).to_be_focused()
                back.click()
                page.get_by_role('button', name='外觀', exact=True).click()
                expect(draft).to_have_value('.prose { color: pink; }')
                page.get_by_role('button', name='關閉設定').click()
                opener.click()
                page.keyboard.press('Escape')
                expect(page.get_by_role('dialog')).to_have_count(0)
            page.close()
    finally:
        browser.close()

assert not errors, errors
print('mobile settings checks passed: categories, enter/back, focus, drafts, save, update, desktop, layouts')

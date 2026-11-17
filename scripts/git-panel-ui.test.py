"""行內 Git 控制互動檢查：python scripts/git-panel-ui.test.py（沿用 Playwright、Edge、Go）。"""
import re
import subprocess
import tempfile
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
static = repo / 'internal/static'
index = (static / 'index.html').read_text(encoding='utf-8')
styles = '\n'.join(re.findall(r'<style[^>]*>(.*?)</style>', index, re.S))
config = re.search(r'<script>\s*(tailwind.config = .*?)</script>', index, re.S)[1]
source = '\n'.join((static / 'js' / path).read_text(encoding='utf-8') for path in [
    'hooks/useGitStatus.js', 'chat/GitControls.js', 'chat/ChatHeader.js',
])
fixture = r'''
const {useState,useEffect,useLayoutEffect,useRef,useCallback}=React;
let branch='main',behind=1;
window.calls=[];
const apiFetch=async(url,opts={})=>{
  calls.push({url,...opts});
  let data={},ok=true;
  if(opts.method==='POST'){
    if(url.endsWith('/push')){ok=false;data={error:'遠端拒絕推送'};}
    else {if(url.endsWith('/checkout'))branch=JSON.parse(opts.body).branch;if(url.endsWith('/pull'))behind=0;data={ok:true,output:'操作完成'};}
  }else if(url.endsWith('/status'))data={is_repo:true,branch,upstream:'origin/main',ahead:1,behind,additions:2043,deletions:3,files:[]};
  else data={current:branch,branches:['dev','main']};
  return {ok,status:ok?200:422,json:async()=>data};
};
const AGENT_LABEL={claude:'Claude'};
const sessionDisplayAgent=()=> 'claude',workDirGroupShortLabel=()=> 'repo',resolveQuotaBars=()=>null,getAgentBadgeClass=()=>'';
const QuotaBadge=()=>null;
const AgentBadgeIcon=()=>null,SessionStateChip=()=>React.createElement('span',null,'待命');
const GitBranchBadge=({branch})=>React.createElement('span',null,branch);
function Fixture(){
  const [busy,setBusy]=useState(false),[guest,setGuest]=useState(false);
  window.setGitBusy=value=>ReactDOM.flushSync(()=>setBusy(value));
  window.setGitGuest=value=>ReactDOM.flushSync(()=>setGuest(value));
  return <ChatSessionHeader session={{id:'fixture',name:'測試會話',git_branch:'main',status:busy?'running':'idle'}}
    agentType="claude" state={busy?'THINKING':'IDLE'} guest={guest?{role:'viewer',nickname:'訪客'}:null}/>;
}
ReactDOM.createRoot(document.getElementById('root')).render(<Fixture/>);
'''
compiled = subprocess.run(
    ['go', 'run', 'github.com/evanw/esbuild/cmd/esbuild', '--loader=jsx', '--target=es2020'],
    input=source+fixture, encoding='utf-8', capture_output=True, cwd=repo, check=True,
).stdout

with tempfile.TemporaryDirectory(prefix='git-panel-') as tmp, sync_playwright() as p:
    html = Path(tmp) / 'panel.html'
    html.write_text('<!doctype html><html lang="zh-TW" data-theme="dark"><meta charset="utf-8">'
                    '<meta name="viewport" content="width=device-width,initial-scale=1">'
                    '<style>'+styles+'</style><body><main id="root"></main></body></html>', encoding='utf-8')
    browser = p.chromium.launch(channel='msedge', headless=True)
    try:
        for width, fallback in [(390, False), (1280, False), (390, True)]:
            page = browser.new_page(viewport={'width':width,'height':844})
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.set_default_timeout(5000)
            if fallback:
                page.add_init_script("const supports=CSS.supports.bind(CSS);CSS.supports=(...args)=>args[0]==='position-area: block-end'?false:supports(...args)")
            page.goto(html.as_uri())
            if fallback:
                page.add_style_tag(content='.ra-git-popover {position:fixed!important;position-area:none!important;inset:auto!important;margin:0!important;left:var(--git-left)!important;top:var(--git-top)!important;}')
            for name in ['tailwind.js','react.production.min.js','react-dom.production.min.js']:
                page.add_script_tag(path=str(static/'js/vendor'/name))
            page.add_script_tag(content=config)
            page.add_script_tag(content=compiled)
            if width == 390:
                page.locator('summary').click()
            selector=page.get_by_role('button',name='切換分支',exact=True)
            expect(selector).to_contain_text('main')
            assert page.get_by_role('combobox',name='切換分支').count()==0
            selector.click()
            branch_panel=page.locator('[popover]:popover-open')
            expect(branch_panel.get_by_role('button',name='main',exact=True)).to_have_attribute('aria-current','true')
            page.keyboard.press('Escape')
            expect(selector).to_be_focused()
            expect(page.get_by_text('↑1 ↓1',exact=True).filter(visible=True)).to_be_visible()
            expect(page.get_by_text('+2043',exact=True).filter(visible=True)).to_be_visible()
            expect(page.get_by_text('-3',exact=True).filter(visible=True)).to_be_visible()
            assert page.locator('dialog').count()==0
            assert '同步數字依本機已知的上游狀態顯示。' not in page.locator('body').inner_text()
            assert '領先' not in page.locator('body').inner_text()
            assert '落後' not in page.locator('body').inner_text()
            trigger=page.get_by_role('button',name='Git 操作',exact=True)
            trigger.click()
            popover=page.locator('[popover]:popover-open')
            expect(popover).to_be_visible()
            if fallback:
                page.wait_for_function("document.querySelector('[popover]:popover-open').style.getPropertyValue('--git-top') !== ''")
            box=popover.bounding_box()
            assert box['x']>=0 and box['x']+box['width']<=width+1
            if fallback:
                anchor=trigger.bounding_box()
                assert abs(box['y']-(anchor['y']+anchor['height']+4))<2, (box,anchor)
            page.get_by_role('heading',name='測試會話',exact=True).click() if width == 1280 else page.locator('summary').click()
            expect(popover).to_have_count(0)
            if width == 390:
                page.locator('summary').click()
            trigger.click()
            popover.get_by_role('button',name='Push',exact=True).click()
            expect(page.get_by_role('alert')).to_have_text('遠端拒絕推送')
            expect(selector).to_contain_text('main')
            expect(popover).to_have_count(0)
            trigger.click()
            popover.get_by_role('button',name='Pull',exact=True).click()
            expect(page.get_by_text('↑1 ↓0',exact=True).filter(visible=True)).to_be_visible()
            selector.click()
            page.locator('[popover]:popover-open').get_by_role('button',name='dev',exact=True).click()
            expect(selector).to_contain_text('dev')
            page.evaluate('setGitBusy(true)')
            expect(selector).to_be_disabled()
            trigger.click()
            for name in ['Pull','Push']:
                expect(popover.get_by_role('button',name=name,exact=True)).to_be_disabled()
            page.keyboard.press('Escape')
            expect(popover).to_have_count(0)
            expect(trigger).to_be_focused()
            page.evaluate('setGitBusy(false)')
            trigger.focus()
            page.keyboard.press('Enter')
            expect(popover).to_be_visible()
            page.keyboard.press('Tab')
            expect(popover.get_by_role('button',name='Pull',exact=True)).to_be_focused()
            page.keyboard.press('Escape')
            expect(trigger).to_be_focused()
            before=page.evaluate('calls.length')
            page.evaluate('setGitGuest(true)')
            expect(selector).to_have_count(0)
            expect(trigger).to_have_count(0)
            page.wait_for_timeout(50)
            assert page.evaluate('calls.length')==before,'訪客不查詢 Git API'
            assert not errors, errors
            page.close()
    finally:
        browser.close()
print('Git controls: badges, branch menu, popover, failures, busy, guest and keyboard passed')

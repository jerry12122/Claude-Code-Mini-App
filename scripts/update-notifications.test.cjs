const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/hooks/useDesktopUpdate.js'), 'utf8');

function setup({ hidden = false, bridge = false, storage = new Map() } = {}) {
  let now = 1000000000, config = {version:'0.18.5',sha:'old'}, release = {tag_name:'v0.18.6'}, replyStatus = 200, offline = false;
  const listeners = {}, timers = [], notices = [], statuses = [], calls = [];
  const document = {hidden, addEventListener:(k,f)=>listeners[k]=f, removeEventListener:k=>delete listeners[k]};
  const window = {__desktopUpdater:bridge ? {request(){}} : undefined, addEventListener:(k,f)=>listeners[k]=f, removeEventListener:k=>delete listeners[k]};
  const ctx = vm.createContext({document,window,AbortController,Date:class extends Date { static now(){return now;} },
    localStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)},
    setInterval:f=>{timers.push(f);return 1;},clearInterval:()=>{},
    apiFetch:async()=>({ok:true,json:async()=>config}),
    fetchWithTimeout:async(url,opts)=>{calls.push({url,opts});if(offline)throw new Error('offline');return {ok:replyStatus===200,status:replyStatus,headers:{get:()=> '7200'},json:async()=>release};},
  });
  vm.runInContext(source,ctx);
  const start = () => ctx.startWebUpdateChecks({onStatus:s=>statuses.push(s),onNotice:s=>notices.push(s)});
  const settle = async()=>{for(let i=0;i<12;i++)await Promise.resolve();};
  return {ctx,document,listeners,timers,notices,statuses,calls,start,settle,storage,
    advance:ms=>now+=ms,setConfig:c=>config=c,setRelease:r=>release=r,setReply:s=>replyStatus=s,setOffline:v=>offline=v};
}

test('web checks hourly without credentials; same-version notice is daily and survives restart',async()=>{
  const s=setup(); const stop=s.start(); await s.settle();
  assert.equal(s.notices.length,1); assert.equal(s.statuses.at(-1).availableVersion,'0.18.6');
  assert.equal(s.calls[0].opts.credentials,'omit'); assert.equal(s.calls[0].opts.referrerPolicy,'no-referrer');
  assert.equal(s.calls[0].opts.headers?.Authorization,undefined);
  s.advance(3600000); s.timers[0](); await s.settle(); assert.equal(s.calls.length,2); assert.equal(s.notices.length,1);
  stop(); assert.equal(Object.keys(s.listeners).length,0);
  const next=setup({storage:s.storage}); next.start(); await next.settle(); assert.equal(next.notices.length,0);
  s.advance(86400000); s.timers[0](); await s.settle(); assert.equal(s.notices.length,1,'stopped poll cannot notify');
});

test('hidden web waits for foreground; invalid, same, older and prereleases do not notify',async()=>{
  const s=setup({hidden:true}); s.start(); await s.settle(); assert.equal(s.calls.length,0);
  s.document.hidden=false; s.listeners.visibilitychange(); await s.settle(); assert.equal(s.notices.length,1);
  for(const release of [{tag_name:'v0.18.5'},{tag_name:'v0.18.4'},{tag_name:'v0.19.0',prerelease:true},{tag_name:'v0.19.0',draft:true},{tag_name:'javascript:alert(1)'}]){
    s.advance(3600000);s.setRelease(release);s.timers[0]();await s.settle();assert.equal(s.notices.length,1);
  }
});

test('web respects Retry-After and server upgrade only offers reload',async()=>{
  const s=setup();s.setReply(429);s.start();await s.settle();assert.equal(s.notices.length,0);
  s.advance(3600000);s.timers[0]();await s.settle();assert.equal(s.calls.length,1);
  s.advance(3600000);s.setReply(200);s.setRelease({tag_name:'v0.18.6'});s.setConfig({version:'0.18.6',sha:'new'});s.timers[0]();await s.settle();
  assert.equal(s.statuses.at(-1).reloadRequired,true);assert.equal(s.notices.at(-1).reloadRequired,true);
});

test('desktop bridge performs no browser release checks',async()=>{
  const s=setup({bridge:true});s.start();await s.settle();assert.equal(s.calls.length,0);
});

test('web can notify about host upgrades when GitHub is offline',async()=>{
  const s=setup();s.setOffline(true);s.start();await s.settle();
  s.advance(3600000);s.setConfig({version:'0.18.6',sha:'new'});s.timers[0]();await s.settle();
  assert.equal(s.statuses.at(-1)?.reloadRequired,true);
  assert.equal(s.notices.at(-1)?.reloadRequired,true);
});

test('web reminds after a day and notifies immediately for a different version',async()=>{
  const s=setup();s.start();await s.settle();
  s.advance(86400000);s.timers[0]();await s.settle();assert.equal(s.notices.length,2);
  s.advance(3600000);s.setRelease({tag_name:'v0.18.7'});s.timers[0]();await s.settle();
  assert.equal(s.notices.length,3);assert.equal(s.notices.at(-1).availableVersion,'0.18.7');
});

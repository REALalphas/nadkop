import {installDeviceWidgets} from './fixtures/device-widgets.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';

const { chromium } = await import(process.env.NADKOP_PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(import.meta.dirname, '..');
const asset = name => path.join(root, 'openwrt/luci-app-nadkop/htdocs/luci-static/resources', name);
const [viewSource, pickerSource, css] = await Promise.all([
 fs.readFile(asset('view/nadkop/overview.js'), 'utf8'), fs.readFile(asset('nadkop/picker.js'), 'utf8'), fs.readFile(asset('nadkop/picker.css'), 'utf8')
]);
const browser = await chromium.launch({ executablePath: process.env.NADKOP_CHROMIUM || '/run/current-system/sw/bin/chromium', headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage({ viewport: { width: 1200, height: 950 }, colorScheme: 'light' });
const failures = [];
page.on('pageerror', error => failures.push(error.message));
try {
 await page.route('https://nadkop.test/**/picker.css*', route => route.fulfill({ contentType: 'text/css', body: css }));
 await page.route('https://cdn.jsdelivr.net/**', route => route.abort());
 await page.route('https://nadkop.test/', route => route.fulfill({contentType:'text/html',body:'<main></main>'}));
 await page.goto('https://nadkop.test/');
 await page.setContent('<style>body{font-family:sans-serif;margin:24px}.cbi-value{margin:12px 0}.cbi-value-title{display:inline-block;min-width:180px}.cbi-value-field{display:inline-block;max-width:100%}.cbi-value-description{color:#666;font-size:13px}input,textarea,select{padding:8px;max-width:100%;box-sizing:border-box}button{margin:3px;padding:7px}.cbi-section{border-radius:8px}summary{padding:8px 0}</style><main></main>');
 await page.evaluate(installDeviceWidgets);
 await page.evaluate(({ viewSource, pickerSource }) => {
  window.E = function(tag, attrs, children) {
   const el = document.createElement(tag);
   Object.entries(attrs || {}).forEach(([key, value]) => {
    if (value == null) return;
    if (typeof value === 'function') el.addEventListener(key, value);
    else el.setAttribute(key, value === true ? '' : String(value));
   });
   const append = value => { if (Array.isArray(value)) throw new Error('LuCI E() children must be flat'); else if (value != null) el.append(value instanceof Node ? value : document.createTextNode(String(value))); };
   (children || []).forEach(append); return el;
  };
  const ref = 'srv_' + '1'.repeat(32);
  window.editor = { sources: [{ name: 'Existing', url: 'https://existing.test/sub?token=fake-private-token', headers: {Authorization:'fake-private-header'}, hwid: { enabled: true, value:'fake-private-hwid' } }], rules: [{ name:'First', domains: ['saved.example'], action: 'proxy', nodeRef: ref }, {name:'Second',domains:['second.example'],action:'block'}], picker: { pinned: [ref], order: [ref], emojiStyle: 'system' }, default: 'direct', socksPort: 1080, zapretArgs: [] };
  window.disk = structuredClone(window.editor);
  window.summaries = [{ id: ref, source: 'Existing', name: 'Existing server', tag: 'Existing/main', protocol: 'trojan', available: true }];
  window.saveCalls = 0; window.holdSave = false;
  window.calls = []; window.notices = []; window.saved = null; window.actions = [];
  let revision = 'r1';
  const api = { declare({ method }) { return async (...args) => {
   if (method === 'service') { window.actions.push(args[0]); return {}; }
   if (method === 'addsource') {
    window.calls.push(structuredClone(args));
    await new Promise(resolve => setTimeout(resolve, 90));
    if (args[1] !== revision) return { error: 'Stale revision' };
    if (args[0].includes('broken')) return { error: 'Subscription HTTP status 503' };
    let source = window.disk.sources.find(s => (s.url || s.file) === args[0]);
    const existing = !!source;
    if (!source) {
     source = args[0].startsWith('/') ? { name: 'local_nodes', file: args[0] } : { name: 'New_VPN_-_EU-', url: args[0], ...args[2] };
     window.disk.sources.push(structuredClone(source)); revision = 'r' + (window.disk.sources.length + 1);
     window.summaries.push({ id: 'srv_' + String(window.disk.sources.length).repeat(32), source: source.name, name: 'New server', protocol: 'vless', tag: source.name + '/main', available: true });
    }
    return structuredClone({ source, revision, servers: window.summaries, existing });
   }
   if (method === 'zaprettune')return {job:null};
   if (method === 'validate')return {ok:true};
   if (method === 'save') {
    window.saveCalls++;
    if(window.holdSave)await new Promise(resolve=>{window.releaseSave=resolve;});
    if (args[2] !== revision) return { error: 'Stale save revision' };
    window.saved = structuredClone({ config: args[0], nodes: args[1] }); return { revision: 'saved' };
   }
   return {};
  }; } };
  const picker = new Function('baseclass', 'E', pickerSource)({ extend: value => value }, E);
  const view = new Function('view', 'rpc', 'ui', 'poll', 'picker', 'E', 'L', viewSource)(
   { extend: value => value }, api, { addNotification(_, el) { window.notices.push(el.textContent); },
    showModal(title,content){document.body.append(E('div',{role:'dialog','aria-label':title},content));},
    hideModal(){document.querySelector('[role=dialog]')?.remove();} }, { add() {} }, picker, E,
   { resource: name => 'https://nadkop.test/luci-static/resources/' + (/^(?:[a-zA-Z0-9_.%,;-]+\/)*[a-zA-Z0-9_.%,;-]+$/.test(name) ? name : '') }
  );
  document.querySelector('main').append(view.render([{ config: window.editor, nodes: [], servers: window.summaries, revision }, { instances: {}, devices: ['eth0'] }, { id: 'device-hwid-value' }]));
 }, { viewSource, pickerSource });

 const unloadBlocked = () => page.evaluate(() => {
  const event = new Event('beforeunload', {cancelable:true});
  window.dispatchEvent(event);
  return event.defaultPrevented && event.returnValue === false;
 });
 const reset = page.getByRole('button',{name:'Discard changes',exact:true});
 assert.equal(await reset.isDisabled(),true);
 assert.equal(await unloadBlocked(),false);
 await page.locator('[data-rule-index="0"] .nk-rule-head').hover();
 await page.getByRole('button',{name:'Move down rule 1',exact:true}).click();
 assert.equal(await unloadBlocked(),true);
 assert.equal(await reset.isEnabled(),true);
 assert.deepEqual(await page.evaluate(()=>Object.keys(localStorage).filter(key=>!['nadkop.rules.collapsed.v1','nadkop.servers.collapsed.v1'].includes(key))),[]);
 assert.equal(await page.evaluate(()=>/fake-private|saved.example|srv_111/.test(JSON.stringify(localStorage))),false);
 await reset.click();
 await page.getByRole('button',{name:'Cancel',exact:true}).click();
 assert.equal(await unloadBlocked(),true);
 await reset.click();
 await page.getByRole('button',{name:'Discard',exact:true}).click();
 assert.equal(await page.evaluate(()=>window.saveCalls),0);
 assert.equal(await unloadBlocked(),false);
 assert.equal(await reset.isDisabled(),true);
 assert.equal(await page.locator('[data-rule-index="0"] .nk-rule-title').textContent(),'First');
 await page.locator('[data-rule-index="0"] .nk-rule-head').hover();
 await page.getByRole('button',{name:'Move down rule 1',exact:true}).click();
 assert.equal(await page.locator('[data-rule-index="0"] .nk-rule-title').textContent(),'Second');
 assert.equal(await page.getByRole('button',{name:'Move up rule 1',exact:true}).isDisabled(),true);
 assert.equal(await page.getByRole('button',{name:'Move down rule 2',exact:true}).isDisabled(),true);
 await page.getByRole('tab',{name:'Settings',exact:true}).click();
 await page.getByLabel('Apply rules to the home network',{exact:true}).selectOption('on');
 await page.getByLabel('Home network devices',{exact:true}).selectOption(['br-lan','guest']);
 await page.getByLabel('Internet uplink device',{exact:true}).selectOption('eth0');
 await page.getByLabel('Zapret strategy',{exact:true}).fill('--filter-tcp=443\n--dpi-desync=fake');
 await page.getByLabel('IPv6 traffic',{exact:true}).selectOption('off');
 await page.getByText('Advanced',{exact:true}).click();
 await page.getByLabel('Internal Xray port',{exact:true}).fill('12346');
 await page.getByLabel('Internal DNS port',{exact:true}).fill('1054');
 await page.getByLabel('DNS for regular queries',{exact:true}).fill('9.9.9.9');
 await page.getByText('Exceptions',{exact:true}).click();
 await page.getByLabel('Direct addresses',{exact:true}).fill(['203.0.113.0/24','2001:db8::/32'].join('\n'));
 await page.getByLabel('Update every, hours',{exact:true}).fill('12');
 await page.evaluate(()=>{window.holdSave=true;});
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.waitForFunction(()=>!!window.releaseSave);
 assert.equal(await reset.isDisabled(),true);
 assert.equal(await unloadBlocked(),true);
 await page.evaluate(()=>{window.releaseSave();});
 await page.waitForFunction(()=>window.saved!==null);
 assert.equal(await unloadBlocked(),false);
 assert.equal(await reset.isDisabled(),true);
 await page.evaluate(()=>{window.holdSave=false;});
 assert.equal(await page.evaluate(()=>window.saved.config.transparent.enabled),true);
 assert.deepEqual(await page.evaluate(()=>window.saved.config.transparent.interfaces),['br-lan','guest']);
 assert.equal(await page.evaluate(()=>window.saved.config.wanInterface),'eth0');
 assert.deepEqual(await page.evaluate(()=>window.saved.config.zapretArgs),['--filter-tcp=443','--dpi-desync=fake']);
 assert.equal(await page.evaluate(()=>window.saved.config.transparent.ipv6),false);
 assert.equal(await page.evaluate(()=>window.saved.config.transparent.port),12346);
 assert.equal(await page.evaluate(()=>window.saved.config.transparent.dnsPort),1054);
 assert.equal(await page.evaluate(()=>window.saved.config.transparent.dnsUpstream),'9.9.9.9');
 assert.equal(await page.evaluate(()=>window.saved.config.updateIntervalHours),12);
 assert.equal(await page.evaluate(()=>window.saved.config.rules[1].nodeRef),'srv_'+'1'.repeat(32));
 await page.waitForFunction(()=>!document.querySelector('.nk-app button').disabled);
 // Reset must use the latest confirmed save, including settings and revision.
 const savesBeforeReset = await page.evaluate(()=>window.saveCalls);
 await page.getByLabel('Internal Xray port',{exact:true}).fill('12347');
 assert.equal(await unloadBlocked(),true);
 await reset.click();
 await page.getByRole('button',{name:'Discard',exact:true}).click();
 assert.equal(await page.evaluate(()=>window.saveCalls),savesBeforeReset);
 assert.equal(await unloadBlocked(),false);
 await page.getByRole('tab',{name:'Settings',exact:true}).click();
 await page.getByText('Advanced',{exact:true}).click();
 assert.equal(await page.getByLabel('Internal Xray port',{exact:true}).inputValue(),'12346');

 await page.evaluate(()=>{window.saved=null;});
 await page.getByRole('button',{name:'Save and apply',exact:true}).click();
 await page.waitForFunction(()=>window.notices.length>0||window.actions.length>0);
 // Revision errors must not apply an unsaved configuration.
 assert.equal(await page.evaluate(()=>window.actions.length),0);
 assert.match(await page.evaluate(()=>window.notices.at(-1)),/Stale save revision/);
 await page.setViewportSize({width:390,height:844});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 assert.deepEqual(failures,[]);
 console.log('PASS: draft beforeunload guard, reset/cancel without save, clean/busy reset controls, secrets absent from localStorage');
 console.log('PASS: rule order controls, preserved references, transparent LAN/IPv6/DNS/ports/update settings, save, stale-revision refusal and mobile settings');
} finally {await browser.close();}

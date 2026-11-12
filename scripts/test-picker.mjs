import {installDeviceWidgets} from './fixtures/device-widgets.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';

const { chromium } = await import(process.env.NADKOP_PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(import.meta.dirname, '..');
const asset = name => path.join(process.env.NADKOP_PICKER_ASSET_ROOT || path.join(root,'openwrt/luci-app-nadkop/htdocs/luci-static/resources'), name);
const [viewSource, pickerSource, css] = await Promise.all([
 fs.readFile(asset('view/nadkop/overview.js'), 'utf8'), fs.readFile(asset('nadkop/picker.js'), 'utf8'), fs.readFile(asset('nadkop/picker.css'), 'utf8')
]);
const browser = await chromium.launch({ executablePath: process.env.NADKOP_CHROMIUM || '/run/current-system/sw/bin/chromium', headless: true, args: ['--no-sandbox'] });
const page = await browser.newPage({ viewport: { width: 1360, height: 1020 }, colorScheme: 'light' });
const failures = [];
page.on('pageerror', error => failures.push(error.message));
const ids = ['1', '2', '3', '4'].map(c => 'srv_' + c.repeat(32));
const servers = [
 { id: ids[0], name: 'Amsterdam · Main', location: { countryCode: 'NL', city: 'Amsterdam' }, source: 'Europe', protocol: 'vless', transport: 'raw', port: 443, position: 1, tag: 'Europe/Amsterdam', available: true },
 { id: ids[1], name: '🇺🇸 Europe route 🔥', location: { countryCode: 'DE', city: 'Frankfurt' }, source: 'Europe', protocol: 'trojan', transport: 'grpc', port: 8443, position: 2, tag: 'Europe/Frankfurt', available: true },
 { id: ids[2], name: '🇫🇮 Helsinki', location: { country: 'Finland', city: 'Helsinki' }, source: 'Nordic', protocol: 'vless', transport: 'ws', port: 2053, position: 0, tag: 'Nordic/Helsinki', available: true },
 { id: ids[3], name: '🇨🇭 Zurich · Backup', source: 'Europe', protocol: 'vless', transport: 'xhttp', port: 443, position: 4, tag: 'Europe/Zurich', available: false },
 { id: 'srv_' + '5'.repeat(32), name: 'Main / Europe', source: 'Europe', separator: true, available: true, position: 0 },
 { id: 'srv_' + '6'.repeat(32), name: 'Backup', source: 'Europe', separator: true, available: true, position: 3 }
];
const priorOrder = [ids[2], ids[3], ids[1], ids[0]];
const config = { sources: [], picker: { order: priorOrder }, rules: [
 { domains: ['video.example'], action: 'proxy', nodeRef: ids[0] },
 { domains: ['work.example'], action: 'direct' },
 { domains: ['backup.example'], action: 'proxy', nodeRef: ids[3] }
], default: 'direct', socksPort: 1080, wanInterface:'eth0',zapretArgs:['--filter-tcp=80,443','--dpi-desync=fake'] };
try {
 await page.route('https://nadkop.test/**/picker.css*', route => route.fulfill({ contentType: 'text/css', body: css }));
 // Serve the same local flag assets that are packaged on the router.
 await page.route('https://nadkop.test/luci-static/resources/nadkop/flags/*.svg', async route => {
  const file = path.join(asset('nadkop/flags'), new URL(route.request().url()).pathname.split('/').at(-1));
  try { await route.fulfill({ contentType: 'image/svg+xml', body: await fs.readFile(file) }); }
  catch { await route.abort(); }
 });
 // Asset URLs are served by the CDN directly in production. Tests can reuse
 // a workstation-only cache to avoid depending on external browser TLS/DNS.
 if (process.env.NADKOP_EMOJI_TEST_CACHE) await page.route('https://cdn.jsdelivr.net/**/*.png', async route => {
  const file = path.join(process.env.NADKOP_EMOJI_TEST_CACHE, new URL(route.request().url()).pathname.split('/').at(-1));
  try { await route.fulfill({ contentType: 'image/png', body: await fs.readFile(file) }); }
  catch { await route.abort(); }
 });
 await page.route('https://nadkop.test/', route => route.fulfill({contentType:'text/html',body:'<!doctype html><main></main>'}));
 await page.goto('https://nadkop.test/');
 await page.setContent('<!doctype html><meta charset="utf-8"><style>body{font:14px system-ui;margin:32px auto;padding:0 24px;max-width:1260px;color:#233044;background:#f6f8fb}button,input,textarea,select{font:inherit}input,textarea,select{border:1px solid #d4dce6;padding:9px;background:white;color:inherit}.cbi-button{border:1px solid #d4dce6;border-radius:8px;background:white;padding:8px 13px;cursor:pointer}.cbi-button-positive{background:#eaf2ff;border-color:#5595ff}.cbi-value{padding:10px 0}.cbi-value-title{display:block;padding-bottom:5px}.cbi-value-description{font-size:12px;opacity:.6}.cbi-input-textarea{box-sizing:border-box;width:100%}</style><main></main>');
 await page.evaluate(installDeviceWidgets);
 await page.evaluate(({ viewSource, pickerSource, config, servers }) => {
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
  window.config = config; window.servers = servers; window.refreshed = servers;
  window.probes = []; window.notices = []; window.saved = null; window.failRefresh = false;
  window.serviceCalls = []; window.serviceStatus = {instances:{xray:{running:true}},enabled:true};
  const api = { declare({ method }) { return async (...args) => {
   if (method === 'service') {
    window.serviceCalls.push(args[0]);
    if (window.holdService) await new Promise(resolve => { window.releaseService = resolve; });
    if (args[0] === 'stop' || args[0] === 'start') window.serviceStatus.instances.xray.running = args[0] === 'start';
    if (args[0] === 'disable' || args[0] === 'enable') window.serviceStatus.enabled = args[0] === 'enable';
    return {};
   }
   if (method === 'status') return structuredClone(window.serviceStatus);
   if (method === 'geodata') return {items:[
    {id:'geosite:dynamic-service',name:'dynamic-service',kind:'geosite',count:42,zapret:true},
    {id:'geoip:dynamic-service',name:'dynamic-service',kind:'geoip',count:3,zapret:true},
    {id:'mixed:dynamic-service',name:'dynamic-service',kind:'mixed',count:45,zapret:true},
    {id:'geosite:regex-demo',name:'regex-demo',kind:'geosite',count:2,zapret:false}
   ],sources:{geositeURL:'https://data.test/site.dat',geoipURL:'https://data.test/ip.dat'}};
   if (method === 'probe') {window.probes.push(structuredClone(args));if(window.probeErrors?.length)return {error:window.probeErrors.shift()}; if(window.holdProbe)await new Promise(resolve => {window.releaseProbe=resolve;}); return {results:Object.fromEntries(args[0].map(id => [id,window.probeResults?.[id] || {available:true,delayMs:42}]))};}
   if (method === 'nodes') return window.failRefresh ? { error: 'Source unavailable' } : { nodes: window.refreshed };
   if (method === 'check') return window.checkResult || { ok: true, steps: ['config','sources','routes','xray','nfqws','nftables'].map((id, i) => ({ id, title: ['Configuration','Subscriptions','Routes','Xray','Zapret','nftables'][i], state: 'ok', detail: 'Checked', durationMs: i + 1 })) };
   if (method === 'zaprettune')return {job:null};
   if (method === 'validate') {window.validated=structuredClone(args[0]);return {ok:true};}
   if (method === 'save') { if(window.saveError)return window.saveError; window.saved = structuredClone({ config: args[0], nodes: args[1] }); return { revision: 'new', servers: window.saveServers }; }
   return {};
  }; } };
  const picker = new Function('baseclass', 'E', pickerSource)({ extend: value => value }, E);
  const view = new Function('view', 'rpc', 'ui', 'poll', 'picker', 'E', 'L', viewSource)(
   { extend: value => value }, api, { addNotification(_, el) { window.notices.push(el.textContent); },
    showModal(title, content) { const modal = E('div', { role: 'dialog', 'aria-label': title }, [E('h3', {}, [title])].concat(content)); document.body.append(modal); },
    hideModal() { document.querySelector('[role=dialog]')?.remove(); } }, { add() {} }, picker, E,
   { resource: name => 'https://nadkop.test/luci-static/resources/' + (/^(?:[a-zA-Z0-9_.%,;-]+\/)*[a-zA-Z0-9_.%,;-]+$/.test(name) ? name : '') }
  );
  window.render = cfg => {
   window.config = cfg;
   const main = document.querySelector('main'); main.replaceChildren(view.render([{ config: cfg, nodes: [], servers: window.servers, revision: 'old' },
    { instances: { xray: { running: true }, nfqws: { running: true } }, enabled: true, runtimeChecks:[{title:'Xray',state:'ok',detail:'Xray process running under Nadkop service management.'},{title:'Zapret',state:'info',detail:'Long explanation for unused Zapret.'},{title:'Server selection',state:'error',detail:'Monitor not running.'}], devices: ['eth0', 'eth1'] }, {}]));
  };
  window.render(config);
 }, { viewSource, pickerSource, config, servers });
 await page.getByRole('tab', { name: 'Rules', exact: true }).click();
 const pool = page.getByRole('complementary', { name: 'Server picker' });
 const poolCard = id => pool.locator(`[data-server-id="${id}"]`);
 const rule = index => page.locator(`[data-rule-index="${index}"]`);
 assert.equal(await pool.locator('.nk-server-card').count(), 4);
 assert.equal(await poolCard(ids[0]).locator('.nk-flag img').getAttribute('src'), '/luci-static/resources/nadkop/flags/nl.svg');
 assert.equal(await rule(2).locator('.nk-server-card.is-missing').count(), 1);
 await rule(0).locator('.nk-rule-body').hover();
 assert.deepEqual(await rule(0).locator('.nk-rule-header-actions>button').evaluateAll(els=>els.map(el=>getComputedStyle(el).opacity)),['0','0','0','0'],'header actions remain visible');
 assert.equal(await rule(0).locator('.nk-rule-rename').evaluate(el=>getComputedStyle(el).opacity),'0');
 assert.equal(await rule(0).locator('.nk-rule-delete').evaluate(el=>getComputedStyle(el).opacity),'0');
 const headerBefore=await rule(0).locator('.nk-rule-title').boundingBox();
 await rule(0).locator('.nk-rule-head').hover();
 assert.deepEqual(await rule(0).locator('.nk-rule-header-actions>button').evaluateAll(els=>els.map(el=>getComputedStyle(el).opacity)),['1','1','1','1'],'hover does not reveal all header actions');
 assert.equal(await rule(0).locator('.nk-rule-rename').evaluate(el=>getComputedStyle(el).opacity),'1');
 assert.equal(await rule(0).locator('.nk-rule-delete').evaluate(el=>getComputedStyle(el).opacity),'1');
 assert.deepEqual(await rule(0).locator('.nk-rule-title').boundingBox(),headerBefore,'hover shifted header');
 await rule(0).locator('.nk-rule-body').hover();
 await page.keyboard.press('Tab');
 await rule(0).locator('.nk-rule-toggle').focus();
 assert.deepEqual(await rule(0).locator('.nk-rule-header-actions>button').evaluateAll(els=>els.map(el=>getComputedStyle(el).opacity)),['1','1','1','1'],'keyboard focus does not reveal all header actions');
 assert.equal(await rule(0).locator('.nk-rule-rename').evaluate(el=>getComputedStyle(el).opacity),'1','keyboard focus hides actions');
 await rule(0).locator('.nk-rule-toggle').blur();
 await rule(0).locator('.nk-rule-head').hover();
 await rule(0).getByRole('button',{name:'Collapse Rule 1',exact:true}).click();
 await rule(0).locator('.nk-rule-title').hover();
 await pool.hover();
 assert.deepEqual(await rule(0).locator('.nk-rule-header-actions>button').evaluateAll(els=>els.map(el=>getComputedStyle(el).opacity)),['0','0','0','0'],'mouse focus keeps header actions visible after leaving');
 await rule(0).locator('.nk-rule-toggle').click();
 assert.equal(await rule(1).locator('.nk-dropzone').count(), 0, 'empty direct rule shows a drop field');
 assert.equal(await rule(0).locator('.nk-dropzone').count(), 1);
 assert.equal(await rule(1).locator('.nk-matcher-switches input').count(),0);
 assert.equal(await rule(1).getByRole('button',{name:'IP / CIDR',exact:true}).getAttribute('aria-pressed'),'false');
 assert.equal(await rule(1).getByLabel('IP / CIDR',{exact:true}).isVisible(),false);
 assert.equal(await rule(1).getByRole('button',{name:'Add set',exact:true}).isVisible(),false);
 await rule(1).getByRole('button',{name:'IP / CIDR',exact:true}).click();
 await rule(1).getByRole('button',{name:'Sets',exact:true}).click();
 await rule(1).getByLabel('IP / CIDR',{exact:true}).fill('192.0.2.3\n2001:db8::/32');
 await rule(1).getByRole('button',{name:'Add set',exact:true}).click();
 await page.getByRole('searchbox',{name:'Search sets',exact:true}).fill('dynamic-service');
 await page.getByRole('dialog').getByRole('button',{name:/mixed:dynamic-service/}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Done',exact:true}).click();
 assert.deepEqual(await page.evaluate(() => window.config.rules[1].sets),['mixed:dynamic-service']);
 assert.deepEqual(await page.evaluate(() => window.config.rules[1].ips),['192.0.2.3','2001:db8::/32']);
 await rule(1).getByRole('button',{name:'Remove mixed:dynamic-service',exact:true}).click();
 await rule(1).getByLabel('IP / CIDR',{exact:true}).fill('');

 const domains = rule(1).getByLabel('Domains',{exact:true});
 await domains.fill('https://Work.Example/path?q=1#part http://other.test/abc third.test/a');
 await domains.blur();
 assert.equal(await domains.inputValue(), 'work.example\nother.test\nthird.test');
 await domains.fill('work.example'); await domains.blur();
 assert.equal(await rule(1).locator('[data-choose-rule]').count(), 0, 'direct rule shows server picker');
 assert.equal(await rule(1).locator('.nk-rule-body').isVisible(), true);
 for (const action of ['Zapret', 'Block', 'Direct']) {
  await rule(1).getByRole('button', { name:action, exact:true }).click();
  assert.equal(await rule(1).locator('[data-choose-rule]').count(), 0, action + ' shows server picker');
  assert.equal(await rule(1).locator('.nk-rule-body').isVisible(), true, 'changing action collapsed rule');
 }
 await rule(1).getByRole('button', { name:'VPN', exact:true }).click();
 assert.equal(await rule(1).getByRole('button', { name:'VPN', exact:true }).getAttribute('aria-pressed'), 'true', 'server action needs a selected connection');
 assert.equal(await page.evaluate(() => window.config.rules[1].action), 'proxy');
 assert.equal(await rule(1).getByRole('button', { name:'Select server', exact:true }).isEnabled(), true);
 await rule(1).getByRole('button', { name:'Select server', exact:true }).click();
 await poolCard(ids[2]).getByRole('button', { name:/^Select / }).click();
 await rule(1).getByRole('button', { name:'Remove server from rule', exact:true }).click();
 assert.equal(await page.evaluate(() => window.config.rules[1].action), 'proxy', 'removing last server resets action');
 assert.equal(await rule(1).getByRole('button', { name:'Select server', exact:true }).isEnabled(), true);
 await rule(1).getByRole('button', { name:'Direct', exact:true }).click();
 assert.equal(await rule(1).locator('[data-choose-rule]').count(), 0);
 assert.equal(await pool.locator('.nk-picker-target').isVisible(), false, 'picker still targets direct rule');
 assert.equal(await page.getByRole('heading', { name: 'Nadkop', exact: true }).count(), 1);
 assert.equal(await pool.locator('.nk-port').count(), 0, 'port remains visible');
 assert.equal(await pool.locator('.nk-location').count(), 0, 'country/city caption remains');
 const [caption, transport] = await Promise.all([poolCard(ids[0]).locator('.nk-source').boundingBox(), poolCard(ids[0]).locator('.nk-transport').boundingBox()]);
 assert.ok(caption.x > transport.x + transport.width, 'subscription caption is not on the right');
 assert.equal(await rule(0).locator('.nk-rule-name').count(), 0, 'rule name starts in editing mode');
 await rule(0).getByRole('button', { name:'Collapse Rule 1', exact:true }).click();
 assert.equal(await rule(0).locator('.nk-rule-body').isVisible(), false);
 await pool.getByRole('button', { name:'Refresh connections', exact:true }).click();
 await page.waitForFunction(() => document.querySelector('[data-rule-index="0"] .nk-rule-toggle').getAttribute('aria-expanded') === 'false');
 assert.equal(await rule(0).locator('.nk-rule-body').isVisible(), false, 'refresh expanded collapsed rule');
 await page.evaluate(() => window.render(window.config));
 assert.equal(await rule(0).locator('.nk-rule-body').isVisible(), false, 'view reload lost collapse preference');
 assert.equal(await rule(1).locator('.nk-rule-body').isVisible(), true, 'default rule is collapsed');
 await rule(0).getByRole('button', { name:'Expand Rule 1', exact:true }).click();
 await rule(0).locator('.nk-rule-head').hover();
 await rule(0).getByRole('button', { name:'Rename Rule 1', exact:true }).click();
 await rule(0).getByRole('textbox', { name:'Rule name 1', exact:true }).fill('Cancel');
 await page.keyboard.press('Escape');
 assert.equal(await rule(0).locator('.nk-rule-title').textContent(), 'Rule 1');
 await rule(0).locator('.nk-rule-head').hover();
 await rule(0).getByRole('button', { name:'Rename Rule 1', exact:true }).click();
 await rule(0).getByRole('textbox', { name:'Rule name 1', exact:true }).fill('Videos');
 await page.keyboard.press('Enter');
 assert.equal(await rule(0).locator('.nk-rule-title').textContent(), 'Videos');
 assert.equal(await rule(0).locator('.nk-rule-name').count(), 0);
 await rule(0).locator('.nk-rule-head').hover();
 await rule(0).getByRole('button', {name:'Rename Videos', exact:true}).click();
 await rule(0).getByRole('textbox', {name:'Rule name 1', exact:true}).fill('Videos');
 await rule(0).getByRole('button', {name:'Collapse Videos', exact:true}).click();
 assert.equal(await rule(0).locator('.nk-rule-body').isVisible(), false, 'blur swallowed collapse click');
 assert.equal(await rule(0).locator('.nk-rule-name').count(), 0);
 await rule(0).getByRole('button', {name:'Expand Videos', exact:true}).click();
 assert.match(await poolCard(ids[0]).locator('.nk-server-meta').textContent(), /VLESS.*TCP/);
 assert.match(await poolCard(ids[1]).locator('.nk-server-meta').textContent(), /TROJAN.*gRPC/);
 assert.equal(await pool.locator('.nk-provider-separator').count(), 2, 'provider headings missing');
 assert.equal(await pool.locator('.nk-provider-separator button, .nk-provider-separator [draggable=true]').count(), 0);
 await page.addStyleTag({ content: 'input[type="search"]{min-width:480px;width:480px}' });
 const assertSearchFits = async () => {
  const [search, field] = await Promise.all([page.getByRole('searchbox', { name: 'Search connections' }).boundingBox(), pool.locator('.nk-search-field').boundingBox()]);
  assert.ok(search.x >= field.x - 1 && search.x + search.width <= field.x + field.width + 1, 'search field overflows picker');
 };
 await assertSearchFits();
 assert.equal(await poolCard(ids[1]).locator('.nk-flag img').getAttribute('src'), '/luci-static/resources/nadkop/flags/de.svg', 'name flag overrides subscription metadata');
 assert.equal(await poolCard(ids[2]).locator('.nk-flag img').getAttribute('src'), '/luci-static/resources/nadkop/flags/fi.svg', 'country name is not resolved');
 assert.ok((await poolCard(ids[0]).boundingBox()).height <= 60, 'connection cards are too tall');
 assert.ok(await poolCard(ids[0]).locator('svg.nk-icon').count() >= 2, 'Lucide icons missing');
 const subscriptionFilter = page.getByRole('combobox', { name: 'Subscription filter' });
 await subscriptionFilter.selectOption('Europe');
 assert.equal(await pool.locator('.nk-server-card').count(), 3);
 assert.equal(await pool.locator('.nk-source').count(), 0, 'selected subscription caption is duplicated');
 assert.deepEqual(await pool.locator('.nk-picker-list > [data-server-id]').evaluateAll(els => els.map(el => el.getAttribute('data-server-id'))),
  ['srv_' + '5'.repeat(32), ids[0], ids[1], 'srv_' + '6'.repeat(32), ids[3]], 'provider order was overridden');
 await subscriptionFilter.selectOption('Nordic');
 assert.equal(await pool.locator('.nk-server-card').count(), 1);
 await subscriptionFilter.selectOption('');
 assert.equal(await pool.locator('.nk-server-card').count(), 4);
 // Scroll the rule into view before computing the source position: the
 // sticky sidebar changes position when Playwright scrolls a distant target.
 await rule(1).scrollIntoViewIfNeeded();
 await poolCard(ids[0]).dragTo(rule(1));
 assert.deepEqual(await page.evaluate(() => window.config.rules.slice(0, 2).map(r => r.nodeRef)), [ids[0], ids[0]]);
 assert.equal(await poolCard(ids[0]).count(), 1, 'assigning removed the source card');
 assert.equal(await rule(1).locator('.nk-dropzone').count(), 1, 'selected server drop field missing');
 assert.equal(await page.locator('.nk-rule.is-target,.nk-rule.is-over').count(), 0, 'drop outline remains after successful drop');
 // Cancelling a drag also clears the target outline.
 await page.locator('[data-choose-rule="1"]').click();
 const dragData = await page.evaluateHandle(() => { const data = new DataTransfer(); data.setData('application/x-nadkop-server', window.servers[0].id); return data; });
 await rule(1).dispatchEvent('dragover', { dataTransfer: dragData });
 assert.equal(await rule(1).evaluate(el => el.classList.contains('is-over')), true);
 await poolCard(ids[0]).dispatchEvent('dragend', { dataTransfer: dragData });
 await dragData.dispose();
 assert.equal(await page.locator('.nk-rule.is-target,.nk-rule.is-over').count(), 0, 'drop outline remains after cancelled drag');
 assert.equal(await rule(1).locator(`[data-server-id="${ids[0]}"]`).count(), 1);
 await poolCard(ids[1]).getByRole('button', { name: /^Pin / }).click();
 assert.deepEqual(await page.evaluate(() => window.config.picker.pinned), [ids[1]]);
 await poolCard(ids[2]).dragTo(poolCard(ids[1]));
 assert.deepEqual(await page.evaluate(() => window.config.picker.order), priorOrder, 'pin/card drag changed provider order');
 await page.locator('[data-choose-rule="1"]').click();
 await poolCard(ids[1]).getByRole('button', { name: /^Select / }).focus();
 await page.keyboard.press('Enter');
 assert.deepEqual(await page.evaluate(() => window.config.rules[1].nodeRefs), [ids[0], ids[1]]);
 assert.equal(await rule(0).getByLabel('Server selection', { exact:true }).count(), 0, 'single server shows check selector');
 assert.equal(await rule(1).getByLabel('Server selection', { exact:true }).inputValue(), 'priority');
 assert.deepEqual(await rule(1).locator('.nk-server-position').allTextContents(),['1','2']);
 for (const name of ['Remove server from rule','Move down server 1','Move up server 2']) {
  assert.ok(await rule(1).getByRole('button',{name,exact:true}).first().locator('svg.nk-icon').count());
 }
 const assigned = rule(1).locator('.nk-rule-servers>li').first();
 const [badgeBox,cardBox,removeBox,upBox] = await Promise.all([assigned.locator('.nk-probe-result').boundingBox(),assigned.locator('.nk-server-card').boundingBox(),assigned.getByRole('button',{name:'Remove server from rule',exact:true}).boundingBox(),assigned.getByRole('button',{name:'Move up server 1',exact:true}).boundingBox()]);
 assert.ok(badgeBox.y < cardBox.y + cardBox.height/2,'status is not in upper row');
 assert.ok(Math.abs(upBox.y+upBox.height/2-badgeBox.y-badgeBox.height/2)<2,'move arrows are not in upper status row');
 assert.ok(removeBox.y>=upBox.y+upBox.height,'remove is not below move arrows');
 assert.equal(await assigned.locator('.nk-probe-result').getAttribute('data-state'),'ok');

 await page.waitForFunction(() => document.querySelector('[data-rule-index="1"] .nk-probe-result')?.textContent === 'Available');
 await rule(1).getByRole('button', { name:'Move down server 1', exact:true }).click();
 assert.deepEqual(await page.evaluate(() => window.config.rules[1].nodeRefs), [ids[1], ids[0]]);
 await rule(1).locator('[data-server-id="' + ids[0] + '"]').dragTo(rule(1).locator('[data-server-id="' + ids[1] + '"]'));
 assert.deepEqual(await page.evaluate(() => window.config.rules[1].nodeRefs), [ids[0], ids[1]], 'drag within rule did not reorder servers');
 assert.equal(await page.locator('.nk-rule.is-over,.nk-rule.is-target').count(), 0, 'reordering retained drop outline');
 assert.equal(await rule(1).locator('.nk-selection-info').count(), 0);
 await rule(1).getByLabel('Server selection', { exact:true }).selectOption('url');
 await rule(1).getByLabel('Check URL', { exact:true }).fill('https://probe.test/204');
 assert.equal(await page.evaluate(() => window.config.rules[1].selection.url), 'https://probe.test/204');
 await page.waitForFunction(() => window.probes.some(args => args[1] === 'url' && args[2] === 'https://probe.test/204'));
 await page.waitForFunction(() => document.querySelector('[data-rule-index="1"] .nk-probe-result')?.textContent === '42 ms');
 await rule(1).getByRole('button', { name:'Remove server from rule', exact:true }).last().click();
 assert.equal(await rule(1).getByLabel('Server selection', { exact:true }).count(), 0, 'selector remains after reducing to one server');
 assert.equal(await rule(1).getByLabel('Check URL', { exact:true }).count(), 0);
 await page.locator('[data-choose-rule="1"]').click();
 await poolCard(ids[1]).getByRole('button', { name:/^Select / }).click();
 await rule(1).getByLabel('Server selection', { exact:true }).selectOption('ping');
 await page.waitForFunction(() => window.probes.some(args => args[1] === 'ping'));
 await page.waitForFunction(() => document.querySelector('[data-rule-index="1"] .nk-probe-result')?.textContent === '42 ms');
 // The collapsed list shows the selected candidate and falls back to the first before checks finish.
 await page.evaluate(ids => {window.holdProbe=true;window.probeResults={[ids[0]]:{available:true,delayMs:80},[ids[1]]:{available:true,delayMs:10}};},ids);
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('url');
 await page.waitForFunction(() => typeof window.releaseProbe === 'function');
 const expandedCard=await rule(1).locator('.nk-rule-servers>li').first().locator('.nk-server-card').boundingBox();
 const expandedRemove=await rule(1).locator('.nk-rule-servers>li').first().locator('.nk-unlink').boundingBox();
 await rule(1).getByRole('button',{name:'Collapse server list',exact:true}).click();
 const collapsedCard=await rule(1).locator('.nk-rule-servers>li:visible .nk-server-card').boundingBox();
 const collapsedMain=await rule(1).locator('.nk-rule-servers>li:visible .nk-server-main').boundingBox();
 assert.ok(Math.abs(collapsedMain.y+collapsedMain.height/2-collapsedCard.y-collapsedCard.height/2)<1,'server text is not vertically centred');
 const collapsedRemove=await rule(1).locator('.nk-rule-servers>li:visible .nk-unlink').boundingBox();
 assert.equal(collapsedCard.x,expandedCard.x,'collapse shifted card horizontally');
 assert.equal(collapsedCard.width,expandedCard.width,'collapse resized card');
 assert.ok(Math.abs(collapsedRemove.x+collapsedRemove.width-collapsedCard.x-collapsedCard.width)<2,'collapsed remove is not at the right edge');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-move-server:visible').count(),0,'move arrows still visible while collapsed');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-server-position').textContent(),'1 of 2');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible').count(),1);
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-server-card').getAttribute('data-server-id'),ids[0]);
 await page.evaluate(() => {window.holdProbe=false;window.releaseProbe();delete window.releaseProbe;});
 await page.waitForFunction(id => document.querySelector('[data-rule-index="1"] .nk-rule-servers>li:not([hidden]) .nk-server-card')?.getAttribute('data-server-id')===id,ids[1]);
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-probe-result').textContent(),'10 ms');
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('priority');
 await page.waitForFunction(id => document.querySelector('[data-rule-index="1"] .nk-rule-servers>li:not([hidden]) .nk-server-card')?.getAttribute('data-server-id')===id,ids[0]);
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('ping');
 await page.waitForFunction(id => document.querySelector('[data-rule-index="1"] .nk-rule-servers>li:not([hidden]) .nk-server-card')?.getAttribute('data-server-id')===id,ids[1]);
 await rule(1).getByRole('button',{name:'Refresh server checks',exact:true}).waitFor({state:'visible'});
 await page.waitForFunction(() => !document.querySelector('[data-rule-index="1"] .nk-server-refresh').disabled);
 await page.evaluate(() => {window.holdProbe=true;});
 const refreshChecks=rule(1).getByRole('button',{name:'Refresh server checks',exact:true});
 const addBox=await rule(1).locator('[data-choose-rule]').boundingBox(),refreshBox=await refreshChecks.boundingBox();
 assert.ok(refreshBox.x>addBox.x+addBox.width,'refresh is not right of add');
 assert.equal((await refreshChecks.textContent()).trim(),'');
 await refreshChecks.click();
 await page.waitForFunction(() => typeof window.releaseProbe==='function');
 assert.equal(await refreshChecks.getAttribute('aria-busy'),'true');
 assert.equal(await refreshChecks.isDisabled(),true);
 await page.evaluate(() => {window.holdProbe=false;window.releaseProbe();delete window.releaseProbe;});
 await page.waitForFunction(() => document.querySelector('[data-rule-index="1"] .nk-server-refresh').getAttribute('aria-busy')==='false');
 const beforeRetry=await page.evaluate(()=>window.probes.length);
 await page.evaluate(()=>{window.probeErrors=['Probe already running; retry in a few seconds'];});
 await refreshChecks.click();
 await page.waitForFunction(n=>window.probes.length>=n+2&&document.querySelector('[data-rule-index="1"] .nk-server-refresh').getAttribute('aria-busy')==='false',beforeRetry);
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-probe-result').textContent(),'10 ms');
 await page.evaluate(()=>{window.probeErrors=['Xray rejected the probe configuration'];});
 await refreshChecks.click();
 await page.waitForFunction(()=>document.querySelector('[data-rule-index="1"] .nk-server-refresh').getAttribute('aria-busy')==='false');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-probe-result').textContent(),'Xray launch error');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-probe-result').getAttribute('title'),'Xray rejected the probe configuration');
 await page.evaluate(() => {window.probeResults=null;});
 await page.evaluate(() => window.render(window.config));
 assert.equal(await rule(1).getByRole('button',{name:'Expand server list',exact:true}).getAttribute('aria-expanded'),'false');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible').count(),1,'reload lost collapsed preference');
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible .nk-server-card').getAttribute('data-server-id'),ids[0],'no check result should show the first server');

 await rule(1).getByRole('button',{name:'Expand server list',exact:true}).click();
 assert.equal(await rule(1).locator('.nk-rule-servers>li:visible').count(),2);
 await page.getByRole('searchbox', { name: 'Search connections' }).fill('HELSINKI');
 assert.equal(await pool.locator('.nk-server-card').count(), 1);
 await page.getByRole('searchbox', { name: 'Search connections' }).fill('Netherlands');
 assert.equal(await pool.locator('.nk-server-card').count(), 1);
 assert.equal(await pool.locator('.nk-server-card').getAttribute('data-server-id'), ids[0]);
 await page.getByRole('searchbox', { name: 'Search connections' }).fill('');
 if (process.env.NADKOP_PICKER_SCREENSHOTS) {
  await fs.mkdir(process.env.NADKOP_PICKER_SCREENSHOTS, { recursive: true });
  await page.screenshot({ path: path.join(process.env.NADKOP_PICKER_SCREENSHOTS, 'picker-desktop.png'), fullPage: true });
 }
 // A changed subscription retains ID references, pin order and missing cards.
 await page.evaluate(() => {
  window.refreshed = [...window.servers].reverse().map(s => ({ ...s, name: s.name + ' · Updated', available: s.id === window.servers[0].id ? false : s.available }));
 });
 await pool.getByRole('button', { name: 'Refresh connections', exact: true }).click();
 await page.waitForFunction(() => document.querySelector('.nk-picker').textContent.includes('Updated'));
 assert.deepEqual(await page.evaluate(() => window.config.rules.map(r => r.nodeRefs || [r.nodeRef])), [[ids[0]], [ids[0],ids[1]], [ids[3]]]);
 assert.equal(await rule(0).locator('.is-missing').count(), 1);
 assert.equal(await poolCard(ids[0]).getByRole('button', { name: /^Select / }).isDisabled(), true);
 await page.evaluate(() => { window.failRefresh = true; });
 await pool.getByRole('button', { name: 'Refresh connections', exact: true }).click();
 await page.waitForFunction(() => window.notices.includes('Source unavailable'));
 assert.equal(await pool.locator('.nk-server-card').count(), 4);
 await page.getByRole('button', { name: 'Save', exact: true }).click();
 await page.waitForFunction(() => !!window.saved);
 const saved = await page.evaluate(() => window.saved);
 assert.equal(saved.config.rules[0].name, 'Videos');
 assert.equal(saved.config.rules[1].selection.mode, 'ping');
 assert.deepEqual(saved.config.rules.map(r => r.nodeRefs || [r.nodeRef]), [[ids[0]], [ids[0],ids[1]], [ids[3]]]);
 assert.equal(JSON.stringify(saved.config.rules).includes('settings'), false);
 assert.deepEqual(saved.config.picker.pinned, [ids[1]]);
 assert.deepEqual(saved.config.picker.order, priorOrder);
 await page.evaluate(() => { window.servers = window.refreshed; window.render(window.saved.config); });
 await page.getByRole('tab', { name: 'Rules', exact: true }).click();
 assert.equal(await rule(0).locator('.nk-rule-title').textContent(), 'Videos');
 assert.equal(await rule(0).locator('.is-missing').count(), 1);
 assert.equal(await pool.locator('.nk-server-card').first().getAttribute('data-server-id'), ids[1]);
 await page.setViewportSize({ width: 390, height: 844 });
 await assertSearchFits();
 assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, 'mobile layout overflows');
 if (process.env.NADKOP_PICKER_SCREENSHOTS) await page.screenshot({ path: path.join(process.env.NADKOP_PICKER_SCREENSHOTS, 'picker-mobile.png'), fullPage: true });
 await page.getByRole('tab', { name: 'Settings', exact: true }).click();
 await page.getByText('Appearance', { exact: true }).click();
 await page.getByLabel('Emoji', { exact: true }).selectOption('system');
 await page.getByRole('tab', { name: 'Rules', exact: true }).click();
 assert.equal(await page.locator('.nk-emoji img').count(), 0, 'system style still creates external emoji requests');
 assert.ok(await page.locator('.nk-flag img').count()>0,'local flags must remain available without emoji fonts');
 await page.route('https://cdn.jsdelivr.net/**', route => route.abort());
 await page.getByRole('tab', { name: 'Settings', exact: true }).click();
 await page.getByLabel('Emoji', { exact: true }).selectOption('apple');
 await page.getByRole('tab', { name: 'Rules', exact: true }).click();
 await pool.scrollIntoViewIfNeeded();
 // Bypass decoded image memory cache: cached Apple images correctly remain
 // usable offline, so a previously unseen URL must exercise the error path.
 await pool.locator('.nk-flag img').first().evaluate(el => { el.loading = 'eager'; el.src = 'https://cdn.jsdelivr.net/nadkop-offline-test/forced.png'; });
 await page.waitForFunction(() => document.querySelector('.nk-picker .nk-flag img')?.style.display === 'none');
 assert.notEqual(await pool.locator('.nk-flag .nk-emoji-fallback').first().evaluate(el => getComputedStyle(el).visibility), 'hidden');
 assert.match(await pool.locator('.nk-flag .nk-emoji-fallback').first().textContent(), /[\u{1f1e6}-\u{1f1ff}]/u);
 // Saving immediately replaces the pool with the backend's cleaned catalogue.
 await page.setViewportSize({ width: 1360, height: 1020 });
 await page.evaluate(() => {
  window.saveServers = window.refreshed.filter(s => s.source !== 'Nordic');
  window.saved = null;
 });
 await page.getByRole('button', { name: 'Save', exact: true }).click();
 await page.waitForFunction(() => !!window.saved);
 await page.waitForFunction(() => document.querySelectorAll('.nk-picker .nk-server-card').length === 3);
 assert.equal(await poolCard(ids[2]).count(), 0, 'cleaned card remains until reload');
 assert.equal(await rule(0).locator('.is-missing').count(), 1, 'bound missing card was lost');
 assert.equal(await poolCard(ids[1]).count(), 1, 'pinned card was lost');
 await page.getByRole('button', {name:'Add rule', exact:true}).click();
 await rule(3).locator('.nk-rule-head').hover();
 await rule(3).locator('.nk-rule-delete').click();
 assert.equal(await page.getByRole('dialog').count(), 1);
 assert.equal(await page.evaluate(() => window.config.rules.length), 4);
 await page.getByRole('dialog').getByRole('button', {name:'Cancel',exact:true}).click();
 assert.equal(await page.evaluate(() => window.config.rules.length), 4);
 await rule(3).locator('.nk-rule-head').hover();
 await rule(3).locator('.nk-rule-delete').click();
 await page.getByRole('dialog').getByRole('button', {name:'Delete',exact:true}).click();
 assert.equal(await page.evaluate(() => window.config.rules.length), 3);
 // Settings explain their purpose inline; other tabs keep secondary help on request.
 assert.equal(await page.locator('.nk-app .cbi-tabmenu [role=tab][aria-selected=true]').count(), 1);
 assert.equal(await page.locator('.nk-picker-footer, .nk-app .cbi-value-description').count(), await page.locator('#nadkop-settings .cbi-value-description').count());
 const tooltip = page.locator('.nk-help[aria-label="Help: Connections"]');
 assert.equal(await tooltip.evaluate(el => el.tagName), 'SPAN');
 assert.match(await tooltip.getAttribute('data-tooltip'), /Select a rule/);
 const [helpBox,titleBox] = await Promise.all([tooltip.boundingBox(), pool.getByRole('heading', {name:'Connections',exact:true}).boundingBox()]);
 assert.ok(helpBox.x >= titleBox.x + titleBox.width && helpBox.x - titleBox.x - titleBox.width < 16, 'tooltip is not immediately after picker title');
 assert.equal(await page.locator('button.nk-help').count(), 0, 'help still uses buttons');
 await tooltip.hover();
 await tooltip.focus();
 assert.equal(await page.getByRole('dialog').count(), 0);
 await page.getByRole('tab', { name: 'Diagnostics', exact: true }).click();
 assert.equal(await page.getByRole('heading',{name:'Status',exact:true}).count(),1);
 assert.equal(await page.locator('.nk-runtime-checks>li>div p').count(),0);
 assert.equal(await page.locator('.nk-runtime-checks details[open]').count(),0);
 // Dangerous service actions require confirmation; safe actions remain immediate.
 await page.evaluate(() => { window.serviceCalls = []; });
 const stopService = page.getByRole('button', {name:'Stop',exact:true});
 await stopService.click();
 assert.equal(await page.getByRole('dialog',{name:'Stop the service?'}).count(),1);
 await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();
 assert.equal(await page.getByRole('dialog').count(),0);
 assert.deepEqual(await page.evaluate(() => window.serviceCalls),[]);
 assert.equal(await stopService.evaluate(el => el === document.activeElement),true);
 await stopService.click();
 await page.evaluate(() => { window.holdService = true; });
 // Retain the detached button to simulate a queued second click while RPC is pending.
 await page.getByRole('dialog').getByRole('button',{name:'Stop',exact:true}).evaluate(el => { el.click(); el.click(); window.serviceConfirm = el; });
 await page.waitForFunction(() => !!window.releaseService);
 await page.evaluate(() => window.serviceConfirm.click());
 assert.deepEqual(await page.evaluate(() => window.serviceCalls),['stop']);
 await page.evaluate(() => { window.holdService = false; window.releaseService(); });
 await page.getByRole('button',{name:'Start',exact:true}).click();
 assert.equal(await page.getByRole('dialog').count(),0);
 await page.getByRole('button',{name:'Restart',exact:true}).click();
 assert.equal(await page.getByRole('dialog').count(),0);
 await page.waitForFunction(() => window.serviceCalls.length === 3 && !document.querySelector('[data-service-action="disable"]').disabled);
 assert.deepEqual(await page.evaluate(() => window.serviceCalls),['stop','start','restart']);
 await page.getByRole('button',{name:'Disable autostart',exact:true}).click();
 assert.match(await page.getByRole('dialog').textContent(),/currently running service will continue/);
 await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();
 assert.equal(await page.getByRole('button',{name:'Disable autostart',exact:true}).evaluate(el => el === document.activeElement),true);
 assert.deepEqual(await page.evaluate(() => window.serviceCalls),['stop','start','restart']);
 await page.getByRole('button',{name:'Disable autostart',exact:true}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Disable',exact:true}).click();
 await page.getByRole('button',{name:'Enable autostart',exact:true}).click();
 assert.equal(await page.getByRole('dialog').count(),0);
 await page.waitForFunction(() => window.serviceCalls.length === 5);
 assert.deepEqual(await page.evaluate(() => window.serviceCalls),['stop','start','restart','disable','enable']);
 console.log('PASS: service confirmation, cancellation/focus, duplicate-click guard and immediate start/restart/enable');
 await page.getByRole('button', { name: 'Check', exact: true }).click();
 await page.waitForFunction(() => document.querySelectorAll('.nk-diagnostic-steps li').length === 6);
 assert.equal(await page.locator('.nk-diagnostic-steps [data-state="ok"]').count(), 6);
 assert.equal(await page.locator('.nk-diagnostic-steps details[open]').count(), 0);
 await page.locator('.nk-diagnostic-steps summary').first().click();
 assert.equal(await page.locator('.nk-diagnostic-steps details[open]').count(), 1);
 await page.evaluate(() => { window.checkResult = { ok: false, error: 'Xray rejected configuration', steps: [
  { id:'config',title:'Configuration',state:'ok',detail:'Checked' },
  { id:'xray',title:'Xray',state:'error',detail:'Xray rejected configuration' },
  { id:'nfqws',title:'Zapret',state:'skipped',detail:'Previous step failed' }
 ] }; });
 await page.getByRole('button', { name: 'Check', exact: true }).click();
 await page.waitForFunction(() => document.querySelector('.nk-diagnostic-steps [data-state="error"]')?.open);
 assert.equal(await page.locator('.nk-diagnostic-steps [data-state="skipped"]').count(), 1);
 await page.getByRole('tab', { name: 'Rules', exact: true }).click();
 // Switching away from VPN and URL checks preserves dormant selections.
 const retainedRefs=await page.evaluate(()=>structuredClone(window.config.rules[1].nodeRefs));
 await rule(1).getByRole('button',{name:'Direct',exact:true}).click();
 assert.deepEqual(await page.evaluate(()=>window.config.rules[1].nodeRefs),retainedRefs);
 await rule(1).getByRole('button',{name:'VPN',exact:true}).click();
 assert.deepEqual(await page.evaluate(()=>window.config.rules[1].nodeRefs),retainedRefs);
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('url');
 await rule(1).getByLabel('Check URL',{exact:true}).fill('https://preserve.test/204');
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('ping');
 assert.equal(await page.evaluate(()=>window.config.rules[1].selection.url),'https://preserve.test/204');
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('url');
 assert.equal(await rule(1).getByLabel('Check URL',{exact:true}).inputValue(),'https://preserve.test/204');
 await rule(1).getByLabel('Server selection',{exact:true}).selectOption('ping');
 // Rule validation points at the current editor and does not write invalid drafts.
 const validDraft=await page.evaluate(()=>structuredClone(window.saved.config));
 await page.evaluate(()=>{const cfg=structuredClone(window.saved.config);cfg.rules[1]={domains:[],action:'block'};window.saved=null;window.render(cfg);});
 await page.getByRole('button',{name:'Save',exact:true}).click();
 assert.equal(await page.evaluate(()=>window.saved),null,'empty draft reached save RPC');
 assert.ok((await rule(1).locator('.nk-rule-error').textContent()).includes('single field is enough'));
 assert.equal(await rule(0).locator('.nk-rule-error').count(),0,'wrong rule marked invalid');
 assert.equal(await rule(1).getByRole('button',{name:'Domains',exact:true}).getAttribute('aria-invalid'),'true');
 await rule(1).getByRole('button',{name:'Domains',exact:true}).click();
 await rule(1).getByLabel('Domains',{exact:true}).fill('fixed.test');
 await rule(1).getByRole('button',{name:'Domains',exact:true}).click();
 assert.deepEqual(await page.evaluate(()=>window.config.rules[1].domains),['fixed.test']);
 assert.equal(await page.evaluate(()=>window.config.rules[1].conditions.domains),false);
 assert.equal(await rule(1).getByLabel('Domains',{exact:true}).isVisible(),false);
 await rule(1).getByRole('button',{name:'Domains',exact:true}).click();
 assert.equal(await rule(1).getByLabel('Domains',{exact:true}).inputValue(),'fixed.test');
 assert.equal(await rule(1).locator('.nk-rule-error').count(),0,'stale empty-rule message survived edit');
 await page.evaluate(()=>{window.config.rules[1].domains=[];});
 await rule(1).getByRole('button',{name:'Sets',exact:true}).click();
 await rule(1).getByRole('button',{name:'Add set',exact:true}).click();
 await page.getByRole('searchbox',{name:'Search sets',exact:true}).fill('regex-demo');
 await page.getByRole('dialog').getByRole('button',{name:/geosite:regex-demo/}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Done',exact:true}).click();
 assert.equal(await rule(1).getByRole('button',{name:'Zapret',exact:true}).isDisabled(),true);
 assert.ok((await rule(1).getByRole('button',{name:'Zapret',exact:true}).getAttribute('title')).includes('Xray (VPN) only.'));
 assert.equal(await rule(1).locator('.nk-action-reason').isVisible(),false,'unselected action shows a persistent warning');
 await rule(1).getByRole('button',{name:'Sets',exact:true}).click();
 await rule(1).getByRole('button',{name:'Zapret',exact:true}).click();
 assert.equal(await rule(1).getByRole('button',{name:'Sets',exact:true}).isDisabled(),true);
 assert.deepEqual(await page.evaluate(()=>window.config.rules[1].sets),['geosite:regex-demo']);
 await rule(1).getByRole('button',{name:'Direct',exact:true}).click();
 await rule(1).getByRole('button',{name:'Sets',exact:true}).click();
 assert.equal(await rule(1).locator('.nk-rule-error').count(),0);
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.waitForFunction(()=>!!window.saved);
 assert.deepEqual(await page.evaluate(()=>window.saved.config.rules[1].sets),['geosite:regex-demo'],'set-only draft lost selector');
 await page.evaluate(()=>{window.saveError={error:'Rule 2: Invalid IP',message:'Invalid IP/CIDR "999.1.1.1".',ruleIndex:1,field:'ips'};});
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('[data-rule-index="1"] .nk-rule-error')?.textContent.includes('999.1.1.1'));
 assert.equal(await rule(1).getByLabel('IP / CIDR',{exact:true}).getAttribute('aria-invalid'),'true');
 await page.evaluate(cfg=>{window.saveError=null;window.saved={config:cfg};window.render(cfg);},validDraft);
 if (process.env.NADKOP_LUCI_THEME_ROOT) {
  const themeRoot = process.env.NADKOP_LUCI_THEME_ROOT;
  await page.evaluate(() => {
   const cfg = structuredClone(window.saved.config);
   cfg.sources = [{ name:'Europe', url:'https://provider.test/sub', hwid:{ enabled:true } }, { name:'Nordic', file:'/etc/nadkop/local.json' }];
   window.render(cfg);
  });
  const themeFiles = {
   Bootstrap: ['luci-theme-bootstrap/htdocs/luci-static/bootstrap/cascade.css', 'luci-theme-bootstrap/htdocs/luci-static/bootstrap/mobile.css'],
   Material: ['luci-theme-material/htdocs/luci-static/material/cascade.css', 'luci-theme-material/htdocs/luci-static/material/custom.css'],
   OpenWrt2020: ['luci-theme-openwrt-2020/htdocs/luci-static/openwrt2020/cascade.css']
  };
  await page.evaluate(() => { document.querySelectorAll('style').forEach(el => el.remove()); document.documentElement.lang = 'en'; const main = document.querySelector('main'); main.id = 'maincontent'; const container = E('div', { 'class':'container' }, []); while (main.firstChild) container.append(main.firstChild); main.append(container); });
  for (const [theme, files] of Object.entries(themeFiles)) {
   const themeCSS = (await Promise.all(files.map(file => fs.readFile(path.join(themeRoot, file), 'utf8')))).join('\n').replace(/@import[^;]+;/g, '');
   const skin = await page.addStyleTag({ content: themeCSS });
   const appStyle = await page.addStyleTag({ content: css });
   for (const width of [1360, 390]) {
    await page.setViewportSize({ width, height: 950 });
    for (const tab of ['Rules', 'Subscriptions', 'Settings', 'Import', 'Diagnostics']) {
     await page.getByRole('tab', { name: tab, exact: true }).click();
     await page.locator('[role=tabpanel]:not([hidden]) details').evaluateAll(els => els.forEach(el => { el.open = true; }));
     const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
     if (overflow > 1) console.log(await page.evaluate(() => Array.from(document.querySelectorAll('body,main,.nk-app,.nk-app *')).filter(el => el.getBoundingClientRect().width && el.getBoundingClientRect().right > innerWidth + 1).slice(0, 12).map(el => ({ tag:el.tagName, cls:el.className, x:el.getBoundingClientRect().x, width:el.getBoundingClientRect().width, box:getComputedStyle(el).boxSizing }))));
     assert.ok(overflow <= 1, theme + ' ' + width + ' ' + tab + ': horizontal overflow ' + overflow);
     if (tab === 'Rules') {
      const headerButtons=await rule(0).locator('.nk-rule-header-actions>button').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect(),s=getComputedStyle(el);return {width:r.width,height:r.height,y:r.y,cls:el.className,font:s.fontSize,padding:s.padding,cssWidth:s.width};}));
      for(const button of headerButtons) {
       assert.ok(Math.abs(button.width-button.height)<1,theme+': header button is not square: '+JSON.stringify(button));
       assert.ok(Math.abs(button.height-headerButtons[0].height)<1&&Math.abs(button.y-headerButtons[0].y)<1,theme+': header buttons differ in size/alignment');
      }
      const [addRefreshBox,ruleRefreshBox,poolRefreshBox]=await Promise.all([rule(1).locator('[data-choose-rule]').boundingBox(),rule(1).locator('.nk-server-refresh').boundingBox(),pool.locator('.nk-pool-refresh').boundingBox()]);
      assert.ok(Math.abs(addRefreshBox.height-ruleRefreshBox.height)<1,theme+': refresh/add heights differ: '+JSON.stringify([addRefreshBox,ruleRefreshBox]));
      assert.ok(Math.abs(poolRefreshBox.height-ruleRefreshBox.height)<1,theme+': connection refresh height differs: '+JSON.stringify([addRefreshBox,ruleRefreshBox,poolRefreshBox]));
      assert.equal((await pool.locator('.nk-pool-refresh').textContent()).trim(),'');
      await assertSearchFits();
      for(const row of await rule(1).locator('.nk-rule-servers>li:visible').all()) {
       const [title,protocol,status,source]=await Promise.all(['.nk-server-title-row','.nk-server-meta','.nk-probe-result','.nk-assigned-footer .nk-source'].map(selector=>row.locator(selector).boundingBox()));
       if(status.height)assert.ok(Math.abs(title.y+title.height/2-status.y-status.height/2)<1,theme+': name/status rows differ');
       assert.ok(Math.abs(protocol.y+protocol.height/2-source.y-source.height/2)<1,theme+': protocol/source rows differ');
      }
      const pin=pool.locator('.nk-card-controls:has(.nk-pin):has(.nk-grip)').first();
      const [pinBox,gripBox]=await Promise.all([pin.locator('.nk-pin').boundingBox(),pin.locator('.nk-grip').boundingBox()]);
      assert.ok(Math.abs(pinBox.x+pinBox.width/2-gripBox.x-gripBox.width/2)<1,theme+': pin is not centred over drag handle');
      const serverTop=await pin.locator('..').locator('.nk-server-main').boundingBox();
      assert.ok(Math.abs(pinBox.y-serverTop.y)<1,theme+': pin is below the server top edge');
      if (width === 1360) {
       const [toolbar, add, choose, actions, controls] = await Promise.all([rule(1).locator('.nk-server-toolbar').boundingBox(), rule(1).locator('.nk-server-buttons').boundingBox(), rule(1).getByLabel('Server selection', {exact:true}).boundingBox(), rule(1).locator('.nk-rule-actions').boundingBox(), rule(1).locator('.nk-server-toolbar .cbi-value-field').boundingBox()]);
       assert.ok(toolbar.y >= actions.y + actions.height, theme + ': toolbar is above action');
       assert.ok(Math.abs(choose.x - actions.x) <= 2, theme + ': server selection does not align with action column');
       assert.ok(Math.abs(add.y + add.height/2 - choose.y - choose.height/2) <= 2, theme + ': selector and add are not aligned');
       assert.ok(Math.abs(add.x + add.width - controls.x - controls.width) <= 2, theme + ': add button is not at the right edge');
      }
     }
    }
   }
   // App controls must retain the skin's own colours and corner shapes.
   await page.evaluate(() => { document.querySelector('#nadkop-settings').hidden = false; });
   const styled = await page.evaluate(() => {
    const probe = E('button', { 'class': 'cbi-button cbi-button-neutral', type: 'button' }, ['Probe']); document.querySelector('.nk-app').append(probe);
    const native = getComputedStyle(probe), actual = getComputedStyle(document.querySelector('.nk-picker button.nk-server-main.cbi-button-neutral:not(:disabled)'));
    const result = { native: [native.backgroundColor, native.borderRadius, native.color], actual: [actual.backgroundColor, actual.borderRadius, actual.color] }; probe.remove(); return result;
   });
   assert.deepEqual(styled.actual, styled.native, theme + ': card overrides skin colours or corners');
   console.log('PASS: ' + theme + ' native controls, five tabs, desktop/mobile and search bounds');
   await skin.evaluate(el => el.remove()); await appStyle.evaluate(el => el.remove());
  }
 }
 assert.deepEqual(failures, []);
 console.log('PASS: Chromium drag/drop, keyboard, compact cards, subscription location/flags/city search, conditional drop fields, subscription filters, Lucide icons, provider order/separators, protocol/transport, right-aligned source, rule names, diagnostics, drag cleanup, native tooltip hooks, source caption filtering, search bounds, pins, refresh/missing/error retention, saved references, mobile layout, CDN fallback and system emoji');
} catch (error) {
 if (process.env.NADKOP_PICKER_SCREENSHOTS) { await fs.mkdir(process.env.NADKOP_PICKER_SCREENSHOTS, { recursive: true }); await page.screenshot({ path: path.join(process.env.NADKOP_PICKER_SCREENSHOTS, 'picker-failure.png'), fullPage: true }); }
 throw error;
} finally { await browser.close(); }

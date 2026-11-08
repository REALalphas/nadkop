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
  window.editor = { sources: [{ name: 'Existing', url: 'https://existing.test/sub', hwid: { enabled: true } }], rules: [{ domains: ['saved.example'], action: 'proxy', nodeRef: ref }], picker: { pinned: [ref], order: [ref], emojiStyle: 'system' }, default: 'direct', socksPort: 1080, zapretArgs: [] };
  window.disk = structuredClone(window.editor);
  window.summaries = [{ id: ref, source: 'Existing', name: 'Existing server', tag: 'Existing/main', protocol: 'trojan', available: true }];
  window.calls = []; window.notices = []; window.saved = null;
  let revision = 'r1';
  const api = { declare({ method }) { return async (...args) => {
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
   if (method === 'save') {
    if (args[2] !== revision) return { error: 'Stale save revision' };
    window.saved = structuredClone({ config: args[0], nodes: args[1] }); return { revision: 'saved' };
   }
   if (method === 'zaprettune')return {job:null};
   return {};
  }; } };
  const picker = new Function('baseclass', 'E', pickerSource)({ extend: value => value }, E);
  const view = new Function('view', 'rpc', 'ui', 'poll', 'picker', 'E', 'L', viewSource)(
   { extend: value => value }, api, { addNotification(_, el) { window.notices.push(el.textContent); },
    showModal(title,content){document.body.append(E('div',{role:'dialog','aria-label':title},content));},
    hideModal(){document.querySelector('[role=dialog]')?.remove();} }, { add() {} }, picker, E,
   { resource: name => 'https://nadkop.test/luci-static/resources/' + name }
  );
  document.querySelector('main').append(view.render([{ config: window.editor, nodes: [], servers: window.summaries, revision }, { instances: {}, devices: ['eth0'] }, { id: 'device-hwid-value' }]));
 }, { viewSource, pickerSource });
 // Keep an unsaved rule edit while a source commits independently.
 await page.getByRole('tab', { name: 'Rules', exact: true }).click();
 await page.locator('[data-rule-index="0"]').getByLabel('Domains',{exact:true}).fill('draft.example');
 await page.getByRole('tab', { name: 'Subscriptions', exact: true }).click();
 assert.equal(await page.getByRole('button', { name: 'Add source', exact: true }).count(), 0);
 const input = page.getByRole('textbox', { name: 'Link or file', exact: true });
 const status = page.locator('.nk-source-message');
 // Paste schedules automatic loading; overlapping Enter/blur must not duplicate it.
 await input.evaluate(el => { el.dispatchEvent(new Event('paste', { bubbles: true })); el.value = 'https://provider.test/direct.json'; el.dispatchEvent(new Event('input', { bubbles: true })); });
 await page.waitForFunction(() => window.calls.length === 1);
 assert.equal(await input.isDisabled(), true);
 await input.evaluate(el => el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
 await page.waitForFunction(() => document.querySelector('.nk-source-message').textContent.includes('Added:'));
 assert.equal(await input.inputValue(), '');
 assert.equal(await page.evaluate(() => window.calls.length), 1);
 assert.equal(await page.evaluate(() => window.disk.rules[0].domains[0]), 'saved.example');
 assert.equal(await page.evaluate(() => window.editor.rules[0].domains[0]), 'draft.example');
 assert.equal(await page.evaluate(() => window.editor.rules[0].nodeRef), 'srv_' + '1'.repeat(32));
 assert.equal(await page.evaluate(() => window.editor.sources[0].hwid.enabled), true);
 assert.equal(await page.evaluate(() => window.calls[0][2].hwid.enabled), true, 'HWID is not enabled by default for new subscriptions');
 assert.match(await status.textContent(), /New_VPN_-_EU-/);
 await page.getByRole('button', {name:'Rename subscription Existing',exact:true}).click();
 await page.getByRole('textbox', {name:'Subscription name',exact:true}).fill('My subscription');
 await page.getByRole('dialog').getByRole('button', {name:'Save',exact:true}).click();
 assert.equal(await page.evaluate(() => window.editor.sources[0].title), 'My subscription');
 assert.equal(await page.evaluate(() => window.editor.sources[0].name), 'Existing');
 assert.equal(await page.locator('.nk-source-heading').first().textContent().then(s => s.includes('My subscription')), true);
 // Same URL produces no duplicate source.
 await input.fill('https://provider.test/direct.json');
 await input.press('Enter');
 await page.waitForFunction(() => document.querySelector('.nk-source-message').textContent.includes('Already added:'));
 assert.equal(await page.evaluate(() => window.editor.sources.length), 2);
 // Failures retain the address and all previous selections for correction/retry.
 await input.fill('https://broken.test/direct.json');
 await input.press('Enter');
 await page.waitForFunction(() => document.querySelector('.nk-source-message').classList.contains('is-error'));
 assert.equal(await input.inputValue(), 'https://broken.test/direct.json');
 assert.equal(await page.evaluate(() => window.editor.sources.length), 2);
 // Local file: Enter/blur/change all use the same insertion path.
 await input.fill('/etc/nadkop/local-nodes.json');
 await input.blur();
 await page.waitForFunction(() => window.editor.sources.length === 3 && !document.querySelector('.nk-source-input').disabled);
 assert.equal(await page.evaluate(() => window.editor.sources[2].file), '/etc/nadkop/local-nodes.json');
 if (process.env.NADKOP_PICKER_SCREENSHOTS) {
  await fs.mkdir(process.env.NADKOP_PICKER_SCREENSHOTS, { recursive: true });
  await page.screenshot({ path: path.join(process.env.NADKOP_PICKER_SCREENSHOTS, 'subscriptions-desktop.png'), fullPage: true });
 }
 await page.setViewportSize({ width: 390, height: 844 });
 assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
 if (process.env.NADKOP_PICKER_SCREENSHOTS) await page.screenshot({ path: path.join(process.env.NADKOP_PICKER_SCREENSHOTS, 'subscriptions-mobile.png'), fullPage: true });
 await page.getByRole('button', { name: 'Save', exact: true }).click();
 await page.waitForFunction(() => window.saved !== null);
 assert.equal(await page.evaluate(() => window.saved.config.sources.length), 3);
 assert.equal(await page.evaluate(() => window.saved.config.rules[0].domains[0]), 'draft.example');
 assert.deepEqual(await page.evaluate(() => window.saved.config.picker.pinned), ['srv_' + '1'.repeat(32)]);
 assert.equal(await page.evaluate(() => window.saved.config.sources.some(s => '_headers' in s)), false);
 assert.deepEqual(failures, []);
 console.log('PASS: automatic subscription paste/Enter/blur, single request, title name, duplicate/error handling, local JSON, independent source commit, unsaved rule/reference retention, revision-aware save and mobile layout');
} finally { await browser.close(); }

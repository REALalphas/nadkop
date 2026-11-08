// S6-only browser adapters. No production source is rewritten or instrumented.
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';

export const s6Root = path.resolve(import.meta.dirname, '../..');
export const s6AssetRoot = path.join(process.env.S6_SOURCE_ROOT || s6Root, 'openwrt/luci-app-nadkop/htdocs/luci-static/resources');
export const s6ThemeRoot = process.env.S6_LUCI_THEME_ROOT || path.join(s6Root, 'build/openwrt-vm/sdk-24.10.8/feeds/luci/themes');
export const s6RPCPath = process.env.S6_LUCI_RPC || path.join(s6Root, 'build/openwrt-vm/sdk-24.10.8/feeds/luci/modules/luci-base/htdocs/luci-static/resources/rpc.js');
export const s6Origin = 'https://s6.nadkop.test';
export const s6IDs = ['1', '2', '3'].map(s6Value => 'srv_' + s6Value.repeat(32));
export const s6Themes = {
  Bootstrap: ['luci-theme-bootstrap/htdocs/luci-static/bootstrap/cascade.css', 'luci-theme-bootstrap/htdocs/luci-static/bootstrap/mobile.css'],
  Material: ['luci-theme-material/htdocs/luci-static/material/cascade.css', 'luci-theme-material/htdocs/luci-static/material/custom.css'],
  OpenWrt2020: ['luci-theme-openwrt-2020/htdocs/luci-static/openwrt2020/cascade.css']
};

export function s6InitialState() {
  return {
    revision: 's6-r1', generation: 1, writes: [], actions: [],
    config: {
      sources: [{ name: 'S6-Europe', url: 'https://synthetic-source.s6.test/sub', hwid: { enabled: false } }],
      rules: [
        { name: 'S6 route', domains: ['saved.s6.test'], ips: ['999.0.0.1', '2001:db8::/33'], sets: ['geosite:s6-dormant'],
          conditions: { domains: true, ips: false, sets: false }, action: 'proxy', nodeRefs: s6IDs.slice(0, 2),
          selection: { mode: 'url', url: 'https://health.s6.test/204?draft=retained' } },
        { name: 'S6 second', domains: ['second.s6.test'], action: 'direct' }
      ],
      picker: { pinned: [s6IDs[0]], order: [s6IDs[1], s6IDs[0]], emojiStyle: 'system' },
      default: 'direct', socksPort: 1080, zapretArgs: [], updateIntervalHours: 6,
      transparent: { enabled: false, interfaces: ['br-lan'], ipv6: true }
    },
    nodes: [],
    servers: [
      { id: s6IDs[0], source: 'S6-Europe', name: 'S6 Amsterdam', protocol: 'vless', transport: 'raw', available: true, position: 0, location: { countryCode: 'NL' } },
      { id: s6IDs[1], source: 'S6-Europe', name: 'S6 Frankfurt', protocol: 'trojan', transport: 'grpc', available: true, position: 1, location: { countryCode: 'DE' } },
      { id: s6IDs[2], source: 'S6-Europe', name: 'S6 spare', protocol: 'shadowsocks', transport: 'ws', available: true, position: 2 }
    ],
    status: { enabled: true, instances: { xray: { running: true } }, zapretRequired: false,
      runtimeChecks: [{ title: 'Xray', state: 'ok', detail: 'Synthetic S6 service' }] }
  };
}

function s6InstallBrowserAdapters({ viewSource, pickerSource, rpcSource, rpcTimeout }) {
  // E() has the flat-children rule of the established fixtures.
  window.E = function s6E(tag, attrs, children) {
    const element = document.createElement(tag);
    for (const [key, value] of Object.entries(attrs || {})) {
      if (value == null) continue;
      if (typeof value === 'function') element.addEventListener(key, value);
      else element.setAttribute(key, value === true ? '' : String(value));
    }
    for (const value of children || []) {
      if (Array.isArray(value)) throw new Error('S6: LuCI E() children must be flat');
      if (value != null) element.append(value instanceof Node ? value : document.createTextNode(String(value)));
    }
    return element;
  };
  // The real LuCI rpc.js uses LuCI's %s/%d String.format helper.
  String.prototype.format = function s6Format(...args) {
    let index = 0;
    return String(this).replace(/%([sd%])/g, (_, type) => type === '%' ? '%' : String(args[index++]));
  };
  window._ = value => value;
  const L = {
    env: { sessionid: '00000000000000000000000000000000', rpctimeout: rpcTimeout },
    url: value => '/' + value, resource: value => '/luci-static/resources/' + (/^(?:[a-zA-Z0-9_.%,;-]+\/)*[a-zA-Z0-9_.%,;-]+$/.test(value) ? value : ''),
    isObject: value => value !== null && typeof value === 'object' && !Array.isArray(value),
    bind: (fn, ctx, ...args) => fn.bind(ctx, ...args),
    raise(type, message, ...args) { const error = new Error(message.format(...args)); error.name = type; throw error; }
  };
  window.L = L;
  // Only request.post is adapted: actual rpc.declare/expect/reject and parsing
  // run unchanged. No full LuCI request batching/session-expiry UI is claimed.
  const request = { async post(url, message, options) {
    const controller = new AbortController();
    let timedOut = false;
    const timer = setTimeout(() => { timedOut = true; controller.abort(); }, options.timeout);
    try {
      const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(message), signal: controller.signal });
      const body = await response.text();
      return { ok: response.ok, status: response.status, statusText: response.statusText, json: () => JSON.parse(body) };
    } catch (error) {
      if (timedOut) throw new Error('S6 RPC timed out after ' + options.timeout + ' ms');
      throw error;
    } finally { clearTimeout(timer); }
  } };
  const rpc = new Function('baseclass', 'request', 'L', '_', rpcSource)({ extend: value => value }, request, L, window._);
  window.s6rpc = rpc;
  window.s6poll = [];
  window.s6notices = [];
  window.s6unhandled = [];
  window.addEventListener('unhandledrejection', event => window.s6unhandled.push(String(event.reason?.stack || event.reason)));
  // Standalone DeviceSelect adapter; it verifies view serialization, not the
  // actual LuCI dropdown's layout/keyboard handling.
  window.widgets = { DeviceSelect: class S6DeviceSelect {
    renderWidget(section, index, value) {
      const select = document.createElement('select'); select.multiple = this.multiple; select.id = 's6-device-' + this.key;
      const values = this.multiple ? value : [value];
      for (const name of [...new Set(['', 'br-lan', 'guest', 'eth0', ...values])]) {
        const option = new Option(name || 'Not specified', name, false, values.includes(name)); select.add(option);
      }
      select.addEventListener('change', () => {
        const names = [...select.selectedOptions].map(option => option.value).filter(Boolean);
        select.dispatchEvent(new CustomEvent('cbi-dropdown-change', { detail: { instance: { getValue: () => this.multiple ? names : names[0] } } }));
      });
      return select;
    }
  } };
  window.form = { NamedSection: class S6NamedSection {}, JSONMap: class S6JSONMap {
    section() { return { option(Widget, key) { const option = new Widget(); option.key = key; return option; } }; }
  } };
  const ui = {
    addNotification(_, element, type) {
      window.s6notices.push({ message: element.textContent, type });
      const notice = E('div', { role: 'alert', 'data-s6-notification': '' }, [element]); document.querySelector('#s6-notices').append(notice);
    },
    showModal(title, content) {
      ui.hideModal();
      const dialog = E('div', { role: 'dialog', 'aria-label': title, tabindex: '-1' }, [E('h3', {}, [title]), ...content]);
      document.body.append(dialog); dialog.focus();
    },
    hideModal() { document.querySelector('[role=dialog]')?.remove(); }
  };
  const picker = new Function('baseclass', 'E', pickerSource)({ extend: value => value }, E);
  window.s6view = new Function('view', 'rpc', 'ui', 'poll', 'picker', 'E', 'L', 'network', viewSource)(
    { extend: value => value }, rpc, ui, { add(fn) { window.s6poll.push(fn); } }, picker, E, L, { getDevices: async () => [] }
  );
  window.s6mount = async function s6Mount() {
    const data = await window.s6view.load();
    window.s6editor = data[0].config;
    document.querySelector('main .container').append(window.s6view.render(data));
  };
}

export async function s6ReadSources() {
  const [viewSource, pickerSource, css, rpcSource] = await Promise.all([
    fs.readFile(path.join(s6AssetRoot, 'view/nadkop/overview.js'), 'utf8'),
    fs.readFile(path.join(s6AssetRoot, 'nadkop/picker.js'), 'utf8'),
    fs.readFile(path.join(s6AssetRoot, 'nadkop/picker.css'), 'utf8'), fs.readFile(s6RPCPath, 'utf8')
  ]);
  return { viewSource, pickerSource, css, rpcSource };
}

export async function s6OpenFixture(browser, sources, options = {}) {
  const state = options.state || s6InitialState();
  const context = options.context || await browser.newContext({ viewport: { width: 1360, height: 950 }, colorScheme: 'light', serviceWorkers: 'block' });
  const page = await context.newPage(); page.setDefaultTimeout(4000);
  const log = [], faults = new Map(), pendingGates = new Set(), errors = [], unexpectedRequests = [];
  page.on('pageerror', error => errors.push(String(error.stack || error)));
  page.on('console', message => { if (message.type() === 'error') log.push({ at: Date.now(), event: 'console-error', text: message.text() }); });
  const fixture = {
    page, context, state, log, errors, unexpectedRequests,
    s6Fault(method, fault) { const queue = faults.get(method) || []; queue.push(fault); faults.set(method, queue); return fault; },
    s6Gate(method, spec = {}) {
      let release;
      const hold = new Promise(resolve => { release = resolve; });
      const fault = { ...spec, hold, release() { release(); pendingGates.delete(fault); } };
      pendingGates.add(fault); this.s6Fault(method, fault); return fault;
    },
    s6Count(method) { return log.filter(event => event.event === 'request' && event.method === method).length; },
    s6Calls(method) { return log.filter(event => event.event === 'request' && event.method === method); },
    async s6WaitCall(method, count = 1) {
      const start = Date.now();
      while (this.s6Count(method) < count) {
        if (Date.now() - start > 4000) throw new Error('S6: RPC ' + method + ' did not reach count ' + count);
        await page.waitForTimeout(10);
      }
    },
    async s6Ready() {
      await page.waitForFunction(() => document.querySelector('.cbi-page-actions button') && !document.querySelector('.cbi-page-actions button').disabled);
    },
    async s6CleanErrors() {
      assert.deepEqual(errors, [], 'Uncaught browser errors');
      assert.deepEqual(await page.evaluate(() => window.s6unhandled), [], 'Unhandled RPC rejection');
      assert.deepEqual(unexpectedRequests, [], 'Unexpected external network request');
    },
    async s6Snapshot() {
      if (page.isClosed()) return null;
      return await page.evaluate(() => ({ editor: window.s6editor, notices: window.s6notices, unhandled: window.s6unhandled,
        active: { tag: document.activeElement?.tagName, label: document.activeElement?.getAttribute('aria-label'), invalid: document.activeElement?.getAttribute('aria-invalid') },
        dirty: document.querySelector('.cbi-page-actions small')?.textContent,
        selectedTab: document.querySelector('[role=tab][aria-selected=true]')?.textContent,
        controls: [...document.querySelectorAll('.nk-app button,.nk-app input,.nk-app textarea,.nk-app select')].map(element => ({
          tag: element.tagName, label: element.getAttribute('aria-label') || element.textContent || element.type,
          disabled: element.disabled, inert: !!element.closest('[inert]'), busy: element.getAttribute('aria-busy'), visible: element.getBoundingClientRect().width > 0 })),
        probes: [...document.querySelectorAll('[data-probe-id]')].map(element => ({ id: element.dataset.probeId, state: element.dataset.state, text: element.textContent, title: element.title })),
        ruleErrors: [...document.querySelectorAll('.nk-rule-error')].map(element => element.textContent),
        overflow: document.documentElement.scrollWidth - innerWidth
      }));
    },
    async s6Close() {
      // End only this fixture's held responses; no process/host state is touched.
      for (const gate of [...pendingGates]) gate.release();
      await page.close(); if (!options.context) await context.close();
    }
  };

  async function s6Dispatch(method, params) {
    if (options.rpcHandler) { const result = await options.rpcHandler(method, params, state); if (result !== undefined) return result; }
    if (method === 'zaprettune') return { job: null };
    if (method === 'get') return structuredClone({ config: state.config, nodes: state.nodes, servers: state.servers, revision: state.revision });
    if (method === 'status') return structuredClone(state.status);
    if (method === 'identity') return { id: 's6-synthetic-device-000000', os: 'S6', version: 'test', model: 'Mock' };
    if (method === 'geodata') return { items: [{ id: 'geosite:s6-dormant', kind: 'geosite', name: 's6-dormant', count: 2, zapret: false }], sources: {} };
    if (method === 'validate') return { ok: true };
    if (method === 'save') {
      if (params.revision !== state.revision) return { error: 'Configuration changed: reload the page before saving' };
      state.config = structuredClone(params.config); state.nodes = structuredClone(params.nodes);
      state.revision = 's6-r' + (++state.generation); state.writes.push({ method, revision: state.revision, config: structuredClone(params.config) });
      return structuredClone({ revision: state.revision, servers: state.servers });
    }
    if (method === 'service') { state.actions.push(params.action); return {}; }
    if (method === 'nodes') return structuredClone({ nodes: state.servers });
    if (method === 'probe') return { results: Object.fromEntries(params.refs.map((id, index) => [id, { available: true, delayMs: 40 + index }])) };
    if (method === 'check') return { ok: true, steps: [{ id: 'config', title: 'Configuration', state: 'ok', detail: 'Synthetic S6 check' }] };
    if (method === 'addsource') {
      if (params.revision !== state.revision) return { error: 'Configuration changed: reload the page before adding a subscription' };
      let source = state.config.sources.find(source => source.url === params.content);
      const existing = !!source;
      if (!source) {
        source = { name: 'S6-added-' + state.config.sources.length, url: params.content, ...params.options };
        state.config.sources.push(structuredClone(source)); state.revision = 's6-r' + (++state.generation);
        state.writes.push({ method, revision: state.revision });
      }
      return structuredClone({ source, existing, revision: state.revision, servers: state.servers });
    }
    throw new Error('S6: no mock for method ' + method);
  }

  await page.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url());
    if (url.origin !== s6Origin) { unexpectedRequests.push(url.href); await route.abort(); return; }
    if (url.pathname === '/') {
      await route.fulfill({ contentType: 'text/html', body: '<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="s6-notices"></div><main id="maincontent"><div class="container"></div></main></body></html>' }); return;
    }
    if (url.pathname.endsWith('/picker.css')) { await route.fulfill({ contentType: 'text/css', body: sources.css }); return; }
    if (/\/nadkop\/flags\/[a-z]{2}\.svg$/.test(url.pathname)) {
      await route.fulfill({ contentType: 'image/svg+xml', body: await fs.readFile(path.join(s6AssetRoot, 'nadkop/flags', path.basename(url.pathname))) }); return;
    }
    if (url.pathname.startsWith('/admin/ubus')) {
      const frame = request.postDataJSON(), method = frame.params[2], params = frame.params[3];
      const fault = (faults.get(method) || []).shift() || {};
      log.push({ at: Date.now(), event: 'request', method, params, fault: fault.kind || (fault.hold ? 'held' : 'success') });
      if (fault.hold) await fault.hold;
      if (page.isClosed()) return;
      try {
        if (fault.kind === 'abort') { await route.abort('connectionreset'); log.push({ at: Date.now(), event: 'abort', method }); return; }
        if (fault.kind === 'http503') { await route.fulfill({ status: 503, body: 'S6 synthetic unavailable' }); log.push({ at: Date.now(), event: 'http503', method }); return; }
        if (fault.kind === 'truncated') { await route.fulfill({ contentType: 'application/json', body: '{"jsonrpc":"2.0","result":[0,' }); log.push({ at: Date.now(), event: 'truncated', method }); return; }
        let payload;
        if (fault.kind === 'commit-abort') { payload = await s6Dispatch(method, params); await route.abort('connectionreset'); log.push({ at: Date.now(), event: 'commit-abort', method, payload }); return; }
        if (fault.kind === 'ubus') payload = null; // Failed RPC was never dispatched.
        else if (fault.kind === 'reject') payload = fault.payload || { error: 'S6 synthetic ' + method + ' rejected' };
        else if (Object.hasOwn(fault, 'payload')) payload = structuredClone(fault.payload);
        else payload = await s6Dispatch(method, params);
        const body = fault.kind === 'ubus' ? { jsonrpc: '2.0', id: frame.id, result: [fault.code ?? 9] }
          : { jsonrpc: '2.0', id: frame.id, result: [0, payload] };
        await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
        log.push({ at: Date.now(), event: 'response', method, body });
      } catch (error) {
        // Closing a fixture may cancel its synthetic, held request.
        if (!page.isClosed()) errors.push('S6 route: ' + String(error.stack || error));
      }
      return;
    }
    // Theme fonts are intentionally not downloaded. Text falls back to local fonts.
    log.push({ at: Date.now(), event: 'unserved-local-asset', path: url.pathname }); await route.abort();
  });
  for (const [method, fault] of options.initialFaults || []) fixture.s6Fault(method, fault);
  await page.goto(s6Origin + '/');
  await page.addStyleTag({ content: 'body{font:14px system-ui;margin:0;padding:16px}main>.container{max-width:1280px;margin:auto}button,input,textarea,select{font:inherit}input,textarea,select{max-width:100%;box-sizing:border-box}.cbi-button{padding:8px}.cbi-value{padding:8px 0}.cbi-value-title{display:block}.cbi-input-textarea{width:100%}' });
  await page.evaluate(s6InstallBrowserAdapters, { ...sources, rpcTimeout: options.rpcTimeout ?? 20 });
  await page.evaluate(() => window.s6mount());
  await page.waitForFunction(() => document.querySelector('.nk-app link[rel=stylesheet]')?.sheet);
  if (options.waitGeo !== false) await page.waitForFunction(() => document.querySelector('.nk-geo-settings')?.textContent.includes('Sets:'));
  return fixture;
}

export async function s6SetTheme(fixture, theme, colorScheme = 'light') {
  const css = (await Promise.all(s6Themes[theme].map(file => fs.readFile(path.join(s6ThemeRoot, file), 'utf8')))).join('\n').replace(/@import[^;]+;/g, '');
  await fixture.page.evaluate(() => document.querySelectorAll('head style').forEach(style => style.remove()));
  await fixture.page.addStyleTag({ content: css });
  // App CSS must follow skin styles, as in the existing theme checks.
  await fixture.page.addStyleTag({ content: await fs.readFile(path.join(s6AssetRoot, 'nadkop/picker.css'), 'utf8') });
  await fixture.page.emulateMedia({ colorScheme, reducedMotion: 'reduce' });
  // Bootstrap's real header mirrors matchMedia into this attribute. Merely
  // emulating prefers-color-scheme does not activate its dark palette.
  await fixture.page.evaluate(scheme => document.documentElement.setAttribute('data-darkmode', String(scheme === 'dark')), colorScheme);
}

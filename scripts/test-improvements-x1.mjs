// Regression checks for X1 using Chromium, unmodified LuCI rpc.js and synthetic RPC routes.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import { s6Root, s6ReadSources, s6InitialState, s6OpenFixture, s6IDs, s6SetTheme, s6Themes } from './fixtures/s6-luci-harness.mjs';

const output = path.resolve(process.env.X1_OUTPUT || path.join(s6Root, 'build/improvements-X/implementation/regression'));
await fs.mkdir(output, { recursive: true });
const { chromium } = await import(process.env.NADKOP_PLAYWRIGHT_MODULE || '/path/to/playwright-env/node_modules/playwright/index.mjs');
const sources = await s6ReadSources();
const browser = await chromium.launch({ executablePath: process.env.NADKOP_CHROMIUM || '/run/current-system/sw/bin/chromium', headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const results = [];
const filter = new RegExp(process.env.X1_CASE || '.');
const rule = (f, index = 0) => f.page.locator('[data-rule-index="' + index + '"]');
const poolCard = (f, id) => f.page.locator('.nk-picker .nk-server-card[data-server-id="' + id + '"]');
const save = f => f.page.getByRole('button', { name: 'Save', exact: true });
const apply = f => f.page.getByRole('button', { name: 'Save and apply', exact: true });
const active = f => f.page.evaluate(() => ({ label: document.activeElement.getAttribute('aria-label'),
  server: document.activeElement.closest('.nk-server-card')?.getAttribute('data-server-id'),
  rule: document.activeElement.closest('[data-rule-index]')?.getAttribute('data-rule-index') }));
async function edit(f) { await rule(f).getByLabel('Domains', { exact: true }).fill('x1.example'); }
async function test(id, fn, options = {}) {
  if (!filter.test(id)) return;
  const f = await s6OpenFixture(browser, sources, options);
  let error;
  try { await fn(f); await f.s6CleanErrors(); } catch (err) { error = String(err.stack || err); }
  const result = { id, status: error ? 'FAIL' : 'PASS', error, ui: await f.s6Snapshot(), calls: f.log };
  await fs.writeFile(path.join(output, id + '.json'), JSON.stringify(result, null, 2) + '\n');
  await f.s6Close(); results.push({ id, status: result.status, error });
  console.log(result.status + ' ' + id + (error ? '\n' + error : ''));
}
try {
  for (const kind of ['ubus', 'reject', 'commit-abort']) {
    await test('save_' + kind, async f => {
      await edit(f); f.s6Fault('save', { kind });
      await apply(f).click(); await f.s6WaitCall('save'); await f.s6Ready();
      assert.equal((await f.s6Snapshot()).dirty, 'Save not confirmed');
      assert.equal(f.s6Count('service'), 0);
      assert.equal(f.s6Count('save'), 1, 'Unconfirmed save was retried');
      assert.deepEqual((await f.s6Snapshot()).editor.rules[0].domains, ['x1.example']);
      assert.equal(f.state.writes.length, kind === 'commit-abort' ? 1 : 0);
      assert.equal(f.s6Calls('save')[0].params.revision, 's6-r1');
      if (kind === 'commit-abort') {
        // A manual retry must retain the original revision and be refused by
        // the backend; it must never silently adopt an unknown revision.
        await save(f).click(); await f.s6Ready();
        assert.equal(f.s6Calls('save')[1].params.revision, 's6-r1');
        assert.equal(f.state.writes.length, 1); assert.equal(f.s6Count('service'), 0);
      }
    });
    await test('service_' + kind, async f => {
      await edit(f); f.s6Fault('service', { kind });
      await apply(f).click(); await f.s6WaitCall('service'); await f.s6Ready();
      assert.equal((await f.s6Snapshot()).dirty, 'Saved, apply not confirmed');
      assert.equal(f.state.writes.length, 1); assert.equal(f.s6Count('service'), 1);
      assert.deepEqual(f.state.actions, kind === 'commit-abort' ? ['restart'] : []);
      await edit(f); await save(f).click(); await f.s6Ready();
      assert.equal(f.s6Calls('save').at(-1).params.revision, 's6-r2');
      assert.equal((await f.s6Snapshot()).dirty, 'Saved');
    });
  }
  await test('save_malformed_success', async f => {
    await edit(f); f.s6Fault('save', { payload: {} });
    await apply(f).click(); await f.s6Ready();
    assert.equal((await f.s6Snapshot()).dirty, 'Save not confirmed');
    assert.equal(f.s6Count('service'), 0);
  });
  await test('applied_status_unavailable', async f => {
    await edit(f); f.s6Fault('status', { kind: 'ubus' });
    await apply(f).click(); await f.s6Ready();
    assert.equal((await f.s6Snapshot()).dirty, 'Saved and applied');
    assert.equal(f.s6Count('service'), 1);
  });
  for (const phase of ['save', 'service']) await test('late_draft_' + phase, async f => {
    await edit(f); const gate = f.s6Gate(phase, { kind: 'commit-abort' });
    await apply(f).click(); await f.s6WaitCall(phase);
    // Deferred input events may arrive after the save snapshot despite busy.
    await rule(f).getByLabel('Domains', { exact: true }).evaluate(el => {
      el.value = 'newer.x1.example'; el.dispatchEvent(new Event('input', { bubbles: true }));
    });
    gate.release(); await f.s6Ready();
    const snapshot = await f.s6Snapshot();
    assert.deepEqual(snapshot.editor.rules[0].domains, ['newer.x1.example']);
    assert.match(snapshot.dirty, phase === 'save' ? /^Save not confirmed.*new changes/ : /^Unsaved.*apply not confirmed/);
    assert.deepEqual(f.state.config.rules[0].domains, ['x1.example']);
  });
  await test('keyboard_move_rules', async f => {
    for (let i = 0; i < 4; i++) {
      const index = i % 2, direction = index ? 'up' : 'down';
      const control = rule(f, index).getByRole('button', { name: (direction === 'up' ? 'Move up' : 'Move down') + ' rule ' + (index + 1), exact: true });
      await control.focus(); await control.press('Enter');
      assert.equal((await active(f)).rule, String(1 - index));
      assert.equal((await active(f)).label, index ? 'Move down rule 1' : 'Move up rule 2');
    }
    assert.equal((await f.s6Snapshot()).editor.rules[0].name, 'S6 route');
    const name = rule(f).getByRole('button', { name: 'Rename S6 route', exact: true });
    await name.click(); await rule(f).getByLabel('Rule name 1', { exact: true }).fill('After move');
    await rule(f).getByLabel('Rule name 1', { exact: true }).press('Enter');
    assert.equal((await active(f)).label, 'Rename After move');
  });
  await test('keyboard_move_servers', async f => {
    for (let i = 0; i < 4; i++) {
      const control = rule(f).locator('[data-server-id="' + s6IDs[0] + '"] [data-move-server="' + (i % 2 ? 'up' : 'down') + '"]');
      await control.focus(); await control.press('Enter');
      assert.equal((await active(f)).server, s6IDs[0]);
      assert.equal((await active(f)).label, i % 2 ? 'Move down server 1' : 'Move up server 2');
    }
    assert.deepEqual((await f.s6Snapshot()).editor.rules[0].nodeRefs, s6IDs.slice(0, 2));
  });
  const longState = s6InitialState();
  longState.config.rules.push({ name: 'Third', domains: ['third.test'], action: 'direct' }, { name: 'Fourth', domains: ['fourth.test'], action: 'direct' });
  longState.servers.push({ ...longState.servers[0], id: 'srv_' + '4'.repeat(32), name: 'Fourth exit', position: 3 });
  longState.config.rules[0].nodeRefs = longState.servers.map(server => server.id);
  await test('repeated_moves_and_scroll', async f => {
    let control = rule(f).getByRole('button', { name: 'Move down rule 1', exact: true });
    await control.focus();
    for (let i = 0; i < 3; i++) {
      const before = await f.page.evaluate(() => scrollY);
      await f.page.keyboard.press('Enter');
      assert.equal((await active(f)).rule, String(i + 1));
      assert.equal((await active(f)).label, i < 2 ? 'Move down rule ' + (i + 2) : 'Move up rule 4');
      assert.equal(await f.page.evaluate(() => scrollY), before);
    }
    control = rule(f, 3).getByRole('button', { name: 'Move down server 1', exact: true });
    await control.focus();
    for (let i = 0; i < 3; i++) {
      await f.page.keyboard.press('Enter');
      assert.equal((await active(f)).server, s6IDs[0]);
      assert.equal((await active(f)).label, i < 2 ? 'Move down server ' + (i + 2) : 'Move up server 4');
    }
    const field = rule(f, 3).getByLabel('Domains', { exact: true });
    await field.focus();
    // A synthetic/programmatic click must not take focus from another field.
    await rule(f, 0).getByRole('button', { name: 'Move down rule 1', exact: true }).dispatchEvent('click');
    assert.notEqual((await active(f)).label, 'Move down rule 2');
  }, { state: longState });
  await test('keyboard_pin_and_empty_filter', async f => {
    const filter = f.page.getByRole('button', { name: 'Pinned', exact: true });
    await filter.focus(); await filter.press('Enter');
    assert.equal((await active(f)).label, null);
    assert.equal(await filter.evaluate(el => el === document.activeElement), true);
    const pin = poolCard(f, s6IDs[0]).locator('.nk-pin');
    await pin.focus(); await pin.press('Enter');
    assert.equal(await f.page.getByLabel('Search connections', { exact: true }).evaluate(el => el === document.activeElement), true);
    assert.equal(await f.page.locator('.nk-picker .nk-server-card').count(), 0);
    await filter.focus(); await filter.press('Enter');
    const newPin = poolCard(f, s6IDs[0]).locator('.nk-pin');
    await newPin.focus(); await newPin.press('Enter');
    assert.equal((await active(f)).server, s6IDs[0]);
    assert.equal(await newPin.getAttribute('aria-pressed'), 'true');
  });
  const duplicateState = s6InitialState();
  duplicateState.config.sources.push({ name: 'Other', title: 'Same provider', url: 'https://other.test/sub' });
  duplicateState.config.sources[0].title = 'Same provider';
  duplicateState.servers.forEach((server, index) => { server.name = 'Same exit'; if (index === 2) server.source = 'Other'; });
  await test('duplicate_names_and_idempotent_selection', async f => {
    const buttons = f.page.locator('.nk-picker .nk-server-main');
    const names = await buttons.evaluateAll(els => els.map(el => el.getAttribute('aria-label')));
    assert.equal(new Set(names).size, 3);
    assert.ok(names.every(name => !/https?:|srv_|secret/.test(name)));
    assert.match(await f.page.locator('.nk-picker-list').ariaSnapshot(), /Same exit.*Same provider.*1/);
    await rule(f).getByRole('button', { name: 'Add server', exact: true }).click();
    const selected = poolCard(f, s6IDs[0]).locator('.nk-server-main');
    assert.match(await selected.getAttribute('aria-label'), /already added to the selected rule/);
    assert.equal(await selected.getAttribute('aria-pressed'), null);
    await selected.click();
    assert.deepEqual((await f.s6Snapshot()).editor.rules[0].nodeRefs, s6IDs.slice(0, 2));
    await rule(f, 1).getByRole('button', { name: 'VPN', exact: true }).click();
    await rule(f, 1).getByRole('button', { name: 'Select server', exact: true }).click();
    assert.doesNotMatch(await poolCard(f, s6IDs[0]).locator('.nk-server-main').getAttribute('aria-label'), /already added/);
    await f.page.getByLabel('Subscription filter', { exact: true }).selectOption('Other');
    assert.equal(await poolCard(f, s6IDs[2]).locator('.nk-server-main').getAttribute('aria-label'), names[2]);
  }, { state: duplicateState });
  for (const theme of Object.keys(s6Themes)) for (const scheme of ['light', 'dark']) {
    await test('zapret_help_' + theme + '_' + scheme, async f => {
      await s6SetTheme(f, theme, scheme);
      const summary = rule(f).locator('.nk-action-help summary');
      await summary.focus(); await summary.press('Enter');
      assert.equal(await rule(f).getByRole('button', { name: 'Zapret', exact: true }).isDisabled(), true);
      const description = await summary.getAttribute('aria-describedby');
      assert.match(await f.page.locator('#' + description).textContent(), /WAN-device and strategy/);
      const cdp = await f.context.newCDPSession(f.page);
      const tree = await cdp.send('Accessibility.getFullAXTree');
      const disclosure = tree.nodes.find(node => !node.ignored && node.name?.value === 'Why Zapret is unavailable');
      assert.ok(disclosure, 'Help is absent from the accessibility tree');
      assert.match(disclosure.description?.value || '', /WAN-device and strategy/);
      await cdp.detach();
      assert.equal((await active(f)).label, 'Why Zapret is unavailable');
      // Compatibility may change during a catalogue response while focused.
      // Trigger the normal repaint without rerendering or moving focus.
      await f.page.evaluate(() => {
        window.s6editor.wanInterface = 'eth0'; window.s6editor.zapretArgs = ['--filter-tcp=443'];
        const editor = document.querySelector('[data-rule-index="0"] textarea');
        editor.dispatchEvent(new Event('input', { bubbles: true }));
      });
      assert.equal((await active(f)).label, 'Zapret is available');
      assert.equal(await summary.evaluate(el => el.closest('details').hidden), false);
      await summary.press('Tab');
      await f.page.waitForFunction(() => document.querySelector('[data-rule-index="0"] .nk-action-help').hidden);
      assert.equal(await rule(f).getByRole('button', { name: 'Zapret', exact: true }).isEnabled(), true);
      assert.ok((await f.s6Snapshot()).overflow <= 1);
    });
  }
  for (const theme of Object.keys(s6Themes)) await test('zapret_touch_' + theme, async f => {
    await s6SetTheme(f, theme);
    const summary = rule(f).locator('.nk-action-help summary');
    await summary.tap();
    const note = rule(f).locator('.nk-action-help p');
    assert.equal(await note.isVisible(), true);
    const bounds = await note.boundingBox();
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 391, JSON.stringify(bounds));
    assert.ok((await f.s6Snapshot()).overflow <= 1);
    await summary.tap(); assert.equal(await note.isVisible(), false);
  }, { context: await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, serviceWorkers: 'block' }) });
  const geoState = s6InitialState();
  geoState.config.wanInterface = 'eth0'; geoState.config.zapretArgs = ['--filter-tcp=443']; geoState.config.rules[0].conditions.sets = true;
  let releaseGeo;
  const holdGeo = new Promise(resolve => { releaseGeo = resolve; });
  await test('focused_help_catalogue_response', async f => {
    try {
      await f.s6WaitCall('geodata');
      const summary = rule(f).locator('.nk-action-help summary'); await summary.focus();
      assert.match(await rule(f).locator('.nk-action-help p').textContent(), /Loading/);
      releaseGeo();
      await f.page.waitForFunction(() => document.querySelector('[data-rule-index="0"] .nk-action-help p').textContent.includes('Xray (VPN) only'));
      assert.equal((await active(f)).label, 'Why Zapret is unavailable');
      assert.equal(await rule(f).getByRole('button', { name: 'Zapret', exact: true }).isDisabled(), true);
    } finally { releaseGeo(); }
  }, { state: geoState, waitGeo: false, initialFaults: [['geodata', { hold: holdGeo }]] });
  await test('background_status_preserves_focus', async f => {
    const field = rule(f).getByLabel('Domains', { exact: true });
    await field.focus(); await f.page.evaluate(() => window.s6poll[0]());
    assert.equal((await active(f)).label, 'Domains');
  });
} finally {
  await browser.close();
  const result = { capturedAt: new Date().toISOString(),
    hashes: Object.fromEntries(Object.entries(sources).map(([name, value]) => [name, crypto.createHash('sha256').update(value).digest('hex')])),
    total: results.length, pass: results.filter(r => r.status === 'PASS').length, fail: results.filter(r => r.status === 'FAIL').length, results };
  await fs.writeFile(path.join(output, 'results.json'), JSON.stringify(result, null, 2) + '\n');
  console.log('X1 ' + JSON.stringify({ total: result.total, pass: result.pass, fail: result.fail }));
  if (result.fail) process.exitCode = 1;
}

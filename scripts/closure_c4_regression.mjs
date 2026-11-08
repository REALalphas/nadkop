// Focused closure assertions beyond the original S6 audit. No live RPC/network.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import { s6Root, s6RPCPath, s6ReadSources, s6OpenFixture, s6IDs } from './fixtures/s6-luci-harness.mjs';

const output = path.resolve(process.env.C4_OUTPUT || path.join(s6Root, 'build/blocker-closure/C4/regression'));
if (!output.startsWith(path.join(s6Root, 'build/blocker-closure/C4/'))) throw new Error('C4 artifacts must stay in C4');
await fs.mkdir(output, { recursive: true });
const { chromium } = await import(process.env.NADKOP_PLAYWRIGHT_MODULE || '/path/to/playwright-env/node_modules/playwright/index.mjs');
const sources = await s6ReadSources();
const browser = await chromium.launch({ executablePath: process.env.NADKOP_CHROMIUM || '/run/current-system/sw/bin/chromium', headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage', '--renderer-process-limit=2'] });
const results = [];
const rule = f => f.page.locator('[data-rule-index="0"]');
const save = f => f.page.getByRole('button', { name: 'Save', exact: true });
const apply = f => f.page.getByRole('button', { name: 'Save and apply', exact: true });
async function locked(f) {
  assert.deepEqual(await f.page.locator('.nk-app button,.nk-app input,.nk-app textarea,.nk-app select')
    .evaluateAll(els => els.filter(el => !el.disabled).map(el => el.outerHTML)), []);
}
async function edit(f, text = 'closure.c4.test') { await rule(f).getByLabel('Domains', { exact: true }).fill(text); }
async function test(id, fn, options = {}) {
  const f = await s6OpenFixture(browser, sources, options);
  let error;
  try { await fn(f); await f.s6CleanErrors(); } catch (err) { error = String(err.stack || err); }
  const result = { id, status: error ? 'FAIL' : 'PASS', error, ui: await f.s6Snapshot(), backend: f.state, rpc: f.log };
  await fs.writeFile(path.join(output, id + '.json'), JSON.stringify(result, null, 2) + '\n');
  await f.s6Close(); results.push({ id, status: result.status, error });
  console.log(result.status + ' ' + id + (error ? '\n' + error : ''));
}
try {
  await test('closure_c4_all_rpc_status_codes', async f => {
    // Capture the view's exact declarations, then exercise them through the
    // original SDK rpc.js and the same HTTP/JSON-RPC fault route as S6.
    const names = await f.page.evaluate(source => {
      const api = {};
      new Function('rpc', 'view', source)({ declare(options) { api[options.method] = window.s6rpc.declare(options); return api[options.method]; } }, { extend: value => value });
      window.closureC4API = api; return Object.keys(api);
    }, sources.viewSource);
    assert.deepEqual(names.sort(), ['get','validate','status','identity','save','geodata','updategeo','probe','import','addsource','nodes','check','service'].sort());
    for (const name of names) {
      for (const code of [1, 4, 9]) {
        f.s6Fault(name, { kind: 'ubus', code });
        const response = await f.page.evaluate(async name => {
          try { await window.closureC4API[name](); return { rejected: false }; }
          catch (err) { return { rejected: true, message: err.message }; }
        }, name);
        assert.equal(response.rejected, true, name + ' accepted nonzero ' + code);
        assert.match(response.message, new RegExp('ubus code ' + code));
      }
      f.s6Fault(name, { payload: { closureC4Success: name } });
      assert.deepEqual(await f.page.evaluate(name => window.closureC4API[name](), name), { closureC4Success: name });
    }
    assert.equal(f.state.writes.length, 0); assert.deepEqual(f.state.actions, []);
  });

  for (const method of ['validate', 'save']) for (const fault of ['ubus', 'reject']) {
    await test('closure_c4_apply_failed_' + method + '_' + fault, async f => {
      await edit(f); f.s6Fault(method, { kind: fault, code: 9 });
      await apply(f).click(); await f.s6WaitCall(method); await f.s6Ready();
      assert.equal(f.s6Count('service'), 0, 'Restart requested after failure');
      assert.equal(f.state.writes.length, 0);
      if (method === 'validate') assert.equal(f.s6Count('save'), 0);
      assert.equal((await f.s6Snapshot()).dirty, method === 'save' ? 'Save not confirmed' : 'Unsaved');
      assert.ok((await f.s6Snapshot()).notices.length > 0);
      // Failed save must retain the previous revision for a successful retry.
      await apply(f).click(); await f.s6WaitCall('service'); await f.s6Ready();
      assert.equal(f.state.writes.length, 1); assert.deepEqual(f.state.actions, ['restart']);
      assert.equal(f.s6Calls('save').at(-1).params.revision, 's6-r1');
      assert.equal((await f.s6Snapshot()).dirty, 'Saved and applied');
      await edit(f, 'next.c4.test'); await save(f).click(); await f.s6Ready();
      assert.equal(f.s6Calls('save').at(-1).params.revision, 's6-r2');
      assert.equal(f.state.writes.length, 2);
    });
  }

  for (const phase of ['validate', 'service']) await test('closure_c4_late_draft_' + phase, async f => {
    await edit(f); const gate = f.s6Gate(phase);
    await apply(f).click(); await f.s6WaitCall(phase); await locked(f);
    // Disabled controls cannot receive user input. This deliberately injects
    // an event to model a deferred editor callback after the save snapshot.
    await rule(f).getByLabel('Domains', { exact: true }).evaluate(el => {
      el.value = 'deferred.c4.test'; el.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await locked(f); gate.release(); await f.s6WaitCall('service'); await f.s6Ready();
    assert.deepEqual(f.state.config.rules[0].domains, ['closure.c4.test']);
    assert.deepEqual((await f.s6Snapshot()).editor.rules[0].domains, ['deferred.c4.test']);
    assert.equal((await f.s6Snapshot()).dirty, 'Unsaved');
    await save(f).click(); await f.s6Ready();
    assert.equal(f.s6Calls('save').at(-1).params.revision, 's6-r2');
    assert.deepEqual(f.state.config.rules[0].domains, ['deferred.c4.test']);
    assert.equal((await f.s6Snapshot()).dirty, 'Saved');
  });

  for (const failure of [false, true]) await test('closure_c4_dynamic_drag_rename_' + (failure ? 'failure' : 'success'), async f => {
    await edit(f); const service = f.s6Gate('service', failure ? { kind: 'ubus', code: 9 } : {});
    await apply(f).evaluate(el => { el.click(); el.click(); }); await f.s6WaitCall('service');
    assert.equal(f.s6Count('validate'), 1); assert.equal(f.s6Count('save'), 1); assert.equal(f.s6Count('service'), 1);
    await locked(f);
    const before = structuredClone((await f.s6Snapshot()).editor);
    await rule(f).locator('.nk-rule-title').dblclick();
    assert.equal(await rule(f).locator('.nk-rule-name').count(), 0);
    // A drag can have started before busy. Dispatch drop on both the selected
    // member (reorder) and rule (assign) and dragend (pool rerender) while busy.
    await f.page.evaluate(ids => {
      const dt = new DataTransfer(); dt.setData('application/x-nadkop-server', ids[2]); dt.setData('text/plain', ids[2]);
      const targets = [document.querySelector('[data-rule-index="0"] .nk-server-card'), document.querySelector('[data-rule-index="0"]')];
      for (const target of targets) target.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt }));
      const card = document.querySelector('.nk-picker .nk-server-card');
      const start = new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt });
      card.dispatchEvent(start); window.closureC4DragPrevented = start.defaultPrevented;
      card.dispatchEvent(new DragEvent('dragend', { bubbles: true, dataTransfer: dt }));
    }, s6IDs);
    assert.equal(await f.page.evaluate(() => window.closureC4DragPrevented), true);
    await locked(f); assert.deepEqual((await f.s6Snapshot()).editor, before);
    service.release(); await f.s6Ready();
    assert.equal((await f.s6Snapshot()).dirty, failure ? 'Saved, apply not confirmed' : 'Saved and applied');
    assert.equal((await f.s6Snapshot()).notices.length, failure ? 1 : 0);
    assert.equal(await rule(f).getByRole('button', { name: 'Move up server 1', exact: true }).isDisabled(), true);
    assert.equal(await rule(f).getByRole('button', { name: 'Move down server 2', exact: true }).isDisabled(), true);
    assert.equal(await rule(f).getByRole('button', { name: 'Zapret', exact: true }).isDisabled(), true);
    await rule(f).getByRole('button', { name: 'Move down server 1', exact: true }).click();
    assert.deepEqual((await f.s6Snapshot()).editor.rules[0].nodeRefs, [s6IDs[1], s6IDs[0]]);
    await rule(f).locator('.nk-rule-title').dblclick();
    await rule(f).getByLabel('Rule name 1', { exact: true }).fill('Closure C4 rename');
    await rule(f).getByLabel('Rule name 1', { exact: true }).press('Enter');
    assert.equal((await f.s6Snapshot()).editor.rules[0].name, 'Closure C4 rename');
    assert.equal((await f.s6Snapshot()).dirty, 'Unsaved');
    await save(f).click(); await f.s6Ready();
    assert.equal(f.s6Calls('save').at(-1).params.revision, 's6-r2');
    assert.deepEqual(f.state.config.rules[0].nodeRefs, [s6IDs[1], s6IDs[0]]);
    assert.equal(f.state.config.rules[0].name, 'Closure C4 rename');
  });

  for (const failure of [false, true]) {
    let releaseGeo;
    const hold = new Promise(resolve => { releaseGeo = resolve; });
    await test('closure_c4_late_status_geo_' + (failure ? 'failure' : 'success'), async f => {
      try {
        await edit(f); const status = f.s6Gate('status');
        await f.page.evaluate(() => { window.closureC4Poll = window.s6poll[0](); }); await f.s6WaitCall('status', 2);
        const validation = f.s6Gate('validate', failure ? { kind: 'ubus', code: 9 } : {});
        await save(f).click(); await f.s6WaitCall('validate'); await locked(f);
        status.release(); releaseGeo(); await f.page.evaluate(() => window.closureC4Poll);
        await f.page.waitForFunction(() => document.querySelector('.nk-geo-settings').textContent.includes('Sets:'));
        await locked(f);
        // Repeat a rerender (including dragend) to verify remembered disabled
        // state is not replaced with the temporary locked state.
        await f.page.locator('.nk-picker .nk-server-card').first().dispatchEvent('dragend'); await locked(f);
        validation.release(); await f.s6Ready();
        assert.equal(f.state.writes.length, failure ? 0 : 1);
        assert.equal(f.s6Count('service'), 0);
        assert.equal((await f.s6Snapshot()).dirty, failure ? 'Unsaved' : 'Saved');
        await f.page.getByRole('tab', { name: 'Diagnostics', exact: true }).click();
        assert.equal(await f.page.getByRole('button', { name: 'Restart', exact: true }).isEnabled(), true);
        await f.page.getByRole('tab', { name: 'Settings', exact: true }).click();
        await f.page.getByText('Routing databases', { exact: true }).click();
        const url = f.page.getByLabel('Geosite URL', { exact: true });
        assert.equal(await url.isEnabled(), true); await url.fill('https://geodata.c4.test/geosite.dat');
        assert.equal((await f.s6Snapshot()).dirty, 'Unsaved');
        await save(f).click(); await f.s6Ready();
        assert.equal(f.s6Calls('save').at(-1).params.revision, failure ? 's6-r1' : 's6-r2');
        assert.equal(f.state.config.geodata.geositeURL, 'https://geodata.c4.test/geosite.dat');
        assert.equal((await f.s6Snapshot()).dirty, 'Saved');
      } finally { releaseGeo(); }
    }, { waitGeo: false, initialFaults: [['geodata', { hold }]] });
  }

  await test('closure_c4_rename_blur_replacement', async f => {
    await edit(f); await rule(f).locator('.nk-rule-title').dblclick();
    const name = rule(f).getByLabel('Rule name 1', { exact: true });
    await name.fill('Late blur C4');
    const validation = f.s6Gate('validate');
    // Trigger save while the editor still has focus. Disabling it may defer
    // blur until after the save snapshot; either snapshot ordering is valid,
    // but dirty must reflect any name not included in that snapshot.
    await save(f).evaluate(el => el.click()); await f.s6WaitCall('validate');
    await f.page.evaluate(() => document.querySelector('[data-rule-index="0"] .nk-rule-name')?.dispatchEvent(new Event('blur')));
    await locked(f); validation.release(); await f.s6Ready();
    const ui = await f.s6Snapshot();
    assert.equal(ui.editor.rules[0].name, 'Late blur C4');
    assert.equal(ui.dirty, f.state.config.rules[0].name === 'Late blur C4' ? 'Saved' : 'Unsaved');
    assert.equal(await rule(f).getByRole('button', { name: 'Rename Late blur C4', exact: true }).isEnabled(), true);
    await save(f).click(); await f.s6Ready();
    assert.equal(f.s6Calls('save').at(-1).params.revision, 's6-r2');
    assert.equal(f.state.config.rules[0].name, 'Late blur C4');
  });

  await test('closure_c4_aria_tab_boundaries', async f => {
    const tabs = f.page.getByRole('tab'); const first = tabs.first(), last = tabs.last();
    await first.focus(); await first.press('ArrowLeft'); assert.equal(await last.evaluate(el => el === document.activeElement), true);
    await last.press('ArrowRight'); assert.equal(await first.evaluate(el => el === document.activeElement), true);
    await first.press('End'); assert.equal(await last.evaluate(el => el === document.activeElement), true);
    await last.press('Home'); assert.equal(await first.evaluate(el => el === document.activeElement), true);
    assert.equal(await tabs.evaluateAll(els => els.filter(el => el.tabIndex === 0).length), 1);
    assert.equal(await first.getAttribute('aria-selected'), 'true');
    await first.press('ArrowRight'); assert.equal(await tabs.nth(1).getAttribute('aria-selected'), 'true');
    assert.equal(await tabs.evaluateAll(els => els.filter(el => el.tabIndex === 0).length), 1);
  });
} finally {
  await browser.close();
  const result = { rpc: s6RPCPath, rpcSha256: crypto.createHash('sha256').update(sources.rpcSource).digest('hex'),
    viewSha256: crypto.createHash('sha256').update(sources.viewSource).digest('hex'), total: results.length,
    pass: results.filter(r => r.status === 'PASS').length, fail: results.filter(r => r.status === 'FAIL').length, results };
  await fs.writeFile(path.join(output, 'results.json'), JSON.stringify(result, null, 2) + '\n');
  console.log('C4 SUMMARY ' + JSON.stringify({ total: result.total, pass: result.pass, fail: result.fail }));
  if (result.fail) process.exitCode = 1;
}

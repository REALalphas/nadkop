// S6 isolated headless audit. Only synthetic HTTP routes and this runner's pages.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import { performance } from 'node:perf_hooks';
import { s6Root, s6RPCPath, s6ThemeRoot, s6IDs, s6InitialState, s6ReadSources, s6OpenFixture, s6SetTheme, s6Themes } from './fixtures/s6-luci-harness.mjs';

const s6Output = path.resolve(process.env.S6_ARTIFACT_DIR || path.join(s6Root, 'build/agent-audit/S6/latest'));
const s6Filter = new RegExp(process.env.S6_CASE || '.');
const auditConfig = JSON.parse(await fs.readFile(new URL('./audit-config.json', import.meta.url), 'utf8'));
const s6Manifest = process.env.S6_MANIFEST || path.join(path.resolve(s6Root, process.env.NADKOP_AUDIT_REPORT || auditConfig.report), auditConfig.manifest);
const playwrightModule = process.env.NADKOP_PLAYWRIGHT_MODULE || auditConfig.playwright;
try { await fs.access(playwrightModule); } catch {
  console.error(`ERROR: Playwright module missing: ${playwrightModule}. Prepare /path/to/playwright-env or set NADKOP_PLAYWRIGHT_MODULE to its index.mjs file.`);
  process.exit(2);
}
const s6Results = [];
const s6Started = new Date().toISOString();
let s6Browser, s6Sources;

async function s6AtomicJSON(file, value) {
  const temporary = file + '.' + process.pid + '.tmp';
  await fs.writeFile(temporary, JSON.stringify(value, null, 2) + '\n'); await fs.rename(temporary, file);
}

async function s6Frozen(name) {
  const manifestBytes = await fs.readFile(s6Manifest), manifest = JSON.parse(manifestBytes);
  const sha256 = {}, mismatches = [];
  for (const [file, expected] of Object.entries(manifest.files)) {
    let actual = null;
    try { actual = crypto.createHash('sha256').update(await fs.readFile(path.join(s6Root, file))).digest('hex'); } catch {}
    sha256[file] = actual; if (actual !== expected) mismatches.push({ file, expected, actual });
  }
  const result = { at: new Date().toISOString(), manifest: path.relative(s6Root, s6Manifest),
    manifestSha256: crypto.createHash('sha256').update(manifestBytes).digest('hex'), filesChecked: Object.keys(sha256).length, mismatches, sha256 };
  await s6AtomicJSON(path.join(s6Output, name + '.json'), result); return result;
}

async function s6Case(id, description, action) {
  if (!s6Filter.test(id)) return;
  const fixtures = [], started = performance.now(), evidence = {};
  const create = async options => { const fixture = await s6OpenFixture(s6Browser, s6Sources, options); fixtures.push(fixture); return fixture; };
  let error;
  try { await action(create, evidence); for (const fixture of fixtures) await fixture.s6CleanErrors(); }
  catch (failure) { error = failure; }
  const snapshots = [];
  for (let index = 0; index < fixtures.length; index++) {
    const fixture = fixtures[index];
    snapshots.push({ page: index, ui: await fixture.s6Snapshot(), backend: fixture.state, pageErrors: fixture.errors, unexpectedRequests: fixture.unexpectedRequests, rpc: fixture.log });
    if (error) await fixture.page.screenshot({ path: path.join(s6Output, id + '-page' + index + '.png'), fullPage: true }).catch(() => {});
  }
  const result = { id, description, status: error ? 'FAIL' : 'PASS', durationMs: Math.round(performance.now() - started),
    ...(error ? { error: String(error.stack || error) } : {}), evidence, snapshots };
  await s6AtomicJSON(path.join(s6Output, id + '.json'), result);
  // Shared contexts are closed after their last page, in reverse order.
  for (const fixture of fixtures.reverse()) await fixture.s6Close();
  s6Results.push({ id, description, status: result.status, durationMs: result.durationMs, ...(error ? { error: error.message } : {}), log: id + '.json' });
  console.log(result.status + ' ' + id + ' (' + result.durationMs + ' ms) ' + description + (error ? '\n  ' + error.message : ''));
  await s6AtomicJSON(path.join(s6Output, 'results.json'), { started: s6Started, filter: s6Filter.source, results: s6Results });
}

const s6Rule = fixture => fixture.page.locator('[data-rule-index="0"]');
const s6Save = fixture => fixture.page.getByRole('button', { name: 'Save', exact: true });
const s6Apply = fixture => fixture.page.getByRole('button', { name: 'Save and apply', exact: true });
const s6Refresh = fixture => s6Rule(fixture).getByRole('button', { name: 'Refresh server checks', exact: true });

async function s6EditDraft(fixture, text = 'draft.s6.test') {
  await s6Rule(fixture).getByLabel('Domains', { exact: true }).fill(text);
  return await fixture.page.evaluate(() => structuredClone(window.s6editor));
}

async function s6AssertDraft(fixture, expected) {
  assert.deepEqual(await fixture.page.evaluate(() => window.s6editor), expected, 'Draft, dormant matchers, references or selection URL changed');
}

async function s6AssertRestored(fixture) {
  await fixture.s6Ready();
  const rulesTab = fixture.page.getByRole('tab', { name: 'Rules', exact: true });
  if (await rulesTab.getAttribute('aria-selected') !== 'true') await rulesTab.click();
  assert.equal(await fixture.page.getByRole('button', { name: 'Move up rule 1', exact: true }).isDisabled(), true, 'First rule can move above its boundary');
  assert.equal(await fixture.page.getByRole('button', { name: 'Move down rule 2', exact: true }).isDisabled(), true, 'Last rule can move below its boundary');
  assert.equal(await s6Rule(fixture).getByRole('button', { name: 'Zapret', exact: true }).isDisabled(), true, 'Incompatible Zapret action was re-enabled');
  assert.equal(await s6Rule(fixture).getByRole('button', { name: 'Domains', exact: true }).isEnabled(), true, 'Matcher control remained disabled');
  assert.equal(await s6Rule(fixture).getByLabel('Domains', { exact: true }).isEnabled(), true, 'Draft control remained disabled');
}

async function s6AssertGlobalLock(fixture) {
  const enabled = await fixture.page.locator('.nk-app').locator('button,input,textarea,select').evaluateAll(elements => elements.filter(element => !element.disabled).map(element => ({ tag: element.tagName, label: element.getAttribute('aria-label') || element.textContent })));
  assert.deepEqual(enabled, [], 'Controls became enabled while a global task was pending');
}

async function s6DoubleClick(locator) {
  // Two actual DOM button activations in one JS task; the second is suppressed
  // by native disabled and by the view's busy guard, not by Playwright waits.
  await locator.evaluate(element => { element.click(); element.click(); });
}

async function s6WaitProbe(fixture) {
  await fixture.page.waitForFunction(() => document.querySelector('[data-rule-index="0"] .nk-server-refresh')?.getAttribute('aria-busy') === 'false');
}

await fs.mkdir(s6Output, { recursive: true });
const s6Before = await s6Frozen('frozen-before');
assert.deepEqual(s6Before.mismatches, [], 'Protected source differs before S6 tests');
try {
  const { chromium } = await import(playwrightModule);
  s6Sources = await s6ReadSources();
  s6Browser = await chromium.launch({ executablePath: process.env.NADKOP_CHROMIUM || '/run/current-system/sw/bin/chromium', headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--renderer-process-limit=2'] });
  await s6AtomicJSON(path.join(s6Output, 'environment.json'), { started: s6Started, node: process.version, chromium: s6Browser.version(),
    rpc: s6RPCPath, rpcSha256: crypto.createHash('sha256').update(s6Sources.rpcSource).digest('hex'), themes: s6ThemeRoot,
    concurrency: 1, maxPages: 2, filter: s6Filter.source, syntheticOrigin: 'https://s6.nadkop.test', hostNetworkUsed: false });

  for (const method of ['validate', 'save', 'service', 'nodes', 'probe']) {
    for (const fault of ['success', 'reject', 'http503', 'abort', 'truncated']) {
      await s6Case('S6-rpc-' + method + '-' + fault, 'Delayed ' + method + ' / ' + fault + ', double activation and state retention', async (create, evidence) => {
        const fixture = await create();
        const expected = await s6EditDraft(fixture);
        const gate = fixture.s6Gate(method, fault === 'success' ? {} : { kind: fault });
        const notices = await fixture.page.evaluate(() => window.s6notices.length);
        if (method === 'service') {
          await fixture.page.getByRole('tab', { name: 'Diagnostics', exact: true }).click();
          await s6DoubleClick(fixture.page.getByRole('button', { name: 'Restart', exact: true }));
        } else if (method === 'nodes') {
          await s6DoubleClick(fixture.page.getByRole('button', { name: 'Refresh connections', exact: true }));
        } else if (method === 'probe') await s6DoubleClick(s6Refresh(fixture));
        else await s6DoubleClick(s6Save(fixture));
        await fixture.s6WaitCall(method);
        await fixture.page.waitForTimeout(90);
        assert.equal(fixture.s6Count(method), 1, 'Duplicate RPC before response');
        if (method === 'probe') {
          assert.equal(await s6Refresh(fixture).isDisabled(), true);
          assert.equal(await s6Refresh(fixture).getAttribute('aria-busy'), 'true');
          assert.equal(await s6Save(fixture).isEnabled(), true, 'Probe incorrectly blocks saving');
        } else await s6AssertGlobalLock(fixture);
        if (method === 'validate') assert.equal(fixture.s6Count('save'), 0, 'Save began before validation resolved');
        evidence.pendingMs = 90;
        gate.release();
        if (method === 'probe') {
          await s6WaitProbe(fixture);
          assert.equal(await s6Refresh(fixture).isEnabled(), true);
          assert.equal(await s6Rule(fixture).locator('.nk-probe-result[data-state="' + (fault === 'success' ? 'ok' : 'error') + '"]').count(), 2);
        } else {
          await fixture.s6Ready();
          if (fault !== 'success') {
            await fixture.page.waitForFunction(previous => window.s6notices.length > previous, notices);
            assert.equal(fixture.state.writes.length, 0, 'Failed RPC caused a save');
          }
        }
        await s6AssertDraft(fixture, expected); await s6AssertRestored(fixture);
        assert.equal(fixture.s6Count(method), 1, 'RPC duplicated after response');
        if (method === 'validate' || method === 'save') {
          assert.equal(fixture.state.writes.length, fault === 'success' ? 1 : 0);
          assert.equal(fixture.state.actions.length, 0);
        }
        if (method === 'service') assert.deepEqual(fixture.state.actions, fault === 'success' ? ['restart'] : []);
        if (method === 'nodes') assert.equal(await fixture.page.locator('.nk-pool-refresh').getAttribute('aria-busy'), 'false');
        if (method === 'nodes') {
          assert.equal(await fixture.page.locator('.nk-picker .nk-server-card').count(), 3, 'Node refresh lost the existing catalogue');
          assert.equal(await s6Rule(fixture).locator('.nk-server-card.is-missing').count(), 0, 'Selected server spuriously became missing');
        }
        if (fault !== 'success') assert.equal(await fixture.page.locator('.cbi-page-actions small').textContent(), method === 'save' ? 'Save not confirmed' : 'Unsaved', 'Failed RPC claimed the draft was saved');
      });
    }
  }

  await s6Case('S6-retry-after-validate-reject', 'Validation rejection retains dormant values and permits a corrected retry', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    fixture.s6Fault('validate', { kind: 'reject' });
    await s6Save(fixture).click(); await fixture.s6WaitCall('validate'); await fixture.s6Ready();
    assert.equal(fixture.s6Count('save'), 0); await s6AssertDraft(fixture, expected);
    await s6Save(fixture).click(); await fixture.s6WaitCall('save'); await fixture.s6Ready();
    assert.equal(fixture.state.writes.length, 1);
    assert.deepEqual(fixture.state.config.rules[0], expected.rules[0]);
  });

  await s6Case('S6-save-committed-response-lost', 'Save commits but response is cut: preserve draft, reject stale retry and never apply', async (create, evidence) => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    fixture.s6Fault('save', { kind: 'commit-abort' });
    await s6Apply(fixture).click(); await fixture.s6WaitCall('save'); await fixture.s6Ready();
    assert.equal(fixture.state.writes.length, 1); assert.equal(fixture.state.actions.length, 0);
    assert.equal(await fixture.page.locator('.cbi-page-actions small').textContent(), 'Save not confirmed');
    await s6AssertDraft(fixture, expected);
    await s6Apply(fixture).click(); await fixture.s6WaitCall('save', 2); await fixture.s6Ready();
    assert.equal(fixture.state.writes.length, 1); assert.equal(fixture.state.actions.length, 0);
    assert.match(await fixture.page.locator('#s6-notices').textContent(), /Configuration changed/);
    evidence.limitation = 'Committed save is ambiguous to the browser after transport loss; recovery requires reload/reconciliation.';
  });

  for (const fault of ['reject', 'abort', 'http503']) await s6Case('S6-apply-service-' + fault, 'Apply: saved revision survives service ' + fault + ', draft remains and retry uses new revision', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    fixture.s6Fault('service', { kind: fault });
    await s6Apply(fixture).click(); await fixture.s6WaitCall('service'); await fixture.s6Ready();
    assert.equal(fixture.state.writes.length, 1); assert.equal(fixture.state.actions.length, 0);
    assert.equal(await fixture.page.locator('.cbi-page-actions small').textContent(), 'Saved, apply not confirmed');
    await s6AssertDraft(fixture, expected);
    await s6Apply(fixture).click(); await fixture.s6WaitCall('service', 2); await fixture.s6Ready();
    assert.equal(fixture.state.writes.length, 2); assert.deepEqual(fixture.state.actions, ['restart']);
    assert.equal(await fixture.page.locator('.cbi-page-actions small').textContent(), 'Saved and applied');
  });

  await s6Case('S6-apply-pending-editor-lock', 'Save+Apply must keep regenerated rule editors locked until the service response returns', async (create, evidence) => {
    const fixture = await create(); await s6EditDraft(fixture);
    const service = fixture.s6Gate('service'); await s6Apply(fixture).click(); await fixture.s6WaitCall('service');
    assert.equal(fixture.state.writes.length, 1);
    const domains = s6Rule(fixture).getByLabel('Domains', { exact: true });
    evidence.regeneratedEditorDisabled = await domains.isDisabled();
    evidence.beforeEdit = await fixture.s6Snapshot();
    if (!evidence.regeneratedEditorDisabled) await domains.fill('unsaved-during-apply.s6.test');
    evidence.savedDomains = fixture.state.config.rules[0].domains;
    service.release(); await fixture.s6Ready(); evidence.afterApply = await fixture.s6Snapshot();
    assert.equal(evidence.regeneratedEditorDisabled, true, 'Save response recreated an editable textarea during pending Apply; new text is labelled saved but absent from saved config');
  });

  await s6Case('S6-apply-pending-selected-server-lock', 'Selected server cannot be removed by a newly enabled control while Apply is still pending', async (create, evidence) => {
    const fixture = await create(); await s6EditDraft(fixture);
    const service = fixture.s6Gate('service'); await s6Apply(fixture).click(); await fixture.s6WaitCall('service');
    const remove = s6Rule(fixture).getByRole('button', { name: 'Remove server from rule', exact: true }).first();
    evidence.removeDisabled = await remove.isDisabled();
    if (!evidence.removeDisabled) await remove.click();
    evidence.editorBeforeRelease = await fixture.s6Snapshot(); evidence.savedRefs = fixture.state.config.rules[0].nodeRefs;
    service.release(); await fixture.s6Ready(); evidence.afterApply = await fixture.s6Snapshot();
    assert.equal(evidence.removeDisabled, true, 'Regenerated remove-server control is active while Apply is busy; editor/server refs diverge and UI reports saved');
  });

  await s6Case('S6-rpc-adapter-ubus-control', 'Unmodified LuCI rpc.js control: ubus code 9 defaults to {}, reject:true rejects it', async (create, evidence) => {
    const fixture = await create();
    fixture.s6Fault('save', { kind: 'ubus', code: 9 }); fixture.s6Fault('save', { kind: 'ubus', code: 9 });
    evidence.defaultReply = await fixture.page.evaluate(() => window.s6rpc.declare({ object: 'nadkop', method: 'save', expect: { '': {} } })());
    evidence.strictReply = await fixture.page.evaluate(async () => {
      try { await window.s6rpc.declare({ object: 'nadkop', method: 'save', expect: { '': {} }, reject: true })(); return { rejected: false }; }
      catch (error) { return { rejected: true, message: error.message }; }
    });
    assert.deepEqual(evidence.defaultReply, {}); assert.equal(evidence.strictReply.rejected, true);
    assert.match(evidence.strictReply.message, /ubus code 9/); assert.equal(fixture.state.writes.length, 0);
  });

  for (const method of ['validate', 'save', 'service', 'nodes', 'probe']) await s6Case('S6-ubus-error-' + method, method + ' must report ubus code 9 as failure without false success or catalogue clearing', async (create, evidence) => {
    const fixture = await create(); const expected = await s6EditDraft(fixture); fixture.s6Fault(method, { kind: 'ubus', code: 9 });
    if (method === 'service') {
      await fixture.page.getByRole('tab', { name: 'Diagnostics', exact: true }).click();
      await fixture.page.getByRole('button', { name: 'Restart', exact: true }).click();
    } else if (method === 'nodes') await fixture.page.getByRole('button', { name: 'Refresh connections', exact: true }).click();
    else if (method === 'probe') await s6Refresh(fixture).click();
    else await s6Save(fixture).click();
    await fixture.s6WaitCall(method);
    if (method === 'probe') await s6WaitProbe(fixture); else await fixture.s6Ready();
    evidence.ui = await fixture.s6Snapshot(); evidence.catalogueCards = await fixture.page.locator('.nk-picker .nk-server-card').count();
    await s6AssertDraft(fixture, expected);
    if (method === 'probe') assert.equal(await s6Rule(fixture).locator('.nk-probe-result[data-state=error]').count(), 2);
    else {
      assert.ok(evidence.ui.notices.length > 0, 'Nonzero ubus status was silently accepted as {}');
      assert.equal(evidence.ui.dirty, method === 'save' ? 'Save not confirmed' : 'Unsaved', 'Failed ubus RPC was labelled saved');
      assert.equal(fixture.state.writes.length, 0);
      if (method === 'validate') assert.equal(fixture.s6Count('save'), 0, 'Save followed failed validate');
      if (method === 'nodes') assert.equal(evidence.catalogueCards, 3, 'Failed ubus nodes cleared existing catalogue');
    }
  });

  await s6Case('S6-ubus-save-apply-no-false-success', 'Save+Apply must not restart the service or claim success when ubus Save failed', async (create, evidence) => {
    const fixture = await create(); await s6EditDraft(fixture); fixture.s6Fault('save', { kind: 'ubus', code: 9 });
    await s6Apply(fixture).click(); await fixture.s6WaitCall('save'); await fixture.s6Ready();
    evidence.ui = await fixture.s6Snapshot(); evidence.writes = fixture.state.writes.length; evidence.actions = fixture.state.actions;
    assert.equal(fixture.state.actions.length, 0, 'Service restart ran after Save failed at ubus layer');
    assert.equal(evidence.ui.dirty, 'Save not confirmed'); assert.ok(evidence.ui.notices.length > 0);
  });

  await s6Case('S6-two-tabs-stale-save', 'Two tabs: stale Save+Apply cannot overwrite another tab or lose its draft', async create => {
    const state = s6InitialState(); const first = await create({ state }); const second = await create({ state, context: first.context });
    await s6EditDraft(first, 'tab-a.s6.test'); const expected = await s6EditDraft(second, 'tab-b.s6.test');
    await s6Save(first).click(); await first.s6WaitCall('save'); await first.s6Ready();
    await s6Apply(second).click(); await second.s6WaitCall('save'); await second.s6Ready();
    assert.equal(state.writes.length, 1); assert.equal(state.config.rules[0].domains[0], 'tab-a.s6.test');
    assert.equal(state.actions.length, 0); await s6AssertDraft(second, expected); await s6AssertRestored(second);
    assert.match(await second.page.locator('#s6-notices').textContent(), /Configuration changed/);
    await s6Save(first).click(); await first.s6WaitCall('save', 2); await first.s6Ready();
    assert.equal(state.writes.length, 2); assert.equal(first.s6Calls('save')[1].params.revision, 's6-r2');
  });

  await s6Case('S6-two-tabs-simultaneous-save', 'Two editors post the same revision: exactly one commit, loser retains draft', async create => {
    const state = s6InitialState(); const first = await create({ state }); const second = await create({ state, context: first.context });
    await s6EditDraft(first, 'winner.s6.test'); const expected = await s6EditDraft(second, 'loser.s6.test');
    const firstGate = first.s6Gate('save'), secondGate = second.s6Gate('save');
    await s6Save(first).click(); await s6Save(second).click(); await first.s6WaitCall('save'); await second.s6WaitCall('save');
    assert.equal(first.s6Calls('save')[0].params.revision, second.s6Calls('save')[0].params.revision);
    firstGate.release(); await first.s6Ready(); secondGate.release(); await second.s6Ready();
    assert.equal(state.writes.length, 1); assert.deepEqual(state.config.rules[0].domains, ['winner.s6.test']);
    await s6AssertDraft(second, expected); assert.match(await second.page.locator('#s6-notices').textContent(), /Configuration changed/);
  });

  await s6Case('S6-two-tabs-stale-source', 'Source commit in tab A makes tab B stale; failed source retains URL and unrelated draft', async create => {
    const state = s6InitialState(); const first = await create({ state }); const second = await create({ state, context: first.context });
    const expected = await s6EditDraft(second);
    for (const [fixture, url] of [[first, 'https://tab-a.s6.test/source'], [second, 'https://tab-b.s6.test/source']]) {
      await fixture.page.getByRole('tab', { name: 'Subscriptions', exact: true }).click();
      const source = fixture.page.getByRole('textbox', { name: 'Link or file', exact: true });
      await source.fill(url); await source.press('Enter'); await fixture.s6WaitCall('addsource'); await fixture.s6Ready();
    }
    assert.equal(state.writes.length, 1); assert.equal(state.config.sources.length, 2);
    assert.equal(await second.page.getByRole('textbox', { name: 'Link or file', exact: true }).inputValue(), 'https://tab-b.s6.test/source');
    assert.match(await second.page.locator('.nk-source-message').textContent(), /Configuration changed/);
    await s6AssertDraft(second, expected);
  });

  for (const fault of ['reject', 'abort', 'truncated']) await s6Case('S6-source-' + fault, 'Source ' + fault + ' retains typed URL, draft, inactive fields and selected IDs', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    await fixture.page.getByRole('tab', { name: 'Subscriptions', exact: true }).click();
    const source = fixture.page.getByRole('textbox', { name: 'Link or file', exact: true });
    const gate = fixture.s6Gate('addsource', { kind: fault });
    await source.fill('https://retry.s6.test/synthetic'); await source.press('Enter'); await fixture.s6WaitCall('addsource');
    await source.evaluate(element => element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
    assert.equal(fixture.s6Count('addsource'), 1); await s6AssertGlobalLock(fixture); gate.release(); await fixture.s6Ready();
    assert.equal(await source.inputValue(), 'https://retry.s6.test/synthetic'); assert.equal(await source.isEnabled(), true);
    assert.equal(await fixture.page.locator('.nk-source-message.is-error').count(), 1); await s6AssertDraft(fixture, expected);
  });

  await s6Case('S6-nodes-missing-no-substitution', 'Node catalogue refresh retains missing selected ID and never chooses the available spare', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    fixture.s6Fault('nodes', { payload: { nodes: fixture.state.servers.filter(server => server.id !== s6IDs[0]) } });
    await fixture.page.getByRole('button', { name: 'Refresh connections', exact: true }).click(); await fixture.s6WaitCall('nodes'); await fixture.s6Ready();
    await s6AssertDraft(fixture, expected);
    assert.equal(await s6Rule(fixture).locator('[data-server-id="' + s6IDs[0] + '"].is-missing').count(), 1);
    assert.equal(await s6Rule(fixture).locator('[data-server-id="' + s6IDs[2] + '"]').count(), 0);
    assert.equal(await fixture.page.locator('.nk-picker [data-server-id="' + s6IDs[0] + '"]').count(), 0);
  });

  for (const order of ['probe-first', 'save-first', 'save-reject-first']) await s6Case('S6-concurrent-' + order, 'Probe + Save settle in order ' + order + '; restore disabled exactly and retain refs', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    const probe = fixture.s6Gate('probe'); await s6Refresh(fixture).click(); await fixture.s6WaitCall('probe');
    const save = fixture.s6Gate('save', order === 'save-reject-first' ? { kind: 'reject' } : {});
    await s6Save(fixture).click(); await fixture.s6WaitCall('save'); await s6AssertGlobalLock(fixture);
    if (order === 'probe-first') {
      probe.release(); await s6WaitProbe(fixture); assert.equal(await s6Refresh(fixture).isDisabled(), true); save.release();
    } else {
      save.release(); await fixture.s6Ready(); assert.equal(await s6Refresh(fixture).isDisabled(), true); probe.release();
    }
    await fixture.s6Ready(); await s6WaitProbe(fixture); await s6AssertRestored(fixture);
    assert.equal(await s6Refresh(fixture).isEnabled(), true); await s6AssertDraft(fixture, expected);
    assert.equal(fixture.state.writes.length, order === 'save-reject-first' ? 0 : 1);
  });

  await s6Case('S6-probe-stale-url', 'Old probe response cannot paint results after selection URL changes', async create => {
    const fixture = await create();
    const oldProbe = fixture.s6Gate('probe', { payload: { results: Object.fromEntries(s6IDs.slice(0, 2).map(id => [id, { available: true, delayMs: 777 }])) } });
    await s6Refresh(fixture).click(); await fixture.s6WaitCall('probe');
    const nextProbe = fixture.s6Gate('probe', { payload: { results: Object.fromEntries(s6IDs.slice(0, 2).map(id => [id, { available: true, delayMs: 123 }])) } });
    await s6Rule(fixture).getByLabel('Check URL', { exact: true }).fill('https://new-health.s6.test/204');
    oldProbe.release(); await fixture.s6WaitCall('probe', 2);
    assert.equal(fixture.s6Calls('probe')[1].params.url, 'https://new-health.s6.test/204');
    assert.equal(await s6Rule(fixture).locator('.nk-probe-result').evaluateAll(elements => elements.some(element => element.textContent.includes('777'))), false);
    nextProbe.release(); await s6WaitProbe(fixture);
    assert.equal(await s6Rule(fixture).locator('.nk-probe-result').evaluateAll(elements => elements.every(element => element.textContent.includes('123'))), true);
    assert.deepEqual(await fixture.page.evaluate(() => window.s6editor.rules[0].nodeRefs), s6IDs.slice(0, 2));
  });

  await s6Case('S6-probe-switch-action', 'Switch away from VPN during a held probe and back preserves URL/references and starts fresh probe', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    const gate = fixture.s6Gate('probe'); await s6Refresh(fixture).click(); await fixture.s6WaitCall('probe');
    await s6Rule(fixture).getByRole('button', { name: 'Direct', exact: true }).click(); gate.release();
    await fixture.page.waitForTimeout(80); assert.equal(await s6Rule(fixture).locator('[data-probe-id]').count(), 0);
    await s6Rule(fixture).getByRole('button', { name: 'VPN', exact: true }).click(); await fixture.s6WaitCall('probe', 2); await s6WaitProbe(fixture);
    await s6AssertDraft(fixture, expected);
  });

  await s6Case('S6-probe-queue-continues', 'An aborted probe does not strand the next rule in the queue', async create => {
    const state = s6InitialState(); state.config.rules[1] = { domains: ['queued.s6.test'], action: 'proxy', nodeRef: s6IDs[2] };
    const fixture = await create({ state }); const first = fixture.s6Gate('probe', { kind: 'abort' });
    await s6Refresh(fixture).click(); await fixture.s6WaitCall('probe');
    await fixture.page.locator('[data-rule-index="1"] .nk-server-refresh').click();
    first.release(); await fixture.s6WaitCall('probe', 2);
    await fixture.page.waitForFunction(() => [...document.querySelectorAll('.nk-rule .nk-server-refresh')].every(element => element.getAttribute('aria-busy') === 'false'));
    assert.deepEqual(fixture.s6Calls('probe')[1].params.refs, [s6IDs[2]]);
    assert.equal(await fixture.page.locator('[data-rule-index="1"] .nk-probe-result[data-state=ok]').count(), 1);
  });

  await s6Case('S6-probe-busy-retry', 'One backend busy response is retried; pending clears and no selection changes', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    fixture.s6Fault('probe', { kind: 'reject', payload: { error: 'Probe already running' } });
    await s6Refresh(fixture).click(); await fixture.s6WaitCall('probe', 2); await s6WaitProbe(fixture);
    assert.equal(fixture.s6Count('probe'), 2); assert.equal(await s6Refresh(fixture).isEnabled(), true); await s6AssertDraft(fixture, expected);
  });

  await s6Case('S6-validate-timeout', 'Hung validate is timed out by the HTTP adapter; draft and controls recover', async create => {
    const fixture = await create({ rpcTimeout: 0.25 }); const expected = await s6EditDraft(fixture);
    const gate = fixture.s6Gate('validate'); await s6Save(fixture).click(); await fixture.s6WaitCall('validate');
    await fixture.s6Ready(); gate.release();
    assert.equal(fixture.s6Count('save'), 0); assert.match(await fixture.page.locator('#s6-notices').textContent(), /timed out/);
    await s6AssertDraft(fixture, expected); await s6AssertRestored(fixture);
  });

  for (const method of ['save', 'nodes', 'service']) await s6Case('S6-timeout-' + method, 'Hung ' + method + ' times out, frees controls and retains draft', async create => {
    const fixture = await create({ rpcTimeout: 0.25 }); const expected = await s6EditDraft(fixture);
    const gate = fixture.s6Gate(method, { kind: 'abort' });
    if (method === 'service') {
      await fixture.page.getByRole('tab', { name: 'Diagnostics', exact: true }).click();
      await fixture.page.getByRole('button', { name: 'Restart', exact: true }).click();
    } else if (method === 'nodes') await fixture.page.getByRole('button', { name: 'Refresh connections', exact: true }).click();
    else await s6Save(fixture).click();
    await fixture.s6WaitCall(method); await fixture.s6Ready(); gate.release();
    assert.match(await fixture.page.locator('#s6-notices').textContent(), /timed out/);
    assert.equal(fixture.state.writes.length, 0); assert.equal(fixture.state.actions.length, 0);
    await s6AssertDraft(fixture, expected); await s6AssertRestored(fixture);
  });

  await s6Case('S6-timeout-probe-retry', 'Probe HTTP timeout retries once, then recovers pending/disabled without replacing refs', async create => {
    const fixture = await create({ rpcTimeout: 0.25 }); const expected = await s6EditDraft(fixture);
    const gate = fixture.s6Gate('probe', { kind: 'abort' }); await s6Refresh(fixture).click(); await fixture.s6WaitCall('probe', 2);
    await s6WaitProbe(fixture); gate.release();
    assert.equal(fixture.s6Count('probe'), 2); assert.equal(await s6Refresh(fixture).isEnabled(), true);
    assert.equal(await s6Rule(fixture).locator('.nk-probe-result[data-state=ok]').count(), 2); await s6AssertDraft(fixture, expected);
  });

  await s6Case('S6-default-20s-save-timeout', 'Real elapsed 20s HTTP-adapter timeout: no false Save, no restart, draft and controls recover', async (create, evidence) => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    const gate = fixture.s6Gate('save', { kind: 'abort' }); const started = performance.now();
    await s6Apply(fixture).click(); await fixture.s6WaitCall('save'); await s6AssertGlobalLock(fixture);
    await fixture.page.waitForFunction(() => !document.querySelector('.cbi-page-actions button').disabled, null, { timeout: 24000 });
    evidence.elapsedMs = Math.round(performance.now() - started); gate.release();
    assert.ok(evidence.elapsedMs >= 19500 && evidence.elapsedMs < 23500, 'Default timeout did not occur near 20 seconds');
    assert.match(await fixture.page.locator('#s6-notices').textContent(), /20000 ms/);
    assert.equal(fixture.state.writes.length, 0); assert.equal(fixture.state.actions.length, 0); await s6AssertDraft(fixture, expected); await s6AssertRestored(fixture);
  });

  await s6Case('S6-probe-16-members-batch-isolation', '16 selected servers: four serial RPC batches of four; one rejected batch leaves the others visible', async create => {
    const state = s6InitialState();
    state.servers = Array.from({ length: 16 }, (_, index) => ({ id: 'srv_' + (index + 1).toString(16).padStart(32, '0'), name: 'S6 batch ' + index, protocol: 'vless', source: 'S6-Europe', available: true, position: index }));
    state.config.rules[0].nodeRefs = state.servers.map(server => server.id);
    const fixture = await create({ state }); const expected = await s6EditDraft(fixture);
    const gates = Array.from({ length: 4 }, (_, index) => fixture.s6Gate('probe', index === 1 ? { kind: 'reject' } : {}));
    await s6Refresh(fixture).click();
    for (let index = 0; index < 4; index++) {
      await fixture.s6WaitCall('probe', index + 1); await fixture.page.waitForTimeout(35);
      assert.equal(fixture.s6Count('probe'), index + 1, 'Probe batches overlapped while the previous one was held');
      assert.deepEqual(fixture.s6Calls('probe')[index].params.refs, state.config.rules[0].nodeRefs.slice(index * 4, index * 4 + 4)); gates[index].release();
    }
    await s6WaitProbe(fixture);
    assert.equal(await s6Rule(fixture).locator('.nk-probe-result[data-state=ok]').count(), 12);
    assert.equal(await s6Rule(fixture).locator('.nk-probe-result[data-state=error]').count(), 4); await s6AssertDraft(fixture, expected);
  });

  for (const action of ['proxy', 'direct']) await s6Case('S6-inactive-selection-' + action + '-save-error', 'Save rejection retains inactive selection URL in ' + action + ' action and all dormant matcher values', async create => {
    const state = s6InitialState(); state.config.rules[0].action = action; state.config.rules[0].selection.mode = 'ping';
    const fixture = await create({ state }); const expected = await s6EditDraft(fixture);
    assert.equal(await s6Rule(fixture).getByLabel('Check URL', { exact: true }).count(), 0);
    fixture.s6Fault('save', { kind: 'reject' }); await s6Save(fixture).click(); await fixture.s6WaitCall('save'); await fixture.s6Ready();
    await s6AssertDraft(fixture, expected); assert.equal(fixture.state.writes.length, 0);
    if (action === 'direct') await s6Rule(fixture).getByRole('button', { name: 'VPN', exact: true }).click();
    await s6Rule(fixture).getByLabel('Server selection', { exact: true }).selectOption('url');
    assert.equal(await s6Rule(fixture).getByLabel('Check URL', { exact: true }).inputValue(), expected.rules[0].selection.url);
  });

  await s6Case('S6-invalid-local-json', 'Invalid local nodes JSON never reaches validate/save and all controls recover', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    await fixture.page.getByRole('tab', { name: 'Import', exact: true }).click();
    await fixture.page.getByText('Local nodes', { exact: true }).click(); await fixture.page.getByLabel('JSON', { exact: true }).fill('{broken-s6-json');
    await s6Save(fixture).click(); await fixture.s6Ready();
    assert.equal(fixture.s6Count('validate'), 0); assert.equal(fixture.s6Count('save'), 0); assert.ok((await fixture.page.locator('#s6-notices').textContent()).length > 0);
    assert.equal(await fixture.page.getByLabel('JSON', { exact: true }).inputValue(), '{broken-s6-json'); await s6AssertDraft(fixture, expected); await s6AssertRestored(fixture);
  });

  await s6Case('S6-focus-local-rule-error', 'Local empty-rule validation opens Rules and focuses its invalid matcher', async create => {
    const state = s6InitialState(); state.config.rules[0].domains = [];
    const fixture = await create({ state });
    await s6Rule(fixture).getByRole('button', { name: 'Collapse S6 route', exact: true }).click();
    await fixture.page.getByRole('tab', { name: 'Settings', exact: true }).click(); await s6Save(fixture).click();
    await fixture.page.waitForFunction(() => document.activeElement?.getAttribute('aria-invalid') === 'true');
    assert.equal(fixture.s6Count('validate'), 0); assert.equal(fixture.s6Count('save'), 0);
    assert.equal(await fixture.page.getByRole('tab', { name: 'Rules', exact: true }).getAttribute('aria-selected'), 'true');
    assert.equal(await s6Rule(fixture).locator('.nk-rule-body').isVisible(), true);
    assert.equal(await fixture.page.evaluate(() => document.getElementById(document.activeElement.getAttribute('aria-describedby'))?.getAttribute('role')), 'alert');
  });

  for (const method of ['validate', 'save']) await s6Case('S6-focus-' + method + '-rule-error', 'Backend ' + method + ' rule error opens target field and focuses it, preserving inactive values', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture, 'invalid_domain.s6.test');
    fixture.s6Fault(method, { kind: 'reject', payload: { error: 'Invalid rule', message: 'S6: invalid domain', ruleIndex: 0, field: 'domains' } });
    await s6Rule(fixture).getByRole('button', { name: 'Collapse S6 route', exact: true }).click();
    await fixture.page.getByRole('tab', { name: 'Diagnostics', exact: true }).click(); await s6Save(fixture).click();
    await fixture.page.waitForFunction(() => document.activeElement?.getAttribute('aria-invalid') === 'true');
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.getAttribute('aria-label')), 'Domains');
    assert.equal(await fixture.page.getByRole('tab', { name: 'Rules', exact: true }).getAttribute('aria-selected'), 'true');
    assert.match(await s6Rule(fixture).locator('.nk-rule-error').textContent(), /invalid domain/);
    assert.equal(fixture.state.writes.length, 0); await s6AssertDraft(fixture, expected); await s6AssertRestored(fixture);
  });

  await s6Case('S6-keyboard-controls', 'Roving tablist, Enter activation, Space matcher toggles, Escape/Enter rename and focus recovery', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    await fixture.page.getByRole('tab', { name: 'Rules', exact: true }).focus(); await fixture.page.keyboard.press('Tab');
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.getAttribute('role') === 'tab'), false, 'Tab must leave the roving tablist');
    await fixture.page.keyboard.press('Shift+Tab');
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.textContent), 'Rules');
    await fixture.page.keyboard.press('ArrowRight');
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.textContent), 'Subscriptions');
    await fixture.page.keyboard.press('Enter');
    assert.equal(await fixture.page.getByRole('tab', { name: 'Subscriptions', exact: true }).getAttribute('aria-selected'), 'true');
    await fixture.page.getByRole('tab', { name: 'Rules', exact: true }).focus(); await fixture.page.keyboard.press('Enter');
    const ips = s6Rule(fixture).getByRole('button', { name: 'IP / CIDR', exact: true });
    await ips.focus(); await fixture.page.keyboard.press('Space');
    assert.equal(await s6Rule(fixture).getByLabel('IP / CIDR', { exact: true }).isVisible(), true);
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.getAttribute('data-matcher-key')), 'ips');
    await fixture.page.keyboard.press('Space'); await s6AssertDraft(fixture, expected);
    await s6Rule(fixture).getByRole('button', { name: 'Rename S6 route', exact: true }).focus(); await fixture.page.keyboard.press('Enter');
    const name = s6Rule(fixture).getByRole('textbox', { name: 'Rule name 1', exact: true });
    await name.fill('S6 cancelled'); await fixture.page.keyboard.press('Escape');
    assert.equal(await s6Rule(fixture).locator('.nk-rule-title').textContent(), 'S6 route');
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.getAttribute('aria-label')), 'Rename S6 route');
    await fixture.page.keyboard.press('Enter'); await name.fill('S6 committed'); await fixture.page.keyboard.press('Enter');
    assert.equal(await s6Rule(fixture).locator('.nk-rule-title').textContent(), 'S6 committed');
  });

  await s6Case('S6-keyboard-tab-arrow-pattern', 'ARIA tablist supports Right/Home/End navigation and updates focused/selected tab', async create => {
    const fixture = await create();
    await fixture.page.getByRole('tab', { name: 'Rules', exact: true }).focus(); await fixture.page.keyboard.press('ArrowRight');
    assert.equal(await fixture.page.evaluate(() => document.activeElement?.textContent), 'Subscriptions', 'ArrowRight must move to the next ARIA tab');
    await fixture.page.keyboard.press('Enter');
    assert.equal(await fixture.page.getByRole('tab', { name: 'Subscriptions', exact: true }).getAttribute('aria-selected'), 'true');
    await fixture.page.keyboard.press('End'); assert.equal(await fixture.page.evaluate(() => document.activeElement?.textContent), 'Diagnostics');
    await fixture.page.keyboard.press('Home'); assert.equal(await fixture.page.evaluate(() => document.activeElement?.textContent), 'Rules');
  });

  await s6Case('S6-poll-return-during-save-lock', 'Status poll already in flight must keep regenerated service controls locked during Save', async (create, evidence) => {
    const fixture = await create(); await s6EditDraft(fixture);
    const status = fixture.s6Gate('status');
    await fixture.page.evaluate(() => { window.s6pendingPoll = window.s6poll[0](); }); await fixture.s6WaitCall('status', 2);
    const save = fixture.s6Gate('save'); await s6Save(fixture).click(); await fixture.s6WaitCall('save');
    await fixture.page.getByRole('tab', { name: 'Diagnostics', exact: true }).click(); status.release();
    await fixture.page.evaluate(() => window.s6pendingPoll);
    const restart = fixture.page.getByRole('button', { name: 'Restart', exact: true });
    evidence.regeneratedServiceButtonDisabled = await restart.isDisabled();
    await restart.evaluate(element => element.click());
    assert.equal(fixture.s6Count('service'), 0, 'Busy guard allowed a service mutation');
    evidence.whileSavePending = await fixture.s6Snapshot(); save.release(); await fixture.s6Ready();
    assert.equal(evidence.regeneratedServiceButtonDisabled, true, 'In-flight poll recreated enabled service buttons while save was busy');
  });

  await s6Case('S6-title-double-click-during-save', 'Double-clicking the text title during Save must not create an enabled editor outside the busy lock', async (create, evidence) => {
    const fixture = await create(); await s6EditDraft(fixture);
    const validation = fixture.s6Gate('validate'); await s6Save(fixture).click(); await fixture.s6WaitCall('validate');
    await s6Rule(fixture).locator('.nk-rule-title').dblclick();
    const editor = s6Rule(fixture).getByRole('textbox', { name: 'Rule name 1', exact: true });
    evidence.titleEditorCount = await editor.count();
    evidence.titleEditorEnabled = evidence.titleEditorCount > 0 && await editor.isEnabled();
    if (evidence.titleEditorEnabled) { await editor.fill('S6 unsaved name during save'); await editor.press('Enter'); }
    validation.release(); await fixture.s6WaitCall('save'); await fixture.s6Ready();
    evidence.finalUI = await fixture.s6Snapshot(); evidence.savedName = fixture.state.config.rules[0].name;
    assert.equal(evidence.titleEditorEnabled, false, 'Double-click title opened an editable name during Save; it diverges from saved name but status says saved');
  });

  await s6Case('S6-geodata-return-during-save-lock', 'Late geodata cannot enable editable fields inside Save or mark an unsaved new edit as saved', async (create, evidence) => {
    let release;
    const hold = new Promise(resolve => { release = resolve; });
    const fixture = await create({ waitGeo: false, initialFaults: [['geodata', { hold }]] }); await s6EditDraft(fixture);
    const validation = fixture.s6Gate('validate'); await s6Save(fixture).click(); await fixture.s6WaitCall('validate');
    await fixture.page.getByRole('tab', { name: 'Settings', exact: true }).click();
    await fixture.page.getByText('Routing databases', { exact: true }).click(); release();
    await fixture.page.waitForFunction(() => document.querySelector('.nk-geo-settings')?.textContent.includes('Sets:'));
    const url = fixture.page.getByLabel('Geosite URL', { exact: true }); await url.waitFor();
    evidence.lateFieldDisabled = await url.isDisabled();
    if (!evidence.lateFieldDisabled) await url.fill('https://unsaved-edit.s6.test/geosite.dat');
    evidence.beforeRelease = await fixture.s6Snapshot(); validation.release(); await fixture.s6WaitCall('save'); await fixture.s6Ready();
    evidence.afterRelease = await fixture.s6Snapshot(); evidence.savedGeodata = fixture.state.config.geodata || null;
    assert.equal(evidence.lateFieldDisabled, true, 'Late geodata created editable fields while validate/save was pending; an uncommitted edit is then labelled saved');
  });

  await s6Case('S6-late-source-header-json', 'Invalid synthetic header JSON reports an error and frees the source entry for retry', async create => {
    const fixture = await create(); const expected = await s6EditDraft(fixture);
    await fixture.page.getByRole('tab', { name: 'Subscriptions', exact: true }).click();
    await fixture.page.locator('.nk-source-entry summary').click();
    await fixture.page.locator('.nk-source-entry').getByLabel('HTTP headers', { exact: true }).fill('{broken-s6-json');
    const source = fixture.page.getByRole('textbox', { name: 'Link or file', exact: true });
    await source.fill('https://headers.s6.test/sub'); await source.press('Enter');
    await fixture.s6Ready(); assert.equal(fixture.s6Count('addsource'), 0);
    assert.equal(await source.inputValue(), 'https://headers.s6.test/sub');
    assert.ok((await fixture.page.locator('#s6-notices').textContent()).length > 0); await s6AssertDraft(fixture, expected);
  });

  for (const theme of Object.keys(s6Themes)) for (const width of [1360, 390]) for (const scheme of ['light', 'dark']) {
    await s6Case('S6-theme-' + theme + '-' + width + '-' + scheme, theme + ' ' + width + 'px ' + scheme + ': five panes, error focus and draft retention', async (create, evidence) => {
      const fixture = await create(); await fixture.page.setViewportSize({ width, height: 950 }); await s6SetTheme(fixture, theme, scheme);
      evidence.themeState = await fixture.page.evaluate(() => ({ darkAttribute: document.documentElement.getAttribute('data-darkmode'), bodyBackground: getComputedStyle(document.body).backgroundColor,
        prefersDark: matchMedia('(prefers-color-scheme:dark)').matches }));
      assert.equal(evidence.themeState.darkAttribute, String(scheme === 'dark'));
      assert.equal(evidence.themeState.prefersDark, scheme === 'dark');
      const expected = await s6EditDraft(fixture); evidence.overflowByPane = {};
      for (const tab of ['Rules', 'Subscriptions', 'Settings', 'Import', 'Diagnostics']) {
        await fixture.page.getByRole('tab', { name: tab, exact: true }).click();
        await fixture.page.locator('[role=tabpanel]:not([hidden]) details').evaluateAll(elements => elements.forEach(element => { element.open = true; }));
        const overflow = await fixture.page.evaluate(() => document.documentElement.scrollWidth - innerWidth); evidence.overflowByPane[tab] = overflow;
        assert.ok(overflow <= 1, theme + ' ' + width + ' ' + tab + ' horizontal overflow ' + overflow + 'px');
        assert.equal(await fixture.page.locator('[role=tabpanel]:not([hidden])').count(), 1);
      }
      fixture.s6Fault('validate', { kind: 'reject', payload: { error: 'S6 rule error', message: 'S6 mobile error', ruleIndex: 0, field: 'domains' } });
      await s6Save(fixture).click();
      await fixture.page.waitForFunction(() => document.activeElement?.getAttribute('aria-invalid') === 'true');
      await s6AssertRestored(fixture); await s6AssertDraft(fixture, expected);
      assert.equal(await fixture.page.evaluate(() => { const box = document.activeElement.getBoundingClientRect(); return box.left >= 0 && box.right <= innerWidth + 1 && box.top >= 0 && box.top < innerHeight; }), true, 'Invalid focused input is off-screen');
      await fixture.page.screenshot({ path: path.join(s6Output, 'S6-theme-' + theme + '-' + width + '-' + scheme + '.png'), fullPage: true });
    });
  }
} finally {
  if (s6Browser) await s6Browser.close();
  const after = await s6Frozen('frozen-after');
  const summary = { started: s6Started, finished: new Date().toISOString(), filter: s6Filter.source,
    total: s6Results.length, pass: s6Results.filter(result => result.status === 'PASS').length,
    fail: s6Results.filter(result => result.status === 'FAIL').length, sourceBefore: { files: s6Before.filesChecked, mismatches: s6Before.mismatches.length },
    sourceAfter: { files: after.filesChecked, mismatches: after.mismatches.length },
    sameSourceHashes: JSON.stringify(s6Before.sha256) === JSON.stringify(after.sha256), results: s6Results };
  await s6AtomicJSON(path.join(s6Output, 'results.json'), summary);
  console.log('S6 SUMMARY ' + JSON.stringify({ total: summary.total, pass: summary.pass, fail: summary.fail, sourceBefore: summary.sourceBefore, sourceAfter: summary.sourceAfter, sameSourceHashes: summary.sameSourceHashes }));
  console.log('S6 ARTIFACTS ' + s6Output);
  assert.deepEqual(after.mismatches, [], 'Protected source differs after S6 tests');
  if (summary.fail) process.exitCode = 1;
}

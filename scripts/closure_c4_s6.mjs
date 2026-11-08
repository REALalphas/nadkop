// Reuse the original S6 cases/mocks, freezing only C4-owned sources during
// authorized concurrent blocker work. Baseline uses saved, unmodified assets.
import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import { spawnSync } from 'node:child_process';

const root = path.resolve(import.meta.dirname, '..');
const baseline = process.argv.includes('--baseline');
const sourceRoot = baseline ? path.join(root, 'build/blocker-closure/baseline') : root;
const output = path.resolve(process.env.S6_ARTIFACT_DIR || path.join(root, 'build/blocker-closure/C4', baseline ? 'baseline' : 's6'));
if (!output.startsWith(path.join(root, 'build/blocker-closure/C4/'))) throw new Error('C4 artifacts must stay in C4');
await fs.mkdir(output, { recursive: true });
const assets = 'openwrt/luci-app-nadkop/htdocs/luci-static/resources/';
const files = {};
for (const file of [path.join(sourceRoot, assets, 'view/nadkop/overview.js'), path.join(sourceRoot, assets, 'nadkop/picker.js'),
  path.join(sourceRoot, assets, 'nadkop/picker.css'), path.join(root, 'scripts/test-s6-luci-resilience.mjs'),
  path.join(root, 'scripts/fixtures/s6-luci-harness.mjs'),
  path.resolve(root, process.env.S6_LUCI_RPC || 'build/openwrt-vm/sdk-24.10.8/feeds/luci/modules/luci-base/htdocs/luci-static/resources/rpc.js')]) {
  files[path.relative(root, file)] = crypto.createHash('sha256').update(await fs.readFile(file)).digest('hex');
}
const manifest = path.join(output, 'c4-source-manifest.json');
await fs.writeFile(manifest, JSON.stringify({ baseline, files }, null, 2) + '\n');
const result = spawnSync(process.execPath, ['scripts/test-s6-luci-resilience.mjs'], {
  cwd: root, stdio: 'inherit', env: { ...process.env, S6_SOURCE_ROOT: sourceRoot, S6_MANIFEST: manifest, S6_ARTIFACT_DIR: output }
});
process.exitCode = result.status ?? 1;

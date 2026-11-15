// Screenshots for the FUOTA operator guide. The OTA window runs on its preview
// page with simulated API answers and demonstration identities (N0CALL);
// no Graywolf instance, radio or station is contacted.
import { chromium } from 'playwright';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

const base = process.env.GW_OTA_TEST_BASE || 'http://127.0.0.1:5187';
const out = resolve('docs/handbook/assets/radio-ota');
const image = process.argv[2];
if (!image) throw new Error('usage: node radio-ota-shots.mjs <OTA firmware.bin used as the selected file>');
const settings = {
  target_board: 'ttgo-lora32-v21', bridge: '/dev/serial/by-id/usb-DEMO_controller-if00', via: 'N0CALL-10',
  relay_recipient: '', chain: [], has_route_secret: true,
  normal: { frequency_hz: 433775000, bandwidth_hz: 125000, spreading_factor: 12, coding_rate: 5, power_dbm: 20 },
  transfer: { frequency_hz: 434500000, bandwidth_hz: 500000, spreading_factor: 7, coding_rate: 5, power_dbm: 9 },
};
const bytes = readFileSync(image);
const sha = createHash('sha256').update(bytes).digest('hex');
const common = { target_board: 'ttgo-lora32-v21', block_size: 128, expected_version: '4.0.2RXT-OTA2.2', sha256: sha, total: bytes.length };
const jobs = {
  ready: { ...common, id: 'p', state: 'ready', phase: 'ready', written: 0, options: { preflight_only: true }, roles_changed: ['N0CALL-10', 'N0CALL-15'],
    route_proof: { version: '4.0.2RXT-OTA2.1', running: 0x10000, boot: 0x10000, pending: false } },
  running: { ...common, id: 'u', state: 'running', phase: 'sending', written: 794624, can_pause: true, options: {},
    route_proof: { version: '4.0.2RXT-OTA2.1', running: 0x10000, boot: 0x10000, pending: false } },
  succeeded: { ...common, id: 'u', state: 'succeeded', phase: 'confirmed', written: bytes.length, options: {}, post_install_verified: true,
    route_proof: { version: '4.0.2RXT-OTA2.1', running: 0x10000, boot: 0x10000, pending: false },
    final: { version: '4.0.2RXT-OTA2.2', running: 0x1f0000, boot: 0x1f0000, pending: false } },
};
let job = null;
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM || '/usr/bin/chromium', args: ['--no-sandbox', '--lang=en-US'], env: { ...process.env, LANG: 'en_US.UTF-8', LANGUAGE: 'en_US' } });
const page = await (await browser.newContext({ viewport: { width: 1000, height: 2600 }, deviceScaleFactor: 2, locale: 'en-US' })).newPage();
await page.route('**/api/**', async route => {
  const path = new URL(route.request().url()).pathname;
  let data = {};
  if (path === '/api/kiss/available-serial-ports') data = [{ path: settings.bridge, description: 'Controller board' }];
  else if (path === '/api/radio-ota/DEMO') data = { controller: 'N0CALL-2', signed_target: 'N0CALL-15', settings, stable_ports: [settings.bridge], job, engine_ready: true, engine_error: '' };
  else throw new Error('Unexpected API request ' + path);
  await route.fulfill({ status: 200, json: data });
});
const dialog = () => page.locator('[role="dialog"]').first();
async function open(state) {
  job = state ? jobs[state] : null;
  await page.goto(base + '/tests/radio-ota.html');
  await page.getByText('View / edit radio settings', { exact: true }).waitFor();
}
// Crop the dialog to the region from the top of `from` to the bottom of `to`,
// both scrolled into view, so each screenshot stays legible once printed.
async function shot(name, from, to) {
  await from.scrollIntoViewIfNeeded();
  await page.waitForTimeout(250);
  const box = await dialog().boundingBox();
  const a = await from.boundingBox(), b = await to.boundingBox();
  const top = Math.max(box.y, a.y - 12), bottom = b.y + b.height + 12;
  if (bottom - top < 40) throw new Error(`${name}: empty crop`);
  // The footer with its Close button starts where the scrolling body ends.
  const footer = await dialog().getByRole('button', { name: 'Close', exact: true }).boundingBox();
  if (bottom > footer.y - 8) throw new Error(`${name}: region does not fit; enlarge the viewport`);
  await page.screenshot({ path: `${out}/${name}.png`, clip: { x: box.x, y: top, width: box.width, height: bottom - top } });
  console.log(name, Math.round(bottom - top));
}
const label = text => page.getByText(text, { exact: true }).first();
const button = name => page.getByRole('button', { name, exact: true });
try {
  await open(null);
  await button('View / edit radio settings').click();
  await page.getByLabel('Target board (required)', { exact: true }).waitFor();
  await shot('01-settings', label('Save once for this station'), page.locator('#ota-relay-chain'));
  await shot('02-route-key', label('Shared route key (saved; leave blank to keep)'), page.getByText('Target uses a different service profile').first());
  await shot('03-settings-profiles', label('Controller / relay service profile'), button('Save OTA settings'));
  await open(null);
  await page.locator('input[type="file"]').setInputFiles(image);
  await page.getByLabel('Expected firmware version', { exact: true }).fill('4.0.2RXT-OTA2.2');
  await page.getByLabel('Discard the previous inactive candidate').check();
  await shot('04-this-update', label('Application firmware (.bin)'), button('Update firmware'));
  await open('ready');
  await shot('05-route-checked', label('Route checked; no firmware transferred'), page.getByText('Image SHA-256:').first());
  await open('running');
  await shot('06-progress', page.locator('progress').first(), button('Pause'));
  await open('succeeded');
  await shot('07-succeeded', page.locator('progress').first(), page.getByText('Image SHA-256:').first());
} finally {
  await browser.close();
}

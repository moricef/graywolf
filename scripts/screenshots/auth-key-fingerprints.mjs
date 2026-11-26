// Documentation-only rendering with a public test key. No service or radio is contacted.
import { chromium } from 'playwright';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';

const fingerprint = createHash('sha256').update(Buffer.from(Array.from({ length: 32 }, (_, i) => i))).digest('hex').slice(0, 6);
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM || '/usr/bin/chromium', args: ['--no-sandbox'] });
try {
  const page = await browser.newPage({ viewport: { width: 1080, height: 450 }, deviceScaleFactor: 2 });
  await page.route('**/*', route => route.abort());
  await page.setContent(`<!doctype html><html lang="en"><meta charset="utf-8"><style>
    *{box-sizing:border-box}body{margin:0;padding:24px;font:18px Arial;color:#202938;background:white}
    h1{font-size:24px;margin:0 0 10px}p{margin:8px 0 20px;font-size:15px}
    main{display:grid;grid-template-columns:1fr 1fr;gap:24px}section{border:1px solid #b9c5ce;padding:22px}
    h2{font-size:21px;margin:0 0 20px}label{display:block;font-size:16px;margin-bottom:9px}
    input{width:100%;padding:12px;border:1px solid #9ca8b2;background:white;color:#586574;font-size:15px}
    .fingerprint{padding:14px 10px;border:2px solid #167895;margin:16px 0 0;font-size:16px;line-height:1.5}
    strong{font-family:monospace;font-size:20px}.firmware{background:#202428;color:#f3f5f7}
    footer{margin-top:18px;font-size:14px;color:#586574}
  </style><h1>Remote command key: compare the two saved fingerprints</h1>
  <p>Local illustration with demonstration data. Not a connection to a board.</p><main>
  <section><h2>Graywolf / Key settings</h2><label>43-character Base64URL key</label>
  <input readonly value="(leave blank to keep current key)">
  <div class="fingerprint">Saved key fingerprint: <strong>${fingerprint}</strong></div></section>
  <section class="firmware"><h2>iGate / Remote Management</h2><label>Remote command secret</label>
  <input readonly value="Leave blank to keep the installed key">
  <div class="fingerprint">Authentication key installed<br>fingerprint <strong>${fingerprint}</strong></div></section>
  </main><footer>Same command key in both applications. The radio update route key is separate.</footer></html>`);
  await page.screenshot({ path: resolve('docs/handbook/assets/authenticated-control-fr/08-key-fingerprints.png'), fullPage: true });
  console.log(`Demonstration fingerprint: ${fingerprint}`);
} finally {
  await browser.close();
}

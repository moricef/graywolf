// Prints the operator guides to PDF. Chromium resolves relative links against
// the local file path, which would leak the build machine's directories into
// the PDF; every relative link is rewritten first to its public GitHub URL,
// and links to a guide's HTML page point to that guide's PDF.
import { chromium } from 'playwright';
import { resolve } from 'node:path';

const PUBLIC = 'https://github.com/moricef/graywolf/blob/feature/rxt-telemetry/docs/handbook/';
const GUIDES = {
  'radio-ota-en.html': 'Graywolf_radio_OTA_en.pdf',
  'radio-ota-fr.html': 'Graywolf_radio_OTA_fr.pdf',
  'authenticated-control-en.html': 'Graywolf_authenticated_control_en.pdf',
  'authenticated-control-fr.html': 'Graywolf_commandes_authentifiees_fr.pdf',
};
const only = process.argv.slice(2);
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM || '/usr/bin/chromium', args: ['--no-sandbox'] });
try {
  for (const [html, pdf] of Object.entries(GUIDES)) {
    if (only.length && !only.includes(html)) continue;
    const page = await browser.newPage();
    await page.goto('file://' + resolve('docs/handbook', html), { waitUntil: 'load' });
    const leaked = await page.evaluate(({ PUBLIC, GUIDES }) => {
      for (const a of document.querySelectorAll('a[href]')) {
        const href = a.getAttribute('href');
        if (/^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith('#')) continue;
        const [path, hash] = href.split('#');
        const name = path.split('/').pop();
        a.setAttribute('href', GUIDES[name] ? PUBLIC + GUIDES[name] : new URL(href, PUBLIC).href);
        if (hash && !GUIDES[name]) a.setAttribute('href', a.getAttribute('href').split('#')[0] + '#' + hash);
      }
      return [...document.querySelectorAll('a[href]')].map(a => a.href).filter(u => u.startsWith('file:'));
    }, { PUBLIC, GUIDES });
    if (leaked.length) throw new Error(`${html}: local links remain: ${leaked.join(', ')}`);
    await page.pdf({ path: resolve('docs/handbook', pdf), format: 'A4', printBackground: true });
    await page.close();
    console.log(`${pdf} written`);
  }
} finally {
  await browser.close();
}

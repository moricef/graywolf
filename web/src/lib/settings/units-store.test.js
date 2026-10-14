import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { compileModule } from 'svelte/compiler';

let moduleId = 0;
function deferred() {
  let resolve;
  const promise = new Promise(r => { resolve = r; });
  return { promise, resolve };
}
const response = system => ({ ok: true, status: 200, json: async () => ({ system }) });

async function loadStore(fetch) {
  const values = new Map([['units-system', 'imperial']]);
  const errors = [];
  globalThis.localStorage = { getItem: k => values.get(k), setItem: (k, v) => values.set(k, v) };
  globalThis.fetch = fetch;
  globalThis.unitsTestErrors = errors;
  const source = readFileSync(new URL('./units-store.svelte.js', import.meta.url), 'utf8')
    .replace("import { toasts } from '../stores.js';", 'const toasts = { error: message => globalThis.unitsTestErrors.push(message) };');
  // Compile the actual store. Server generation removes reactive rendering;
  // these tests exercise request ordering and persistence, not DOM updates.
  const { js } = compileModule(source, { filename: 'units-store.svelte.js', generate: 'server' });
  const code = js.code.replace(/^import \* as \$ from 'svelte\/internal\/server';$/m, '');
  const { unitsState } = await import('data:text/javascript,' + encodeURIComponent(code) + '#' + moduleId++);
  return { store: unitsState, values, errors };
}

test('an old page-load response cannot undo a saved choice', async () => {
  const oldGet = deferred();
  const { store, values } = await loadStore((url, opts) => opts?.method === 'PUT'
    ? Promise.resolve(response(JSON.parse(opts.body).system)) : oldGet.promise);
  const loading = store.fetchConfig();
  await store.setSystem('metric');
  oldGet.resolve(response('imperial'));
  await loading;
  assert.equal(store.system, 'metric');
  assert.equal(values.get('units-system'), 'metric');
});

test('entering Preferences during a pending save does not read the old value', async () => {
  const put = deferred();
  const requests = [];
  const { store } = await loadStore((url, opts) => { requests.push(opts?.method || 'GET'); return put.promise; });
  const saving = store.setSystem('metric');
  await store.fetchConfig();
  await Promise.resolve();
  assert.deepEqual(requests, ['PUT']);
  assert.equal(store.system, 'metric');
  put.resolve(response('metric'));
  await saving;
});

test('rapid changes reach the server in order without restoring an old response', async () => {
  const first = deferred();
  const second = deferred();
  const choices = [];
  const { store, values } = await loadStore((url, opts) => {
    choices.push(JSON.parse(opts.body).system);
    return choices.length === 1 ? first.promise : second.promise;
  });
  const a = store.setSystem('metric');
  const b = store.setSystem('imperial');
  await Promise.resolve();
  assert.deepEqual(choices, ['metric']);
  first.resolve(response('metric'));
  await a;
  assert.equal(store.system, 'imperial');
  await Promise.resolve();
  assert.deepEqual(choices, ['metric', 'imperial']);
  second.resolve(response('imperial'));
  await b;
  assert.equal(values.get('units-system'), 'imperial');
});

test('failed saves roll back to the last confirmed choice', async () => {
  const { store, values, errors } = await loadStore(async () => { throw new Error('offline'); });
  const a = store.setSystem('metric');
  const b = store.setSystem('imperial');
  const c = store.setSystem('metric');
  await Promise.all([a, b, c]);
  assert.equal(store.system, 'imperial');
  assert.equal(values.get('units-system'), 'imperial');
  assert.equal(errors.length, 1);
});

test('startup loads the saved metric preference into a fresh browser mirror', async () => {
  const { store, values } = await loadStore(async () => response('metric'));
  await store.fetchConfig();
  assert.equal(store.system, 'metric');
  assert.equal(values.get('units-system'), 'metric');
});

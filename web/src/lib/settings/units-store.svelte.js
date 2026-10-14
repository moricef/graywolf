// Reactive unit system preference backed by the server (sqlite) with
// a localStorage mirror so the initial render before fetchConfig()
// resolves still respects the last known value — avoids a flicker
// from imperial to metric on page load for metric operators.

import { toasts } from '../stores.js';

const LS_KEY = 'units-system';

function normalize(v) {
  return v === 'metric' ? 'metric' : 'imperial';
}

function readStored() {
  try { return normalize(localStorage.getItem(LS_KEY)); }
  catch { return 'imperial'; }
}

function writeStored(v) {
  try { localStorage.setItem(LS_KEY, v); } catch {}
}

export const unitsState = (() => {
  const initial = readStored();
  let system = $state(initial);
  let confirmedSystem = initial;
  let revision = 0;
  let fetchSequence = 0;
  let pendingSaves = 0;
  let saveQueue = Promise.resolve();

  async function fetchConfig() {
    // A page mount must not read the old database value during a save.
    if (pendingSaves > 0) return;
    const startedRevision = revision;
    const sequence = ++fetchSequence;
    try {
      const res = await fetch('/api/preferences/units', { credentials: 'same-origin' });
      if (res.status === 401) return;
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      // Ignore reads that started before a choice or a newer reload.
      if (startedRevision !== revision || sequence !== fetchSequence || pendingSaves > 0) return;
      const next = normalize(data?.system);
      confirmedSystem = next;
      system = next;
      writeStored(next);
    } catch {
      // Offline or not yet authenticated — stick with the localStorage
      // mirror we initialized from.
    }
  }

  function setSystem(next) {
    const v = normalize(next);
    const choiceRevision = ++revision;
    system = v;
    writeStored(v);
    pendingSaves++;
    // Serialize writes so rapid changes also reach SQLite in choice order.
    saveQueue = saveQueue.then(async () => {
      try {
        const res = await fetch('/api/preferences/units', {
          method: 'PUT',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ system: v }),
        });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = await res.json();
        confirmedSystem = normalize(data?.system);
        if (choiceRevision === revision) {
          system = confirmedSystem;
          writeStored(confirmedSystem);
        }
      } catch {
        if (choiceRevision === revision) {
          system = confirmedSystem;
          writeStored(confirmedSystem);
          toasts.error("Couldn't save units preference — try again.");
        }
      } finally {
        pendingSaves--;
      }
    });
    return saveQueue;
  }

  return {
    get system() { return system; },
    set system(v) { setSystem(v); },

    get isMetric() { return system === 'metric'; },

    fetchConfig,
    setSystem,
  };
})();

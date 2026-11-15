import { getBearerToken } from '../androidBridge.js';

// OTA never uses mock data: a network failure must not look like a completed update.
export async function otaRequest(path, { method = 'GET', body, binary = false } = {}) {
  const headers = {};
  const token = getBearerToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) headers['Content-Type'] = binary ? 'application/octet-stream' : 'application/json';
  const res = await fetch(`/api${path}`, {
    method, credentials: 'same-origin', headers,
    body: body === undefined ? undefined : binary ? body : JSON.stringify(body),
  });
  if (res.status === 401) window.location.hash = '#/login';
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

export function otaProgress(job) {
  if (job?.state === 'succeeded') return 100;
  return job?.total > 0 ? Math.min(99, Math.floor(100 * job.written / job.total)) : 0;
}
export function otaPhase(job) {
  if (!job) return 'Ready';
  if (job.state === 'ready') return 'Route checked; no firmware transferred';
  if (job.state === 'stale') return job.error || 'Settings changed; check preparation again';
  if (job.state === 'succeeded') return job.post_install_verified ? 'Firmware updated, confirmed and installation verified' : 'Firmware confirmed over radio; final readback not recorded by this older job';
  if (job.state === 'interrupted') return job.error || 'Update interrupted by a Graywolf restart';
  if (job.state === 'failed') return job.error || 'Update failed';
  if (job.state === 'paused') return 'Paused. Allow up to one minute for radio recovery, then restart with the same firmware.';
  return ({ preparing_route:'Preparing signed radio route', checking_route:'Checking authenticated route', releasing_session:'Releasing the check session without discarding its checkpoint', connecting: 'Contacting station', sending: 'Sending firmware', verifying: 'Verifying firmware',
    confirming: 'Starting and confirming new firmware', waiting_rollback: 'Validation: withholding confirmation for rollback',
    reactivating: 'Validation: rollback observed, reactivating candidate', checking_installation: 'Reading back installed version, partition and SHA-256', recovering: 'Recovering radio link',
    repairing: 'Repairing missing blocks', pausing: 'Pausing and restoring radio profile' })[job.phase] || 'Updating';
}

export function otaStartPath(base, options) {
  const query = new URLSearchParams({expected_version: options.expected_version.trim()});
  for (const name of ['replace_existing', 'require_offline', 'exercise_confirm_timeout']) {
    if (options[name]) query.set(name, 'true');
  }
  return `${base}/firmware${query.size ? `?${query}` : ''}`;
}

export async function otaImageDetails(file, subtle = globalThis.crypto?.subtle) {
  if (file.size < 256 || file.size > 1966080) throw new Error('Select an ESP32 application image between 256 and 1966080 bytes');
  const bytes = await file.arrayBuffer();
  const descriptor = new TextDecoder().decode(new Uint8Array(bytes, 48, Math.min(32, Math.max(0, bytes.byteLength - 48)))).split('\0')[0];
  // WebCrypto is unavailable on plain HTTP LAN origins. Keep that operator
  // path usable; Graywolf always computes and verifies the authoritative hash.
  const digest = subtle ? await subtle.digest('SHA-256', bytes) : null;
  return {descriptor, size: bytes.byteLength, sha256: digest ? Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('') : ''};
}

// Available on plain HTTP LAN origins too; never fall back to Math.random.
export function generateRouteSecret(cryptoApi = globalThis.crypto) {
  if (typeof cryptoApi?.getRandomValues !== 'function') {
    throw new Error('Secure random generation is unavailable in this browser. Import an existing route key instead.');
  }
  const bytes = cryptoApi.getRandomValues(new Uint8Array(32));
  return btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}


export const otaBoards = [
  {value:'ttgo-lora32-v21', label:'TTGO LoRa32 V2.1', validation:'Physical OTA cycle validated on TTGO. The Graywolf 128-byte path still needs a hardware test.'},
  {value:'heltec-v3', label:'Heltec V3', validation:'Software tests only; hardware validation still required.'},
  {value:'qrp-labs-lightgateway-plus', label:'QRP Labs LightGateway Plus', validation:'Software tests only; hardware validation by Jon still required.'},
];
export const otaBoard = value => otaBoards.find(board => board.value === value);

export function otaPreflightPath(base,options){
 return `${base}/preflight?${new URLSearchParams({expected_version:options.expected_version.trim()})}`;
}

// Further relays after the first one, nearest first: "CALL[:RFNAME], ...".
// RFNAME is the on-air (tactical) name when it differs from the Callsign-SSID.
export function formatRelayChain(chain) {
  return (chain || []).map(h => h.recipient && h.recipient !== h.callsign ? `${h.callsign}:${h.recipient}` : h.callsign).join(', ');
}
export function parseRelayChain(text) {
  return String(text || '').split(',').map(item => item.trim().toUpperCase()).filter(Boolean).map(item => {
    const [callsign, recipient = ''] = item.split(':').map(part => part.trim());
    return recipient && recipient !== callsign ? {callsign, recipient} : {callsign};
  });
}

import test from 'node:test';
import assert from 'node:assert/strict';
import { igateRows, formatDX } from './igate-presentation.js';

test('reports retain zero counters, omit absent counters and format remote DX', () => {
  assert.deepEqual(igateRows({ entries: { IGATE: '', MSG_CNT: '0', RF_CNT: '29', DX: '1*F5ZQB-10(139km@136°)' } }), [
    ['Messages gated', '0', 'MSG_CNT'], ['RF stations', '29', 'RF_CNT'], ['DX', 'F5ZQB-10 · 139 km · 136°', 'DX'],
  ]);
});
test('capability reports without DX still show counters and malformed counters are omitted', () => {
  assert.deepEqual(igateRows({ entries: { IGATE: '', DIR_CNT: '1', RF_CNT: '-1', MSG_CNT: 'oops' } }), [['Direct stations', '1', 'DIR_CNT']]);
  assert.deepEqual(igateRows(null), []);
  assert.deepEqual(igateRows({ entries: { RF_CNT: '12' } }), []);
});
test('DX supports Graywolf and replacement degree bytes without inventing unknown data', () => {
  assert.equal(formatDX('F4MLV-10(0km@280deg)'), 'F4MLV-10 · 0 km · 280°');
  assert.equal(formatDX('1*F6DEV-10(94km@139\uFFFD)'), 'F6DEV-10 · 94 km · 139°');
  assert.equal(formatDX('unrecognized'), 'unrecognized');
});

test('receiver DX carries measured link metrics, separate from advertised capabilities', async () => {
 const {receiverDXRows}=await import('./igate-presentation.js');
 const rows=Object.fromEntries(receiverDXRows({direct_stations:3,dx:{callsign:'F1ZDB-13',distance_km:117.15,bearing_deg:136.3,rssi_dbm:-100,snr_db:0,frequency_error_hz:0}}).map(([key,value])=>[key,value]));
 assert.equal(rows.DX,'F1ZDB-13 · 117.2 km · 136°');
 assert.equal(rows.SNR,'0 dB');assert.equal(rows['Frequency offset'],'0 Hz');
 assert.ok(!Object.hasOwn(rows,'Messages gated'));
 assert.equal(receiverDXRows(null).length,0);
 assert.equal(receiverDXRows({direct_stations:1})[1][1],'No qualifying positioned link');
});

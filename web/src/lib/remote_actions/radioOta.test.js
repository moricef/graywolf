import test from 'node:test';
import assert from 'node:assert/strict';
import { otaProgress, otaPhase, otaStartPath, otaImageDetails, generateRouteSecret } from './radioOta.js';
test('full byte count is not boot confirmation', () => {
 assert.equal(otaProgress({state:'running',written:512,total:512}),99);
 assert.equal(otaProgress({state:'succeeded',written:512,total:512}),100);
 assert.match(otaPhase({state:'succeeded',post_install_verified:true}),/installation verified/);
});
test('interrupted, paused and failed jobs never become success', () => {
 for (const state of ['failed','paused','interrupted']) assert.notEqual(otaProgress({state,written:512,total:512}),100);
 assert.match(otaPhase({state:'paused'}),/same firmware/);
 assert.match(otaPhase({state:'interrupted'}),/restart/);
});

test('normal updates omit destructive and validation flags; explicit choices travel together', () => {
 const base='/radio-ota/GENATX';
 assert.equal(otaStartPath(base,{expected_version:'new'}),`${base}/firmware?expected_version=new`);
 const query=new URL(otaStartPath(base,{expected_version:'new',replace_existing:true,require_offline:true,exercise_confirm_timeout:true}),'http://localhost').searchParams;
 for(const name of ['replace_existing','require_offline','exercise_confirm_timeout']) assert.equal(query.get(name),'true');
});
test('image descriptor and SHA are read from the selected bytes', async () => {
 const bytes=new Uint8Array(512);bytes.set(new TextEncoder().encode('4.0.2RXT-OTA-B'),48);
 const details=await otaImageDetails(new Blob([bytes]));
 assert.equal(details.descriptor,'4.0.2RXT-OTA-B');assert.equal(details.size,512);assert.match(details.sha256,/^[a-f0-9]{64}$/);
});

test('plain HTTP LAN origins can still select firmware without WebCrypto', async () => {
 const details=await otaImageDetails(new Blob([new Uint8Array(512)]),null);
 assert.equal(details.size,512);assert.equal(details.sha256,'');
});

test('historical success does not invent a final readback proof', () => {
 assert.match(otaPhase({state:'succeeded'}),/final readback not recorded/);
});

test('route keys encode exactly 32 cryptographically random bytes as unpadded Base64URL', () => {
 let calls = 0;
 const source = {getRandomValues(bytes) { calls++; assert.equal(bytes.length,32); bytes.set(Array.from({length:32},(_,i)=>i)); return bytes; }};
 const key=generateRouteSecret(source);
 assert.equal(calls,1);
 assert.equal(key,'AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8');
 assert.match(key,/^[A-Za-z0-9_-]{43}$/);
 assert.equal(Buffer.from(key,'base64url').length,32);
 assert.notEqual(generateRouteSecret(),generateRouteSecret());
});
test('route key generation fails closed without a secure random source', () => {
 for(const source of [null,{}]) assert.throws(()=>generateRouteSecret(source),/Secure random generation is unavailable/);
});

test('route checks remain distinct from firmware installation', async () => {
 const {otaPreflightPath}=await import('./radioOta.js');
 assert.equal(otaProgress({state:'ready',written:0,total:512}),0);
 assert.match(otaPhase({state:'ready'}),/no firmware transferred/);
 assert.match(otaPhase({state:'running',phase:'releasing_session'}),/without discarding/);
 assert.equal(otaPreflightPath('/radio-ota/GENATX',{expected_version:' new ',replace_existing:true}),'/radio-ota/GENATX/preflight?expected_version=new');
});

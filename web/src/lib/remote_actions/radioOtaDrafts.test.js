import test from 'node:test';
import assert from 'node:assert/strict';
import { rememberOtaDraft, otaDraft, discardOtaDraft, frequencyHz } from './radioOtaDrafts.js';
test('drafts survive reopening and remain separate by station', () => {
 const config={bridge:'COM4',normal:{frequency_hz:0},route_secret:'test-draft'};
 rememberOtaDraft('ONE',config,{normal:'434,100',transfer:'434.550'});
 config.bridge='changed';
 assert.equal(otaDraft('ONE').settings.bridge,'COM4');
 assert.equal(otaDraft('ONE').frequencies.normal,'434,100');
 assert.equal(otaDraft('TWO'),null);
 const read=otaDraft('ONE');read.settings.bridge='other';
 assert.equal(otaDraft('ONE').settings.bridge,'COM4');
 discardOtaDraft('ONE');assert.equal(otaDraft('ONE'),null);
});
test('MHz text accepts decimal comma or point and never silently becomes zero', () => {
 assert.equal(frequencyHz('434,100'),434100000);
 assert.equal(frequencyHz('433.775'),433775000);
 for (const value of ['', '434.', 'text', '434.1234567']) assert.throws(()=>frequencyHz(value));
});

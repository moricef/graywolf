import assert from 'node:assert/strict';
import test from 'node:test';

import {
  CUSTOM_IGATE_SERVER,
  IGATE_SERVER_OPTIONS,
  igateServerSelection,
  nextIgateServerState,
} from './igateServer.js';

test('offers Worldwide, the five Tier-2 regional rotate addresses, and Custom', () => {
  assert.deepEqual(
    IGATE_SERVER_OPTIONS.map((option) => option.value),
    [
      'rotate.aprs2.net',
      'noam.aprs2.net',
      'soam.aprs2.net',
      'euro.aprs2.net',
      'asia.aprs2.net',
      'aunz.aprs2.net',
      CUSTOM_IGATE_SERVER,
    ]
  );
});

test('selects a known regional server directly', () => {
  assert.equal(igateServerSelection('euro.aprs2.net'), 'euro.aprs2.net');
});

test('selects Worldwide for the default rotate.aprs2.net server', () => {
  // Fresh installs default to rotate.aprs2.net; it must not render as Custom.
  assert.equal(igateServerSelection('rotate.aprs2.net'), 'rotate.aprs2.net');
});

test('preserves operator-provided hosts through Custom', () => {
  assert.equal(igateServerSelection('aprs.example.net'), CUSTOM_IGATE_SERVER);
});

test('uses the selected regional host when switching to Custom without a remembered host', () => {
  assert.deepEqual(
    nextIgateServerState(
      { selection: 'euro.aprs2.net', server: 'euro.aprs2.net', customServer: '' },
      CUSTOM_IGATE_SERVER
    ),
    {
      selection: CUSTOM_IGATE_SERVER,
      server: 'euro.aprs2.net',
      customServer: '',
    }
  );
});

test('remembers a custom host while switching between server options', () => {
  const regional = nextIgateServerState(
    {
      selection: CUSTOM_IGATE_SERVER,
      server: 'aprs.example.net',
      customServer: 'rotate.aprs2.net',
    },
    'asia.aprs2.net'
  );

  assert.deepEqual(
    nextIgateServerState(regional, CUSTOM_IGATE_SERVER),
    {
      selection: CUSTOM_IGATE_SERVER,
      server: 'aprs.example.net',
      customServer: 'aprs.example.net',
    }
  );
});

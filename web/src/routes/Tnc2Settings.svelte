<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input, Select } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';

  let config = $state({ tcp_address: '', serial_device: '', serial_baud: 115200, tx_transport: '', tx_source: '', tx_channel: 1, max_tx_bytes: 255 });
  let status = $state({ tcp_connected: false, serial_connected: false });
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');
  let saved = $state(false);
  let serialPorts = $state([]);
  let timer;

  onMount(() => {
    load();
    fetch('/api/kiss/available-serial-ports').then((r) => r.ok ? r.json() : []).then((ports) => { serialPorts = ports || []; }).catch(() => {});
    timer = setInterval(refreshStatus, 5000);
    return () => clearInterval(timer);
  });

  async function load() {
    try {
      const response = await fetch('/api/tnc2/config');
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      config = { ...config, ...data };
      status = data;
      error = '';
    } catch (err) { error = `Unable to load TNC2 settings: ${err.message}`; }
    finally { loading = false; }
  }

  async function refreshStatus() {
    try {
      const response = await fetch('/api/tnc2/config');
      if (response.ok) status = await response.json();
    } catch { /* Keep the last known state until the next refresh. */ }
  }

  async function save() {
    saving = true;
    saved = false;
    error = '';
    try {
      const response = await fetch('/api/tnc2/config', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tcp_address: config.tcp_address, serial_device: config.serial_device,
          serial_baud: Number(config.serial_baud), tx_transport: config.tx_transport,
          tx_source: config.tx_source, tx_channel: Number(config.tx_channel), max_tx_bytes: Number(config.max_tx_bytes) }),
      });
      if (!response.ok) throw new Error((await response.text()).trim() || `HTTP ${response.status}`);
      status = await response.json();
      saved = true;
    } catch (err) { error = `Unable to save TNC2 settings: ${err.message}`; }
    finally { saving = false; }
  }
</script>

<PageHeader title="TNC2" subtitle="Plain-text LoRa transceiver connections" />
{#if error}<div class="error" role="alert">{error}</div>{/if}
<form onsubmit={(event) => { event.preventDefault(); save(); }}>
  <Box title="Connections">
    <div class="fields">
      <label for="tnc2-tcp">TCP host and port</label>
      <Input id="tnc2-tcp" bind:value={config.tcp_address} disabled={loading || saving} placeholder="192.168.1.161:8001" />
      <label for="tnc2-serial">Serial device</label>
      <Input id="tnc2-serial" bind:value={config.serial_device} disabled={loading || saving} placeholder="/dev/ttyUSB0" />
      {#if serialPorts.length}
        <span></span><Select aria-label="Detected serial ports" value="" onValueChange={(value) => { if (value) config.serial_device = value; }}
          options={[{ value: '', label: 'Choose a detected port…' }, ...serialPorts.map((port) => ({ value: port.path, label: `${port.description || port.path} (${port.path})` }))]} />
      {/if}
      <label for="tnc2-baud">Serial baud rate</label>
      <Input id="tnc2-baud" type="number" min="1" bind:value={config.serial_baud} disabled={loading || saving} />
    </div>
    <p class="hint">Both connections can receive. Leave an address or device empty to disable that connection.</p>
  </Box>
  <Box title="Transmission">
    <div class="fields">
      <label for="tnc2-transport">Transmit via</label>
      <Select id="tnc2-transport" bind:value={config.tx_transport} disabled={loading || saving}
        options={[{ value: '', label: 'Disabled' }, { value: 'tcp', label: 'TCP' }, { value: 'serial', label: 'Serial' }]} />
      <label for="tnc2-source">Source identity</label>
      <Input id="tnc2-source" bind:value={config.tx_source} disabled={loading || saving} placeholder="F4MLV-2" />
      <label for="tnc2-channel">RF TX channel</label>
      <Input id="tnc2-channel" type="number" min="1" bind:value={config.tx_channel} disabled={loading || saving} />
      <label for="tnc2-limit">Maximum packet bytes</label>
      <Input id="tnc2-limit" type="number" min="1" bind:value={config.max_tx_bytes} disabled={loading || saving} />
    </div>
    <p class="hint">Messages and beacons on this channel use native TNC2 TX. Only packets with the exact source identity can be transmitted. Incoming packets remain receive-only.</p>
    <div class="actions"><Button variant="primary" type="submit" disabled={loading || saving}>{saving ? 'Saving…' : 'Save'}</Button>{#if saved}<span class="saved">Saved and applied.</span>{/if}</div>
  </Box>
</form>
<Box title="Connection status"><div class="status"><span>TCP</span><strong class:connected={status.tcp_connected}>{status.tcp_connected ? 'Connected' : 'Disconnected'}</strong><span>Serial</span><strong class:connected={status.serial_connected}>{status.serial_connected ? 'Connected' : 'Disconnected'}</strong></div></Box>

<style>
  form { display: grid; gap: 16px; }
  .fields, .status { display: grid; grid-template-columns: minmax(140px, 220px) minmax(0, 1fr); gap: 10px 18px; align-items: center; }
  label, .status span { color: var(--text-muted); font-size: 13px; }
  .hint { margin: 12px 0 0; color: var(--text-muted); font-size: 13px; }
  .actions { display: flex; align-items: center; gap: 12px; margin-top: 16px; }
  .saved, .connected { color: #2fbf71; }
  .error { margin-bottom: 12px; padding: 10px 12px; color: var(--color-danger, #ff6b6b); border: 1px solid currentColor; border-radius: 6px; }
  @media (max-width: 600px) { .fields { grid-template-columns: 1fr; gap: 6px; } }
</style>

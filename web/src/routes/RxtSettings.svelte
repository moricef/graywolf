<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';

  let endpoint = $state('');
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');
  let saved = $state(false);
  let status = $state(null);
  let statusTimer;

  onMount(() => {
    loadConfig();
    loadStatus();
    statusTimer = setInterval(loadStatus, 5000);
    return () => clearInterval(statusTimer);
  });

  async function loadConfig() {
    try {
      const response = await fetch('/api/rxt/config', { credentials: 'same-origin' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      endpoint = (await response.json()).endpoint || '';
      error = '';
    } catch (err) {
      error = `Unable to load RXT configuration: ${err?.message || err}`;
    } finally {
      loading = false;
    }
  }

  async function saveConfig() {
    saving = true;
    saved = false;
    try {
      const response = await fetch('/api/rxt/config', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpoint }),
      });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      endpoint = (await response.json()).endpoint || '';
      error = '';
      saved = true;
      await loadStatus();
    } catch (err) {
      error = `Unable to save RXT configuration: ${err?.message || err}`;
    } finally {
      saving = false;
    }
  }

  async function loadStatus() {
    try {
      const response = await fetch('/api/rxt/status', { credentials: 'same-origin' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      status = await response.json();
    } catch (err) {
      status = { enabled: false, last_error: `Unable to load status: ${err?.message || err}` };
    }
  }

  function timestamp(value) {
    if (!value) return 'Never';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? 'Never' : date.toLocaleString();
  }

  function healthLabel() {
    if (!status?.enabled) return 'Disabled';
    if (status.last_error) return 'Error';
    if (status.last_success_at) return 'Healthy';
    return 'Waiting for first response';
  }
</script>

<PageHeader title="RXT" subtitle="LoRa reception telemetry source" />

{#if error}<div class="error" role="alert">{error}</div>{/if}

<Box title="iGate endpoint">
  <form class="config" onsubmit={(event) => { event.preventDefault(); saveConfig(); }}>
    <label for="rxt-endpoint">RXT JSON URL</label>
    <div class="config-row">
      <Input id="rxt-endpoint" type="url" bind:value={endpoint} disabled={loading} placeholder="http://192.168.1.161/rxt.json" />
      <Button variant="primary" type="submit" disabled={loading || saving}>{saving ? 'Saving…' : 'Save'}</Button>
    </div>
    <p class="hint">Graywolf polls this iGate endpoint every five seconds. Leave it empty to disable RXT polling.</p>
    {#if saved}<p class="saved">Saved and applied.</p>{/if}
  </form>
</Box>

<Box title="Connection status">
  <div class="status-grid">
    <span>State</span><strong class:healthy={status?.enabled && status?.last_success_at && !status?.last_error} class:failed={status?.last_error}>{healthLabel()}</strong>
    <span>Last attempt</span><strong>{timestamp(status?.last_attempt_at)}</strong>
    <span>Last success</span><strong>{timestamp(status?.last_success_at)}</strong>
    <span>Records received</span><strong>{status?.records_received ?? 0}</strong>
    <span>Active links</span><strong>{status?.active_links ?? 0}</strong>
  </div>
  {#if status?.last_error}<p class="status-error">{status.last_error}</p>{/if}
</Box>

<style>
  .config { display: grid; gap: 8px; }
  .config-row { display: grid; grid-template-columns: minmax(240px, 1fr) auto; gap: 8px; }
  .hint, .saved { margin: 4px 0 0; font-size: 13px; color: var(--text-muted); }
  .saved { color: #2fbf71; }
  .error { margin-bottom: 12px; padding: 10px 12px; color: var(--color-danger, #ff6b6b); border: 1px solid currentColor; border-radius: 6px; }
  .status-grid { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 20px; font-size: 13px; }
  .status-grid span { color: var(--text-muted); }
  .healthy { color: #2fbf71; }
  .failed, .status-error { color: var(--color-danger, #ff6b6b); }
  .status-error { margin: 12px 0 0; font-size: 13px; overflow-wrap: anywhere; }
</style>

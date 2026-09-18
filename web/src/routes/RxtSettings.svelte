<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';

  let endpoint = $state('');
  let loading = $state(true);
  let saving = $state(false);
  let error = $state('');
  let saved = $state(false);

  onMount(loadConfig);

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
    } catch (err) {
      error = `Unable to save RXT configuration: ${err?.message || err}`;
    } finally {
      saving = false;
    }
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

<style>
  .config { display: grid; gap: 8px; }
  .config-row { display: grid; grid-template-columns: minmax(240px, 1fr) auto; gap: 8px; }
  .hint, .saved { margin: 4px 0 0; font-size: 13px; color: var(--text-muted); }
  .saved { color: #2fbf71; }
  .error { margin-bottom: 12px; padding: 10px 12px; color: var(--color-danger, #ff6b6b); border: 1px solid currentColor; border-radius: 6px; }
</style>

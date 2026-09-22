<script>
  import { onMount } from 'svelte';
  import { Box, Button, Input } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';

  let endpoints = $state(['']);
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
      const body = await response.json();
      endpoints = body.endpoints?.length ? body.endpoints : [''];
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
        body: JSON.stringify({ endpoints: endpoints.map((value) => value.trim()).filter(Boolean) }),
      });
      if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
      const body = await response.json();
      endpoints = body.endpoints?.length ? body.endpoints : [''];
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

  function healthLabel(source) {
    if (!source?.enabled) return 'Disabled';
    if (source.last_error) return 'Error';
    if (source.last_success_at) return 'Healthy';
    return 'Waiting for first response';
  }

  function addEndpoint() {
    endpoints = [...endpoints, ''];
  }

  function removeEndpoint(index) {
    endpoints = endpoints.filter((_, current) => current !== index);
    if (!endpoints.length) endpoints = [''];
  }
</script>

<PageHeader title="RXT" subtitle="LoRa reception telemetry source" />

{#if error}<div class="error" role="alert">{error}</div>{/if}

<Box title="iGate endpoints">
  <form class="config" onsubmit={(event) => { event.preventDefault(); saveConfig(); }}>
    <label for="rxt-endpoint-0">LoRa APRS JSON URLs</label>
    {#each endpoints as endpoint, index}
      <div class="config-row">
        <Input id={`rxt-endpoint-${index}`} type="url" bind:value={endpoints[index]} disabled={loading} placeholder="http://192.168.1.161/api/v1/aprs/stream" />
        <Button type="button" onclick={() => removeEndpoint(index)} disabled={loading}>Remove</Button>
      </div>
    {/each}
    <div class="actions">
      <Button type="button" onclick={addEndpoint} disabled={loading}>Add source</Button>
      <Button variant="primary" type="submit" disabled={loading || saving}>{saving ? 'Saving…' : 'Save'}</Button>
    </div>
    <p class="hint">Graywolf consumes every configured source independently and combines their RXT links. NDJSON streams and legacy /rxt.json polling are both supported.</p>
    {#if saved}<p class="saved">Saved and applied.</p>{/if}
  </form>
</Box>

<Box title={`Connection status · ${status?.active_links ?? 0} active links`}>
  {#if !status?.sources?.length}
    <p class="hint">No RXT source configured.</p>
  {/if}
  {#each status?.sources || [] as source}
    <section class="source-status">
      <h3>{source.endpoint}</h3>
      <div class="status-grid">
        <span>State</span><strong class:healthy={source.enabled && source.last_success_at && !source.last_error} class:failed={source.last_error}>{healthLabel(source)}</strong>
        <span>Last attempt</span><strong>{timestamp(source.last_attempt_at)}</strong>
        <span>Last success</span><strong>{timestamp(source.last_success_at)}</strong>
        <span>Records received</span><strong>{source.records_received ?? 0}</strong>
        <span>Active links</span><strong>{source.active_links ?? 0}</strong>
        <span>History resume</span><strong>{source.resume_supported ? 'Available' : 'Unavailable'}</strong>
        <span>Last cursor</span><strong class="cursor">{source.last_event_id || 'None'}</strong>
      </div>
      {#if source.last_error}<p class="status-error">{source.last_error}</p>{/if}
    </section>
  {/each}
</Box>

<style>
  .config { display: grid; gap: 8px; }
  .config-row { display: grid; grid-template-columns: minmax(240px, 1fr) auto; gap: 8px; }
  .actions { display: flex; justify-content: space-between; gap: 8px; }
  .hint, .saved { margin: 4px 0 0; font-size: 13px; color: var(--text-muted); }
  .saved { color: #2fbf71; }
  .error { margin-bottom: 12px; padding: 10px 12px; color: var(--color-danger, #ff6b6b); border: 1px solid currentColor; border-radius: 6px; }
  .status-grid { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 20px; font-size: 13px; }
  .status-grid span { color: var(--text-muted); }
  .source-status + .source-status { margin-top: 18px; padding-top: 18px; border-top: 1px solid var(--border-subtle, rgba(255,255,255,.12)); }
  .source-status h3 { margin: 0 0 10px; font-size: 13px; overflow-wrap: anywhere; }
  .cursor { overflow-wrap: anywhere; }
  .healthy { color: #2fbf71; }
  .failed, .status-error { color: var(--color-danger, #ff6b6b); }
  .status-error { margin: 12px 0 0; font-size: 13px; overflow-wrap: anywhere; }
</style>
